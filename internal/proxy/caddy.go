package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/atomlust/atomgo/internal/target"
)

// Caddy 编排常量(每主机一个 Caddy 容器,跑在共享 docker 网络上,按上游容器名路由)。
const (
	// proxyNetwork 是 Caddy 与上游容器共享的 docker 网络名(Caddy 据此按容器名解析上游)。
	proxyNetwork = "atomgo-proxy"
	// caddyContainer 是托管的反代容器名(每主机一个,幂等创建)。
	caddyContainer = "atomgo-caddy"
	// defaultCaddyImage 是默认反代镜像:官方 stock caddy(caddyserver/caddy,见
	// https://github.com/caddyserver/caddy),即 Docker Hub 上的 `caddy:2`。
	// 官方 stock caddy 原生支持 HTTP-01 自动 HTTPS,覆盖绝大多数反代场景(多域名、路径路由、
	// 负载均衡、健康检查、gRPC、Basic Auth、安全头、重定向、IP 名单、子域名等)。
	// 仅「TCP 透传(layer4)」一项依赖 caddy-l4 插件,stock caddy 默认不含 —— 需要该能力时,
	// 经 ATOMGO_CADDY_IMAGE 指向一个自带 caddy-l4 的镜像即可(如自行用 xcaddy 构建)。
	// 注意:DNS-01 / 通配符证书所需的插件已随 DNS 提供商集成一并移除,本项目不再签发通配符证书。
	defaultCaddyImage = "caddy:2"
	// caddyImageEnv 是覆盖反代镜像的环境变量名。
	caddyImageEnv = "ATOMGO_CADDY_IMAGE"
	// caddyDataVol 是证书/ACME 账户持久卷(删路由不删卷,避免重签触发 LE 限速)。
	caddyDataVol = "atomgo_caddy_data"
	// caddyConfigVol 是 Caddy 自动持久化配置卷。
	caddyConfigVol = "atomgo_caddy_config"
	// caddyfilePath 是容器内 Caddyfile 路径。
	caddyfilePath = "/etc/caddy/Caddyfile"
	// caddyfileTmpPath 是 apply 时在目标主机上的临时落盘路径(再 docker cp 进容器)。
	caddyfileTmpPath = "/tmp/atomgo-caddyfile"
)

// 反代运行模式(ATOMGO_PROXY_MODE 控制):
//   - modeManaged:默认。AtomGo 在目标机自建并托管一个 Caddy 容器(atomgo-caddy),独占 80/443。
//   - modeExternal:外接模式。目标机已存在用户自管的官方 Caddy,AtomGo 不接管容器、不占端口,
//     只把反代片段写入主机目录、并触发用户主 Caddyfile 的 reload(其需 `import` 该片段)。
const (
	modeManaged  = "managed"
	modeExternal = "external"
	// proxyModeEnv 选择反代运行模式;非法/空 → managed。
	proxyModeEnv = "ATOMGO_PROXY_MODE"
	// externalConfigPathEnv 是外接模式下用户主 Caddyfile 路径(AtomGo reload 它)。
	externalConfigPathEnv = "ATOMGO_CADDY_EXTERNAL_CONFIG_PATH"
	// externalFragmentDirEnv 是外接模式下 AtomGo 写入反代片段的目录(主 Caddyfile 需 import 之)。
	externalFragmentDirEnv = "ATOMGO_CADDY_EXTERNAL_FRAGMENT_DIR"
	// externalReloadCmdEnv 是外接模式下触发 reload 的命令(数组形态,空格分隔);默认 `caddy reload`。
	externalReloadCmdEnv = "ATOMGO_CADDY_EXTERNAL_RELOAD_CMD"
	// 默认值(官方 caddy 镜像约定的主配置路径 / 片段目录)。
	defaultExternalConfigPath  = "/etc/caddy/Caddyfile"
	defaultExternalFragmentDir = "/etc/atomgo-proxy"
	// externalFragmentFile 是 AtomGo 在外接模式写出的反代片段文件名(主 Caddyfile 应 import 它)。
	externalFragmentFile = "routes.caddy"
)

// 反代运行模式(对外导出;与内部 modeManaged / modeExternal 同义,供 API 层 / 前端引用)。
const (
	ModeManaged  = "managed"  // 默认:AtomGo 在目标机自建并托管 Caddy 容器。
	ModeExternal = "external" // 外接:目标机已有用户自管 Caddy,AtomGo 只写片段 + reload。
)

// renderCaddyfile 据一组 enabled 路由生成 Caddyfile 文本(纯函数,可直接单测)。
// 每条路由产一个站点块:`domain {\n    reverse_proxy container:port\n}`。Caddy 见到带域名的
// 站点块即自动经 Let's Encrypt(HTTP-01)签发并续期证书,无需任何额外指令(DNS-01 / 通配符已移除)。
// 无 enabled 路由时返回带说明注释的空配置(reload 一份合法空配置,不破坏既有 Caddy 进程)。
func renderCaddyfile(routes []Route) string {
	// 稳定排序:按 domain 升序,保证渲染结果确定(便于测试 + reload 幂等)。
	sorted := make([]Route, len(routes))
	copy(sorted, routes)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Domain < sorted[j].Domain })

	var b strings.Builder
	b.WriteString("# 由 AtomGo 自动生成,请勿手改。\n")

	// R4 E4.3:TCP 透传(caddy-l4)走 Caddyfile **全局选项块**里的 layer4 段,不是 HTTP 站点块。
	// 凡有路由带 TCPPassthrough,就先发一个全局 { layer4 { :port { route { proxy c:port } } } }。
	// 必须在任何站点块之前(Caddyfile 要求全局选项块为文件首个块)。
	renderLayer4Global(&b, sorted)

	if len(sorted) == 0 {
		b.WriteString("# 当前没有启用的反代路由。\n")
		return b.String()
	}
	for _, r := range sorted {
		renderSite(&b, r)
	}
	return b.String()
}

// renderLayer4Global 渲染 TCP 透传的 layer4 全局块(R4 E4.3;caddy-l4)。
// 仅当至少一条路由带 TCPPassthrough 时才输出;按监听端口升序稳定排序(渲染确定 + reload 幂等)。
// 形如:
//
//	{
//	    layer4 {
//	        :5432 {
//	            route {
//	                proxy db_container:5432
//	            }
//	        }
//	    }
//	}
//
// 所有进入文本的值已在领域层(validateConfig)严格校验过(端口 1..65535 + 容器名安全字符集)。
func renderLayer4Global(b *strings.Builder, routes []Route) {
	tcps := make([]TCPConfig, 0)
	for _, r := range routes {
		if tc := r.Config.TCPPassthrough; tc != nil {
			tcps = append(tcps, *tc)
		}
	}
	if len(tcps) == 0 {
		return
	}
	sort.Slice(tcps, func(i, j int) bool { return tcps[i].ListenPort < tcps[j].ListenPort })

	b.WriteString("{\n")
	b.WriteString("    layer4 {\n")
	for _, tc := range tcps {
		b.WriteString("        :" + strconv.Itoa(tc.ListenPort) + " {\n")
		b.WriteString("            route {\n")
		b.WriteString("                proxy " + tc.UpstreamContainer + ":" + strconv.Itoa(tc.UpstreamPort) + "\n")
		b.WriteString("            }\n")
		b.WriteString("        }\n")
	}
	b.WriteString("    }\n")
	b.WriteString("}\n")
}

// renderSite 渲染单条路由的站点块(R2:多域名别名 + 访问控制 + 安全头 + 压缩 + 重定向)。
// 站点块头 = 主域名 + 别名,以 ", " 连接;块内指令按确定顺序输出(IP 黑名单 → 白名单 →
// basic auth → header → encode → redir → reverse_proxy),便于 golden 测试与 reload 幂等。
// 所有进入文本的值已在领域层(validateConfig)严格校验过,渲染处不再二次防注入。
func renderSite(b *strings.Builder, r Route) {
	cfg := r.Config

	// 站点块头:主域名 + 别名。
	header := r.Domain
	for _, a := range cfg.Aliases {
		header += ", " + a
	}
	b.WriteString(header)
	b.WriteString(" {\n")

	// IP 黑名单:命中即 403(优先于白名单,显式拒绝先行)。
	// 用 handle 包 respond,不发裸 `respond @denied 403`:Caddy 的指令排序会把裸 respond 排到
	// reverse_proxy/handle 之后,一旦本路由用了路径规则/负载均衡(handle 块),IP 名单会被绕过、形同虚设。
	// handle 在指令顺序里早于 reverse_proxy,且 handle 块按源码顺序互斥求值,放在内容 handle 之前即真正拦截。
	if len(cfg.IPDeny) > 0 {
		b.WriteString("    @denied remote_ip " + strings.Join(cfg.IPDeny, " ") + "\n")
		b.WriteString("    handle @denied {\n")
		b.WriteString("        respond 403\n")
		b.WriteString("    }\n")
	}
	// IP 白名单:不在白名单内即 403(只有列出的网段可访问)。
	if len(cfg.IPAllow) > 0 {
		b.WriteString("    @notallowed not remote_ip " + strings.Join(cfg.IPAllow, " ") + "\n")
		b.WriteString("    handle @notallowed {\n")
		b.WriteString("        respond 403\n")
		b.WriteString("    }\n")
	}
	// Basic Auth:用户名 + bcrypt 哈希(两者都在才渲染)。
	if cfg.BasicAuthUser != "" && cfg.BasicAuthHash != "" {
		b.WriteString("    basic_auth {\n")
		b.WriteString("        " + cfg.BasicAuthUser + " " + cfg.BasicAuthHash + "\n")
		b.WriteString("    }\n")
	}
	// 安全头 + HSTS(任一开启即渲染 header 块)。
	if cfg.HSTS || cfg.SecurityHeaders {
		b.WriteString("    header {\n")
		if cfg.HSTS {
			b.WriteString("        Strict-Transport-Security \"max-age=31536000; includeSubDomains\"\n")
		}
		if cfg.SecurityHeaders {
			b.WriteString("        X-Frame-Options \"DENY\"\n")
			b.WriteString("        X-Content-Type-Options \"nosniff\"\n")
			b.WriteString("        Referrer-Policy \"strict-origin-when-cross-origin\"\n")
		}
		b.WriteString("    }\n")
	}
	// 压缩。
	if cfg.Compression {
		b.WriteString("    encode gzip zstd\n")
	}
	// 自定义重定向(逐条 redir from to status)。
	for _, rd := range cfg.Redirects {
		b.WriteString("    redir " + rd.From + " " + rd.To + " " + strconv.Itoa(rd.Status) + "\n")
	}

	// 反代到上游(始终最后)。
	// 主内容包进 handle 的条件:有路径规则,或有 IP 名单。有 IP 名单时必须包 handle{},
	// 才能与上面的 `handle @denied/@notallowed` 互斥——否则被拒请求 respond 403 后,裸
	// reverse_proxy 仍会执行并重复写响应。两者都无时保持裸 reverse_proxy(最简形态)。
	if len(cfg.PathRules) > 0 || len(cfg.IPAllow) > 0 || len(cfg.IPDeny) > 0 {
		// R3 E3.5 路径路由:逐条 handle <path> { reverse_proxy c:port },末尾默认 handle 落主上游。
		// 路径路由的各 handle 用裸单上游(LB/健康检查/gRPC 仅作用于默认主上游块,保持语义简单确定)。
		for _, pr := range cfg.PathRules {
			b.WriteString("    handle " + pr.Path + " {\n")
			b.WriteString("        reverse_proxy " + pr.UpstreamContainer + ":" + strconv.Itoa(pr.UpstreamPort) + "\n")
			b.WriteString("    }\n")
		}
		b.WriteString("    handle {\n")
		renderReverseProxy(b, "        ", r, cfg)
		b.WriteString("    }\n")
	} else {
		renderReverseProxy(b, "    ", r, cfg)
	}
	b.WriteString("}\n")
}

// renderReverseProxy 渲染主上游的 reverse_proxy 指令(R4 E4.2 多上游/负载均衡/健康检查 + E4.3 gRPC h2c)。
// indent 是行首缩进(站点块内 4 空格;默认 handle 内 8 空格)。
//   - 多上游:reverse_proxy primary:p up2:p up3:p(主上游始终第一)。
//   - lb_policy / health_uri / health_interval / gRPC transport 任一存在 → 渲染 { ... } 配置块;
//     否则单行(保持 R1-R3 既有 golden 不变)。
//
// 所有进入文本的值已在领域层(validateConfig)严格校验过,渲染处不再二次防注入。
func renderReverseProxy(b *strings.Builder, indent string, r Route, cfg RouteConfig) {
	// 上游列表:主上游 + 额外上游(E4.2)。
	upstreams := r.UpstreamContainer + ":" + strconv.Itoa(r.UpstreamPort)
	for _, u := range cfg.Upstreams {
		upstreams += " " + u.Container + ":" + strconv.Itoa(u.Port)
	}

	needBlock := cfg.LBPolicy != "" || cfg.HealthURI != "" || cfg.HealthInterval != "" || cfg.GRPC
	if !needBlock {
		b.WriteString(indent + "reverse_proxy " + upstreams + "\n")
		return
	}
	b.WriteString(indent + "reverse_proxy " + upstreams + " {\n")
	if cfg.LBPolicy != "" {
		b.WriteString(indent + "    lb_policy " + cfg.LBPolicy + "\n")
	}
	if cfg.HealthURI != "" {
		b.WriteString(indent + "    health_uri " + cfg.HealthURI + "\n")
	}
	if cfg.HealthInterval != "" {
		b.WriteString(indent + "    health_interval " + cfg.HealthInterval + "\n")
	}
	if cfg.GRPC {
		// gRPC 走 h2c(明文 HTTP/2)到上游容器;与上游/lb/health 合并在同一 reverse_proxy 块内。
		b.WriteString(indent + "    transport http {\n")
		b.WriteString(indent + "        versions h2c 2\n")
		b.WriteString(indent + "    }\n")
	}
	b.WriteString(indent + "}\n")
}

// ensureCaddy 幂等地在目标主机上保证 Caddy 反代就绪:建共享网络 + 起 Caddy 容器(若缺)。
// 起容器前探测 80/443 占用,冲突即返回人话错误(不强起)。
//
// tcpPorts 是本主机所有 enabled 路由声明的 TCP 透传监听端口(R4 E4.3 caddy-l4)。这些端口必须由
// Caddy 容器**对宿主发布**(-p)才能被外部访问 —— layer4 监听在容器内,不发布则不可达。Docker 端口
// 在容器创建时固定,故当既有 Caddy 容器未发布某个新增 TCP 端口时,需带「既有映射 ∪ TCP 端口」重建容器
// (镜像自带默认 Caddyfile 兜底启动,随后 apply 会覆盖为真实配置 + reload;证书/配置走持久卷不丢)。
func ensureCaddy(ctx context.Context, tg target.Service, serverID string, tcpPorts []int, settings CaddySettings) error {
	// 外接模式:不接管容器、不占 80/443。仅确保片段目录存在、写一份空片段(避免 import 指向
	// 不存在文件)、并校验用户主 Caddyfile 已 import 该片段;随后由 apply 写真实内容 + reload。
	if settings.EffectiveMode() == modeExternal {
		if err := mkdirExternalFragmentDir(ctx, tg, serverID, settings); err != nil {
			return err
		}
		if err := writeExternalFragment(ctx, tg, serverID, "# AtomGo 管理的反代片段(自动生成,请勿手改)\n", settings); err != nil {
			return err
		}
		return verifyExternalImport(ctx, tg, serverID, settings)
	}
	// 1) 共享网络(存在即跳过)。inspect 非零退出(网络不存在)→ create。
	netInsp, err := tg.Exec(ctx, serverID, []string{"docker", "network", "inspect", proxyNetwork})
	if err != nil {
		return mapExecErr(err)
	}
	if netInsp.ExitCode != 0 {
		res, cerr := tg.Exec(ctx, serverID, []string{"docker", "network", "create", proxyNetwork})
		if cerr != nil {
			return mapExecErr(cerr)
		}
		if res.ExitCode != 0 {
			return fmt.Errorf("%w:%s", ErrCaddyStart, strings.TrimSpace(firstNonEmpty(res.Stderr, res.Stdout)))
		}
	}

	// 2) Caddy 容器存在:校验是否已发布全部 TCP 监听端口。已覆盖 → 幂等返回;缺端口 → 保留既有
	//    端口映射(如自定义 18080/18443)并补上缺失的 TCP 端口后重建容器。
	insp, err := tg.Exec(ctx, serverID, []string{"docker", "inspect", caddyContainer})
	if err != nil {
		return mapExecErr(err)
	}
	if insp.ExitCode == 0 {
		cur, perr := inspectCaddyPortMap(ctx, tg, serverID)
		if perr != nil {
			return perr
		}
		if caddyPortsCover(cur, tcpPorts) {
			return nil
		}
		// 重建:rm -f 释放端口 → 用「既有映射 ∪ TCP 端口」重起。
		if rmRes, rmErr := tg.Exec(ctx, serverID, []string{"docker", "rm", "-f", caddyContainer}); rmErr != nil {
			return mapExecErr(rmErr)
		} else if rmRes.ExitCode != 0 {
			return fmt.Errorf("%w:%s", ErrCaddyStart, strings.TrimSpace(firstNonEmpty(rmRes.Stderr, rmRes.Stdout)))
		}
		return runCaddyContainer(ctx, tg, serverID, mergeCaddyPorts(cur, tcpPorts))
	}

	// 3) 全新部署:起前探测 80/443 占用(避免与既有进程抢端口,LE 校验需 80)。
	if busy, detail := portsBusy(ctx, tg, serverID); busy {
		return fmt.Errorf("%w%s", ErrPortConflict, detail)
	}
	// 默认映射 80→80 / 443→443,再叠加 TCP 端口(p→p)。
	defaults := map[int]int{80: 80, 443: 443}
	return runCaddyContainer(ctx, tg, serverID, mergeCaddyPorts(defaults, tcpPorts))
}

// runCaddyContainer 用给定的「容器端口 → 宿主端口」映射起 Caddy 容器(array 不拼 shell)。
// --add-host host.docker.internal:host-gateway 让 address 类上游能反代到宿主机服务。
func runCaddyContainer(ctx context.Context, tg target.Service, serverID string, ports map[int]int) error {
	image := caddyImageRef()
	// best-effort pull(离线/已缓存场景交由 docker run 决断)。
	if _, perr := tg.Exec(ctx, serverID, []string{"docker", "pull", image}); perr != nil {
		return mapExecErr(perr)
	}
	runCmd := []string{
		"docker", "run", "-d",
		"--name", caddyContainer,
		"--restart", "unless-stopped",
		"--network", proxyNetwork,
		"--add-host", "host.docker.internal:host-gateway",
	}
	// 端口按容器端口升序发布(渲染确定,便于诊断)。
	cports := make([]int, 0, len(ports))
	for cp := range ports {
		cports = append(cports, cp)
	}
	sort.Ints(cports)
	for _, cp := range cports {
		runCmd = append(runCmd, "-p", strconv.Itoa(ports[cp])+":"+strconv.Itoa(cp))
	}
	runCmd = append(runCmd,
		"-v", caddyDataVol+":/data",
		"-v", caddyConfigVol+":/config",
		image,
		"caddy", "run", "--config", caddyfilePath, "--adapter", "caddyfile",
	)
	res, err := tg.Exec(ctx, serverID, runCmd)
	if err != nil {
		return mapExecErr(err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("%w:%s", ErrCaddyStart, strings.TrimSpace(firstNonEmpty(res.Stderr, res.Stdout)))
	}
	return nil
}

// inspectCaddyPortMap 读 atomgo-caddy 已发布的「容器端口 → 宿主端口」(仅 tcp)。
// 无容器 / 解析失败 → 空 map(调用方据此当全新部署处理)。
func inspectCaddyPortMap(ctx context.Context, tg target.Service, serverID string) (map[int]int, error) {
	res, err := tg.Exec(ctx, serverID, []string{
		"docker", "inspect", caddyContainer,
		"--format", "{{range $p, $c := .HostConfig.PortBindings}}{{$p}}={{(index $c 0).HostPort}} {{end}}",
	})
	if err != nil {
		return nil, mapExecErr(err)
	}
	m := map[int]int{}
	for _, tok := range strings.Fields(res.Stdout) {
		eq := strings.IndexByte(tok, '=')
		if eq <= 0 {
			continue
		}
		proto := tok[:eq] // 形如 "80/tcp"
		if !strings.HasSuffix(proto, "/tcp") {
			continue
		}
		cp, e1 := strconv.Atoi(strings.TrimSuffix(proto, "/tcp"))
		hp, e2 := strconv.Atoi(tok[eq+1:])
		if e1 == nil && e2 == nil {
			m[cp] = hp
		}
	}
	return m, nil
}

// caddyPortsCover 报告既有端口映射是否已覆盖全部所需 TCP 端口。
func caddyPortsCover(cur map[int]int, tcpPorts []int) bool {
	for _, tp := range tcpPorts {
		if _, ok := cur[tp]; !ok {
			return false
		}
	}
	return true
}

// mergeCaddyPorts 在既有「容器端口 → 宿主端口」映射上叠加 TCP 端口(p→p,已存在则不覆盖)。
func mergeCaddyPorts(base map[int]int, tcpPorts []int) map[int]int {
	out := make(map[int]int, len(base)+len(tcpPorts))
	for cp, hp := range base {
		out[cp] = hp
	}
	for _, tp := range tcpPorts {
		if _, ok := out[tp]; !ok {
			out[tp] = tp
		}
	}
	return out
}

// inspectCaddy 经 docker inspect 探测目标主机上的 atomgo-caddy 容器:
//   - inspect 非零退出(No such object)= 容器不存在 → Installed:false(非错误)。
//   - inspect 成功 → Installed:true,并从 --format 输出解析 running / image。
//   - 端口为 best-effort:再发一次 inspect 读 NetworkSettings.Ports,解析失败给 ""(前端按 80/443 兜底)。
//
// 仅 target.Exec 的传输层错误(SSH 不可达 / vault 未配 / 主机不存在)才返回 error。
func inspectCaddy(ctx context.Context, tg target.Service, serverID string, settings CaddySettings) (*CaddyStatus, error) {
	// 外接模式:不存在 atomgo-caddy 容器;返回「由用户自管」的伪状态(Installed/Running 均为真,
	// Image 标 external),由上层据 mode 展示「外接 Caddy」而非容器卡片。
	if settings.EffectiveMode() == modeExternal {
		return &CaddyStatus{Mode: modeExternal, Installed: true, Running: true, Image: "external (user-managed)", Ports: ""}, nil
	}
	st := &CaddyStatus{Mode: modeManaged}
	// 一次 inspect 同时拿运行态与镜像(format 以 | 分隔,避免拼 shell)。
	insp, err := tg.Exec(ctx, serverID, []string{
		"docker", "inspect", caddyContainer,
		"--format", "{{.State.Running}}|{{.Config.Image}}",
	})
	if err != nil {
		return nil, mapExecErr(err)
	}
	if insp.ExitCode != 0 {
		// 容器不存在(或 inspect 失败但非传输层)→ 视为未安装,不报错。
		return st, nil
	}
	st.Installed = true
	out := strings.TrimSpace(insp.Stdout)
	if i := strings.IndexByte(out, '|'); i >= 0 {
		st.Running = strings.EqualFold(strings.TrimSpace(out[:i]), "true")
		st.Image = strings.TrimSpace(out[i+1:])
	}
	// 端口摘要(best-effort:任何失败都给 "",不阻断也不报错)。
	st.Ports = inspectCaddyPorts(ctx, tg, serverID)
	return st, nil
}

// inspectCaddyPorts best-effort 读 atomgo-caddy 的发布端口摘要(如 "80,443")。
// 解析 NetworkSettings.Ports 的 key(形如 "80/tcp"),取端口号去重升序。任何失败给 ""。
func inspectCaddyPorts(ctx context.Context, tg target.Service, serverID string) string {
	res, err := tg.Exec(ctx, serverID, []string{
		"docker", "inspect", caddyContainer,
		"--format", "{{json .NetworkSettings.Ports}}",
	})
	if err != nil || res == nil || res.ExitCode != 0 {
		return ""
	}
	var ports map[string]any
	if jerr := json.Unmarshal([]byte(strings.TrimSpace(res.Stdout)), &ports); jerr != nil {
		return ""
	}
	seen := map[string]struct{}{}
	nums := make([]int, 0, len(ports))
	for k := range ports {
		p := k
		if i := strings.IndexByte(p, '/'); i >= 0 {
			p = p[:i] // 去掉 "/tcp" 后缀
		}
		n, cerr := strconv.Atoi(p)
		if cerr != nil {
			continue
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		nums = append(nums, n)
	}
	if len(nums) == 0 {
		return ""
	}
	sort.Ints(nums)
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

// removeCaddy 停止并删除目标主机上的 atomgo-caddy 容器(array 不拼 shell)。
// 保留命名卷 atomgo_caddy_data(证书/ACME 账户持久,避免重建时重签触发 LE 限速)。
// 幂等:容器已不存在时,docker stop/rm 报「No such container」视为成功;仅传输层错误才返回 error。
func removeCaddy(ctx context.Context, tg target.Service, serverID string, settings CaddySettings) error {
	// 外接模式:不接管容器,不存在「移除反代容器」一说(用户自管 Caddy)。no-op。
	if settings.EffectiveMode() == modeExternal {
		return nil
	}
	// 1) stop(容器不存在的 "No such container" 容忍)。
	if _, err := tg.Exec(ctx, serverID, []string{"docker", "stop", caddyContainer}); err != nil {
		return mapExecErr(err)
	}
	// 2) rm(同样容忍不存在)。绝不删卷(无 docker volume rm)。
	if _, err := tg.Exec(ctx, serverID, []string{"docker", "rm", caddyContainer}); err != nil {
		return mapExecErr(err)
	}
	return nil
}

// portsBusy 探测目标主机 80/443 是否被(非 Caddy 容器的)进程占用。best-effort:探测命令本身
// 失败(无 ss / 无权限)视为「无法确认占用」→ 不阻断起容器(返回 false)。
func portsBusy(ctx context.Context, tg target.Service, serverID string) (bool, string) {
	res, err := tg.Exec(ctx, serverID, []string{"ss", "-ltn"})
	if err != nil || res == nil || res.ExitCode != 0 {
		return false, ""
	}
	out := res.Stdout
	var hit []string
	for _, p := range []string{":80 ", ":443 "} {
		if strings.Contains(out, p) {
			hit = append(hit, strings.TrimSpace(strings.TrimSuffix(p, " ")))
		}
	}
	if len(hit) == 0 {
		return false, ""
	}
	return true, "(" + strings.Join(hit, "、") + " 已被占用)"
}

// connectUpstream 把上游容器接入共享网络,使 Caddy 能按容器名访问它。
// 已连接(docker 报 "already exists")视为成功(幂等);其余非零退出 → 人话错误。
func connectUpstream(ctx context.Context, tg target.Service, serverID, container string, settings CaddySettings) error {
	// 外接模式:上游容器接入共享网络由用户自管(其 Caddy 与上游的组网由用户负责,通常用
	// address 上游 host.docker.internal / 宿主机 IP 即可)。此处为 no-op。
	if settings.EffectiveMode() == modeExternal {
		return nil
	}
	res, err := tg.Exec(ctx, serverID, []string{"docker", "network", "connect", proxyNetwork, container})
	if err != nil {
		return mapExecErr(err)
	}
	if res.ExitCode != 0 {
		msg := strings.ToLower(firstNonEmpty(res.Stderr, res.Stdout))
		if strings.Contains(msg, "already exists") || strings.Contains(msg, "already") {
			return nil
		}
		return fmt.Errorf("%w:%s", ErrUpstreamConnect, strings.TrimSpace(firstNonEmpty(res.Stderr, res.Stdout)))
	}
	return nil
}

// applyCaddyfile 把渲染好的 Caddyfile 落到目标主机临时路径 → docker cp 进 Caddy 容器 → reload
// (优雅热加载,零停机)。reload 前先 validate;validate 失败不 reload(不破坏既有可用配置)。
func applyCaddyfile(ctx context.Context, tg target.Service, serverID, content string, settings CaddySettings) error {
	// 外接模式:Caddy 由用户自管。把反代片段写到主机目录,再触发其主 Caddyfile 的 reload
	// (主 Caddyfile 必须 `import` 该片段目录)。TCP 透传是否可用取决于用户 Caddy 是否含
	// caddy-l4 插件 —— 这里不再做 stock 镜像早拒(那是针对托管容器镜像的),交给 validate 报错并附提示。
	if settings.EffectiveMode() == modeExternal {
		return applyExternalCaddyfile(ctx, tg, serverID, content, settings)
	}
	// 托管容器模式:官方 stock caddy 不含 caddy-l4(TCP 透传)插件:若本批配置用到了 TCP 透传,
	// 早拒并给清晰提示,避免把 stock caddy 解析不了的配置下发进去、拿到难懂的 Caddy 解析报错。
	if proxyImageIsStockCaddy() && caddyfileNeedsPlugins(content) {
		return fmt.Errorf("%w: 当前 Caddy 镜像为官方 stock caddy,不含 TCP 透传所需的 caddy-l4 插件;如需 TCP 透传请经 ATOMGO_CADDY_IMAGE 指向一个自带 caddy-l4 的镜像(如用 xcaddy 构建);其余 HTTP-01 反向代理功能 stock caddy 完整可用", ErrApply)
	}
	// 1) Upload 到主机临时路径(流式;remotePath 作位置参数,不拼 shell)。
	if err := tg.Upload(ctx, serverID, bytes.NewReader([]byte(content)), caddyfileTmpPath); err != nil {
		return mapExecErr(err)
	}
	// 1.5) 临时文件含反代配置,立即 chmod 0600 收紧权限(best-effort:chmod 失败不阻断;
	//      path 作 array 参数不拼 shell)。
	if chRes, chErr := tg.Exec(ctx, serverID, []string{"chmod", "600", caddyfileTmpPath}); chErr != nil {
		return mapExecErr(chErr)
	} else {
		_ = chRes
	}
	// 2) docker cp 进容器。
	cpRes, err := tg.Exec(ctx, serverID, []string{"docker", "cp", caddyfileTmpPath, caddyContainer + ":" + caddyfilePath})
	if err != nil {
		return mapExecErr(err)
	}
	if cpRes.ExitCode != 0 {
		return fmt.Errorf("%w:%s", ErrApply, strings.TrimSpace(firstNonEmpty(cpRes.Stderr, cpRes.Stdout)))
	}
	// 3) validate(best-effort:旧 Caddy 无 validate 子命令时跳过,不阻断 reload)。
	val, err := tg.Exec(ctx, serverID, []string{"docker", "exec", caddyContainer, "caddy", "validate", "--config", caddyfilePath, "--adapter", "caddyfile"})
	if err != nil {
		return mapExecErr(err)
	}
	if val.ExitCode != 0 && !looksUnsupported(val.Stderr) {
		return fmt.Errorf("%w:%s", ErrApply, strings.TrimSpace(firstNonEmpty(val.Stderr, val.Stdout)))
	}
	// 4) reload(优雅热加载)。
	rl, err := tg.Exec(ctx, serverID, []string{"docker", "exec", caddyContainer, "caddy", "reload", "--config", caddyfilePath, "--adapter", "caddyfile"})
	if err != nil {
		return mapExecErr(err)
	}
	if rl.ExitCode != 0 {
		return fmt.Errorf("%w:%s", ErrApply, strings.TrimSpace(firstNonEmpty(rl.Stderr, rl.Stdout)))
	}
	return nil
}

// looksUnsupported 判定一段 stderr 是否为「子命令/flag 不被该版本支持」(用于 validate 兜底跳过)。
func looksUnsupported(stderr string) bool {
	s := strings.ToLower(stderr)
	return strings.Contains(s, "unknown command") ||
		strings.Contains(s, "unknown flag") ||
		strings.Contains(s, "unknown shorthand")
}

// caddyImageRef 返回反代镜像引用:ATOMGO_CADDY_IMAGE 覆盖,否则默认自构建镜像。
// 仅接受合法镜像引用字符集(防经 env 注入 docker 额外 flag/参数);非法即回退默认。
func caddyImageRef() string {
	v := strings.TrimSpace(os.Getenv(caddyImageEnv))
	if v == "" {
		return defaultCaddyImage
	}
	if !imageRefRe.MatchString(v) {
		return defaultCaddyImage
	}
	return v
}

// proxyImageIsStockCaddy 报告当前配置的反代镜像是否为官方 stock caddy
// (即 Docker Hub 上的 caddy / caddy:2,caddyserver/caddy 出品)。
// 仅用于在「配置用到了插件能力(目前仅 caddy-l4 的 layer4 / TCP 透传)」时给出清晰人话提示,
// 绝不作为放行/拦截其它镜像的逻辑依据(避免误伤同样不含 caddy-l4 但自带其它插件的第三方镜像)。
func proxyImageIsStockCaddy() bool {
	ref := caddyImageRef()
	noTag := ref
	if i := strings.LastIndexByte(ref, ':'); i > strings.LastIndexByte(ref, '/') {
		noTag = ref[:i]
	}
	switch noTag {
	case "caddy", "library/caddy", "docker.io/library/caddy", "index.docker.io/library/caddy":
		return true
	}
	return false
}

// caddyfileNeedsPlugins 报告渲染出的 Caddyfile 是否用到了官方 stock caddy 默认不包含的插件指令:
//   - caddy-l4 的 `layer4 {` 全局块(TCP/UDP 透传)。
//
// 纯函数,便于单测。(通配符 / DNS-01 已随 DNS 提供商集成一并移除,故不再检测 tls { dns }。)
func caddyfileNeedsPlugins(content string) bool {
	for _, l := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "layer4") {
			return true
		}
	}
	return false
}

// proxyMode 返回当前反代运行模式(managed | external);非法/空 → managed。
func proxyMode() string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(proxyModeEnv)))
	if v == modeExternal {
		return modeExternal
	}
	return modeManaged
}

// externalConfigPath 返回外接模式下用户主 Caddyfile 路径(经 env 覆盖)。
func externalConfigPath() string {
	v := strings.TrimSpace(os.Getenv(externalConfigPathEnv))
	if v == "" {
		return defaultExternalConfigPath
	}
	return v
}

// externalFragmentDir 返回外接模式下 AtomGo 写反代片段的目录(经 env 覆盖)。
func externalFragmentDir() string {
	v := strings.TrimSpace(os.Getenv(externalFragmentDirEnv))
	if v == "" {
		return defaultExternalFragmentDir
	}
	return v
}

// externalFragmentPath 已由 CaddySettings.FragmentPath() 取代(片段路径 = 生效片段目录 + 片段文件名)。

// externalReloadArgs 返回外接模式触发 reload 的命令(argv 数组)。默认 `caddy reload --config
// <主Caddyfile> --adapter caddyfile`;可用 ATOMGO_CADDY_EXTERNAL_RELOAD_CMD 覆盖(如容器化 Caddy:
// "docker exec my-caddy caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile")。
func externalReloadArgs() []string {
	v := strings.TrimSpace(os.Getenv(externalReloadCmdEnv))
	if v == "" {
		return []string{"caddy", "reload", "--config", externalConfigPath(), "--adapter", "caddyfile"}
	}
	return strings.Fields(v)
}

// mkdirExternalFragmentDir 在外接模式于目标主机建片段目录(-p,幂等;数组不拼 shell)。
func mkdirExternalFragmentDir(ctx context.Context, tg target.Service, serverID string, settings CaddySettings) error {
	res, err := tg.Exec(ctx, serverID, []string{"mkdir", "-p", settings.EffectiveFragmentDir()})
	if err != nil {
		return mapExecErr(err)
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("%w:%s", ErrCaddyStart, strings.TrimSpace(firstNonEmpty(res.Stderr, res.Stdout)))
	}
	return nil
}

// writeExternalFragment 把反代片段内容落到目标主机片段文件(经 tg.Upload,再 chmod 0600 收权限;
// 内容为 HTTP-01 Caddyfile 片段,可能含域名/上游地址,绝不日志)。与托管模式的临时文件同理,
// 只是落点改为用户 import 的目录。
func writeExternalFragment(ctx context.Context, tg target.Service, serverID, content string, settings CaddySettings) error {
	path := settings.FragmentPath()
	if err := tg.Upload(ctx, serverID, strings.NewReader(content), path); err != nil {
		return mapExecErr(err)
	}
	if chRes, chErr := tg.Exec(ctx, serverID, []string{"chmod", "600", path}); chErr != nil {
		return mapExecErr(chErr)
	} else {
		_ = chRes
	}
	return nil
}

// verifyExternalImport 校验外接模式下用户主 Caddyfile 存在且已 import 我们的片段文件。
// 主配置文件缺失 → 明确报错(让用户先准备好主 Caddyfile);未含 import → 明确指引用户加一行。
// 读/查命令本身失败(无 cat/grep/权限)→ best-effort 不阻断(交给后续 reload 暴露问题)。
func verifyExternalImport(ctx context.Context, tg target.Service, serverID string, settings CaddySettings) error {
	cfg := settings.EffectiveConfigPath()
	frag := settings.FragmentPath()
	catRes, err := tg.Exec(ctx, serverID, []string{"cat", cfg})
	if err != nil {
		return mapExecErr(err)
	}
	if catRes.ExitCode != 0 {
		return fmt.Errorf("%w: 外接模式要求主 Caddyfile(%s)存在且可读,当前读取失败:%s", ErrExternalConfigImport, cfg, strings.TrimSpace(firstNonEmpty(catRes.Stderr, "exit "+strconv.Itoa(catRes.ExitCode))))
	}
	gr, gerr := tg.Exec(ctx, serverID, []string{"grep", "-q", "--", frag, cfg})
	if gerr != nil {
		return mapExecErr(gerr)
	}
	if gr.ExitCode != 0 {
		return fmt.Errorf("%w: 请在主 Caddyfile(%s)添加一行 `import %s`,AtomGo 写入的反代片段才会被加载", ErrExternalConfigImport, cfg, frag)
	}
	return nil
}

// applyExternalCaddyfile 是外接模式的 apply:写片段 → validate(best-effort)→ reload 用户主 Caddyfile。
// 不接管容器、不碰用户其它站点(主 Caddyfile 经 import 合并我们的片段)。
func applyExternalCaddyfile(ctx context.Context, tg target.Service, serverID, content string, settings CaddySettings) error {
	if err := writeExternalFragment(ctx, tg, serverID, content, settings); err != nil {
		return err
	}
	cfg := settings.EffectiveConfigPath()
	// validate(best-effort:旧 caddy 无 validate 子命令时 looksUnsupported 跳过,不阻断 reload)。
	val, err := tg.Exec(ctx, serverID, []string{"caddy", "validate", "--config", cfg, "--adapter", "caddyfile"})
	if err != nil {
		return mapExecErr(err)
	}
	if val.ExitCode != 0 && !looksUnsupported(val.Stderr) {
		return enrichExternalApplyErr(content, fmt.Errorf("%w:%s", ErrApply, strings.TrimSpace(firstNonEmpty(val.Stderr, val.Stdout))))
	}
	// reload(触发用户主 Caddyfile 重新加载,含 import 的我们的片段)。
	rl, err := tg.Exec(ctx, serverID, settings.EffectiveReloadArgs())
	if err != nil {
		return mapExecErr(err)
	}
	if rl.ExitCode != 0 {
		return enrichExternalApplyErr(content, fmt.Errorf("%w:%s", ErrApply, strings.TrimSpace(firstNonEmpty(rl.Stderr, rl.Stdout))))
	}
	return nil
}

// ExternalImportResult 是「自动注入 import 行」的预览/执行结果(供前端展示与二次确认)。
type ExternalImportResult struct {
	// File 是用户主 Caddyfile 的绝对路径。
	File string `json:"file"`
	// Line 是待注入(或已存在)的 import 行,如 `import /etc/atomgo-proxy/routes.caddy`。
	Line string `json:"line"`
	// Present 表示主文件当前是否已含该 import 行。
	Present bool `json:"present"`
	// Applied 表示本次调用是否实际写入了文件(仅 apply 阶段、且原本缺失时为 true)。
	Applied bool `json:"applied"`
	// ReloadWarning 是 reload 的非致命告警(空 = 正常);仅 apply 阶段有意义。
	ReloadWarning string `json:"reloadWarning"`
}

// InjectExternalImport 在外接模式把 AtomGo 反代片段的 import 行追加进用户主 Caddyfile。
// apply=false 仅做预览:读取主文件判定是否已含 import 行,不写盘;apply=true 才实际写入。
// 写入前确保片段目录存在、且片段文件存在(缺失则写一份空片段),避免 import 指向不存在文件使 reload 失败。
// 该操作会改写用户主 Caddyfile,调用方须在 UI 层显式二次确认(本函数不负责交互)。
// 非 external 模式 → ErrNotExternalMode。
func InjectExternalImport(ctx context.Context, tg target.Service, serverID string, settings CaddySettings, apply bool) (*ExternalImportResult, error) {
	if settings.EffectiveMode() != modeExternal {
		return nil, ErrNotExternalMode
	}
	cfg := settings.EffectiveConfigPath()
	frag := settings.FragmentPath()
	line := "import " + frag
	res := &ExternalImportResult{File: cfg, Line: line}

	catRes, err := tg.Exec(ctx, serverID, []string{"cat", cfg})
	if err != nil {
		return nil, mapExecErr(err)
	}
	if catRes.ExitCode != 0 {
		return nil, fmt.Errorf("%w: 外接模式要求主 Caddyfile(%s)存在且可读,当前读取失败:%s", ErrExternalConfigImport, cfg, strings.TrimSpace(firstNonEmpty(catRes.Stderr, "exit "+strconv.Itoa(catRes.ExitCode))))
	}
	gr, gerr := tg.Exec(ctx, serverID, []string{"grep", "-q", "--", frag, cfg})
	if gerr != nil {
		return nil, mapExecErr(gerr)
	}
	res.Present = gr.ExitCode == 0
	if !apply {
		return res, nil
	}
	if res.Present {
		res.Applied = false
		return res, nil
	}
	// 确保片段目录存在 + 片段文件存在(缺失才写空片段),避免 import 指向缺失文件使 reload 失败。
	if mkRes, mkErr := tg.Exec(ctx, serverID, []string{"mkdir", "-p", settings.EffectiveFragmentDir()}); mkErr != nil {
		return nil, mapExecErr(mkErr)
	} else if mkRes.ExitCode != 0 {
		return nil, fmt.Errorf("%w:%s", ErrApply, strings.TrimSpace(firstNonEmpty(mkRes.Stderr, mkRes.Stdout)))
	}
	testRes, testErr := tg.Exec(ctx, serverID, []string{"test", "-f", frag})
	if testErr != nil {
		return nil, mapExecErr(testErr)
	}
	if testRes.ExitCode != 0 {
		if wErr := writeExternalFragment(ctx, tg, serverID, "", settings); wErr != nil {
			return nil, wErr
		}
	}
	newContent := catRes.Stdout
	if !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}
	newContent += line + "\n"
	if upErr := tg.Upload(ctx, serverID, strings.NewReader(newContent), cfg); upErr != nil {
		return nil, mapExecErr(upErr)
	}
	res.Applied = true
	// reload(best-effort):失败不回滚已写入的 import,但把告警带回前端。
	rl, rlErr := tg.Exec(ctx, serverID, settings.EffectiveReloadArgs())
	if rlErr != nil {
		return nil, mapExecErr(rlErr)
	}
	if rl.ExitCode != 0 {
		res.ReloadWarning = strings.TrimSpace(firstNonEmpty(rl.Stderr, rl.Stdout))
	}
	return res, nil
}

// enrichExternalApplyErr 在外部模式 apply 失败时,若配置用到了 stock caddy 不含的插件指令
// (目前仅 caddy-l4 的 layer4),追加「外接 Caddy 可能未含对应插件」的提示,
// 让用户明白为何报 unknown directive,而非面对一段难懂的 Caddy 报错。
func enrichExternalApplyErr(content string, base error) error {
	if base == nil {
		return nil
	}
	if !caddyfileNeedsPlugins(content) {
		return base
	}
	return fmt.Errorf("%s —— 配置用到了 TCP 透传(layer4),而当前外接 Caddy 可能未含 caddy-l4 插件;如需该能力请为你的 Caddy 加装 mholt/caddy-l4 插件,或经 ATOMGO_CADDY_IMAGE 指向一个自带该插件的镜像", base.Error())
}

// ProxyModeInfo 是反代运行模式的对外只读快照(供前端展示外接模式的路径与 reload 命令)。
type ProxyModeInfo struct {
	Mode        string `json:"mode"`
	ConfigPath  string `json:"configPath"`
	FragmentDir string `json:"fragmentDir"`
	ReloadCmd   string `json:"reloadCmd"`
}

// CaddySettings 是一台目标主机的反代运行模式及其外接路径(档位 B:可按服务器覆盖全局缺省)。
// 全局 env(ATOMGO_PROXY_MODE / ATOMGO_CADDY_EXTERNAL_*)是进程级缺省;server_proxy_settings 表可按
// 服务器覆盖。空字段表示「回退全局缺省」(覆盖逐字段生效)。
type CaddySettings struct {
	Mode        string // modeManaged | modeExternal;空 → 全局缺省。
	ConfigPath  string // 外接:用户主 Caddyfile 路径;空 → 全局缺省。
	FragmentDir string // 外接:AtomGo 写反代片段的目录;空 → 全局缺省。
	ReloadCmd   string // 外接:触发 reload 的命令(空格分隔);空 → 全局缺省(caddy reload)。
}

// globalCaddySettings 由进程级 env 取「全局缺省」反代设置(向后兼容:未配 server_proxy_settings 时即此值)。
func globalCaddySettings() CaddySettings {
	return CaddySettings{
		Mode:        proxyMode(),
		ConfigPath:  externalConfigPath(),
		FragmentDir: externalFragmentDir(),
		ReloadCmd:   strings.Join(externalReloadArgs(), " "),
	}
}

// EffectiveMode 返回生效模式(空 / 非 external → managed)。
func (s CaddySettings) EffectiveMode() string {
	if s.Mode == modeExternal {
		return modeExternal
	}
	return modeManaged
}

// EffectiveConfigPath 返回生效主 Caddyfile 路径(空 → 默认)。
func (s CaddySettings) EffectiveConfigPath() string {
	if strings.TrimSpace(s.ConfigPath) != "" {
		return s.ConfigPath
	}
	return defaultExternalConfigPath
}

// EffectiveFragmentDir 返回生效片段目录(空 → 默认)。
func (s CaddySettings) EffectiveFragmentDir() string {
	if strings.TrimSpace(s.FragmentDir) != "" {
		return s.FragmentDir
	}
	return defaultExternalFragmentDir
}

// EffectiveReloadArgs 返回生效 reload 命令(argv);空 → `caddy reload --config <主> --adapter caddyfile`。
func (s CaddySettings) EffectiveReloadArgs() []string {
	if strings.TrimSpace(s.ReloadCmd) != "" {
		return strings.Fields(s.ReloadCmd)
	}
	return []string{"caddy", "reload", "--config", s.EffectiveConfigPath(), "--adapter", "caddyfile"}
}

// FragmentPath 返回 AtomGo 写反代片段的完整主机路径(目录 + 片段文件名)。
func (s CaddySettings) FragmentPath() string {
	return s.EffectiveFragmentDir() + "/" + externalFragmentFile
}

// GetProxyModeInfo 返回当前反代模式及外接模式相关的路径/命令(全局只读,无副作用)。
func GetProxyModeInfo() ProxyModeInfo {
	g := globalCaddySettings()
	return ProxyModeInfo{
		Mode:        g.EffectiveMode(),
		ConfigPath:  g.EffectiveConfigPath(),
		FragmentDir: g.EffectiveFragmentDir(),
		ReloadCmd:   strings.Join(g.EffectiveReloadArgs(), " "),
	}
}

// firstNonEmpty 返回首个非空白字符串。
func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// mapExecErr 把 target.Exec/Upload 的传输层错误透传(领域错误已是人话;由上层 humanize)。
func mapExecErr(err error) error {
	if err == nil {
		return nil
	}
	return err
}
