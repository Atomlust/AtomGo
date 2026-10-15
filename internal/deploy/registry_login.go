package deploy

// registry_login.go:部署镜像前在目标机自动 docker login(方案 C)。
//
// 根因:此前部署只向目标机发 `docker pull`,依赖目标机预先 `docker login` 过 registry;
// 未登录 → `no basic auth credentials`。对中小团队产品,这应自动化。
//
// 安全设计(对齐 build.push 的 docker login 与 AC-SEC):
//   - 口令经目标机内存临时文件 + `docker login --password-stdin` 注入,绝不进 argv / 目标机 ps / 部署日志;
//   - 临时文件落在 /dev/shm(内存 tmpfs,重启即失),登录后立即 rm;
//   - 仅当镜像引用主机与本环境 registry 主机一致才登录(跨仓晋升等场景跳过,避免登错仓库);
//   - 登录失败 → 返回人读错误(绝不含口令);凭据仅进程内存在,用完即弃。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/atomlust/atomgo/internal/pipeline"
	"github.com/atomlust/atomgo/internal/run"
	"github.com/atomlust/atomgo/internal/target"
	"github.com/atomlust/atomgo/internal/vault"
)

// registryCreds 是部署前在目标机执行 docker login 所需的登录信息。
// Password 为明文(经 vault 揭示),用过即弃,绝不日志 / 落库 / 出网。
type registryCreds struct {
	Host     string // registry 主机(无 scheme,可带端口),如 docker.atomlust.com
	Username string
	Password string
}

// RegistryCredentialResolver 解析某项目某环境(或按镜像主机回退)绑定的镜像仓库登录凭据,
// 供部署前自动 docker login 目标机。deploy 层经此接口解耦 settings / vault。
type RegistryCredentialResolver interface {
	// Resolve 返回登录凭据;envName 空时回退到「任一 registry URL 主机匹配 refHost 的环境」。
	// 无绑定 / 无凭据 / 保险库未配置 / 解析失败 → 返回 (nil, nil),调用方跳过自动登录
	// (沿用目标机既有登录;可能目标机已手动登录或仓库公开)。
	Resolve(ctx context.Context, projectID, envName, refHost string) (*registryCreds, error)
}

// settingsRegistryResolver 是 RegistryCredentialResolver 的默认实现:
// 组合 pipeline.SettingsService(取环境 registry 绑定)+ vault(揭示 user:password)。
type settingsRegistryResolver struct {
	settings pipeline.SettingsService
	vault    vault.Vault
}

// NewSettingsRegistryResolver 构造默认解析器(由 main 装配注入 deploy.Service)。
func NewSettingsRegistryResolver(settings pipeline.SettingsService, v vault.Vault) RegistryCredentialResolver {
	return &settingsRegistryResolver{settings: settings, vault: v}
}

// Resolve 见接口注释。
func (r *settingsRegistryResolver) Resolve(ctx context.Context, projectID, envName, refHost string) (*registryCreds, error) {
	if r.settings == nil || r.vault == nil || strings.TrimSpace(projectID) == "" {
		return nil, nil
	}
	st, err := r.settings.Get(ctx, projectID)
	if err != nil || st == nil {
		return nil, nil // 解析失败 → 跳过自动登录,不阻断部署
	}
	// 先按环境名精确定位;未命中(如手动触发未解析环境)→ 回退到「主机匹配 refHost」的环境。
	var reg *pipeline.ImageRegistry
	for i := range st.Environments {
		e := &st.Environments[i]
		if strings.TrimSpace(e.ImageRegistry.CredentialID) == "" || strings.TrimSpace(e.ImageRegistry.URL) == "" {
			continue
		}
		if e.Name == envName {
			reg = &e.ImageRegistry
			break
		}
		if reg == nil && hostOfRegistry(e.ImageRegistry.URL) == refHost {
			reg = &e.ImageRegistry // 回退候选
		}
	}
	if reg == nil {
		return nil, nil
	}
	secret, err := r.vault.Reveal(reg.CredentialID)
	if err != nil || secret == "" {
		return nil, nil
	}
	user, pass := splitRegistrySecret(secret)
	if pass == "" {
		return nil, nil
	}
	return &registryCreds{
		Host:     hostOfRegistry(reg.URL),
		Username: user,
		Password: pass,
	}, nil
}

// splitRegistrySecret 把 registry 凭据明文拆为 user / password:约定 "user:password";
// 无冒号 → 整串作口令、user 空(token 场景)。与 build.revealRegistryCred 同语义。
func splitRegistrySecret(secret string) (string, string) {
	if i := strings.Index(secret, ":"); i >= 0 {
		return strings.TrimSpace(secret[:i]), strings.TrimSpace(secret[i+1:])
	}
	return "", strings.TrimSpace(secret)
}

// hostOfRegistry 取 registry URL 的主机(去 scheme / 尾斜杠,保留端口)。空 → ""。
func hostOfRegistry(url string) string {
	h := strings.TrimSpace(url)
	h = strings.TrimPrefix(h, "http://")
	h = strings.TrimPrefix(h, "https://")
	h = strings.TrimRight(h, "/")
	return h
}

// parseRegistryHost 从镜像引用解析 registry 主机(无 scheme)。
// 首段含 '.' / ':' 或为首段 localhost → 视为 registry 主机;否则默认 docker.io。
// 例:docker.atomlust.com/atomlust/atomupdate:tag → docker.atomlust.com。
func parseRegistryHost(ref string) string {
	ref = strings.TrimSpace(ref)
	ref = strings.TrimPrefix(ref, "http://")
	ref = strings.TrimPrefix(ref, "https://")
	if i := strings.Index(ref, "/"); i > 0 {
		cand := ref[:i]
		if strings.ContainsAny(cand, ".:") || cand == "localhost" {
			return cand
		}
	}
	return "docker.io"
}

// shellQuote 把字符串包成单引号 shell 字面量(内部单引号转义),用于构造安全 sh -c 脚本。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// randToken 返回 16 字节随机 hex(用于临时文件名,避免猜测 / 冲突)。
func randToken() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// loginRegistryForImage 在拉取镜像前,自动 docker login 目标机(若本运行环境绑定了 registry 凭据
// 且主机匹配)。返回 (msg, ok):ok=false 时 msg 为失败人读原因(绝不含口令明文)。
//
// 解析链路:runID → run(项目 + 环境)→ settings 环境 registry → vault 揭示凭据。
// 任一环节缺失 / 不匹配 → 跳过自动登录,沿用目标机既有登录(可能已手动登录或仓库公开)。
func (s *service) loginRegistryForImage(ctx context.Context, srv *target.Server, a run.Artifact, ref string) (string, bool) {
	if s.registryResolver == nil {
		return "", true
	}
	refHost := parseRegistryHost(ref)
	projectID, env := "", ""
	if rn, err := s.runs.Get(ctx, a.RunID); err == nil && rn != nil {
		projectID = rn.ProjectID
		env = rn.Trigger.ResolvedEnvironment
	}
	creds, err := s.registryResolver.Resolve(ctx, projectID, env, refHost)
	if err != nil || creds == nil {
		return "", true
	}
	if creds.Host != refHost {
		// 主机不一致(如跨仓晋升):不登录错仓库,交由目标机既有登录或 pull 自身报错。
		return "", true
	}
	return s.dockerLoginOnTarget(ctx, srv, creds)
}

// dockerLoginOnTarget 在目标机安全执行 docker login:
//  1. 把口令经 stdin 写到目标机 /dev/shm 临时文件(target.Upload 走 RunWithStdin,口令不进 argv / 日志);
//  2. `docker login -u <user> --password-stdin < 文件`(口令从文件读,绝不进 argv / ps / 日志);
//  3. 登录后立即 rm 临时文件(尽力)。
//
// 返回 (msg, ok):ok=false 时 msg 为失败人读原因。
func (s *service) dockerLoginOnTarget(ctx context.Context, srv *target.Server, creds *registryCreds) (string, bool) {
	user := creds.Username
	pwPath := "/dev/shm/.atomgo-docker-login-" + randToken()

	// 1) 口令经 stdin 安全落到目标机内存临时文件(绝不进 argv / 日志)。
	if err := s.targets.Upload(ctx, srv.ID, strings.NewReader(creds.Password), pwPath); err != nil {
		return "镜像仓库登录失败:无法写入临时凭据文件:" + humanExecError(err), false
	}
	// 用完即弃本地明文引用。
	creds.Password = ""

	// 2) 登录(口令经 --password-stdin 从文件读,绝不进 argv)。
	script := fmt.Sprintf("docker login -u %s --password-stdin < %s", shellQuote(user), shellQuote(pwPath))
	if out, e := s.exec(ctx, srv.ID, []string{"sh", "-c", script}); e != nil || (out != nil && out.ExitCode != 0) {
		_, _ = s.exec(ctx, srv.ID, []string{"rm", "-f", pwPath}) // 3) 清理(尽力)
		if e != nil {
			return "镜像仓库登录失败:" + humanExecError(e), false
		}
		return "镜像仓库登录失败(凭据可能被吊销 / 权限不足):" + truncate(strings.TrimSpace(out.Stderr)), false
	}

	// 3) 清理临时文件(尽力;在 /dev/shm 内存,重启即失)。
	_, _ = s.exec(ctx, srv.ID, []string{"rm", "-f", pwPath})
	return "", true
}
