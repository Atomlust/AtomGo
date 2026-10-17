package proxy

import (
	"context"
	"strings"
	"testing"

	"github.com/atomlust/atomgo/internal/storetest"
	"github.com/atomlust/atomgo/internal/target"
)

// caddyAbsentTarget 返回一个 fakeTarget:docker inspect 恒报「不存在」(触发起容器路径),其余 exit0。
func caddyAbsentTarget() *fakeTarget {
	return &fakeTarget{
		resultFor: func(cmd []string) *target.ExecResult {
			if len(cmd) >= 2 && cmd[0] == "docker" && cmd[1] == "inspect" {
				return &target.ExecResult{ExitCode: 1, Stderr: "No such object"}
			}
			return nil
		},
	}
}

func joinedCalls(ft *fakeTarget) []string {
	out := make([]string, len(ft.execCalls))
	for i, c := range ft.execCalls {
		out[i] = cmdJoin(c)
	}
	return out
}

// TestPrepareCaddyDeploysWithAddHost 验证 PrepareCaddy 部署反代环境:建网络 + 起容器(含 --add-host
// host.docker.internal:host-gateway),返回部署后的状态。
func TestPrepareCaddyDeploysWithAddHost(t *testing.T) {
	ctx := context.Background()
	st := storetest.OpenDB(t)
	ft := caddyAbsentTarget()
	svc := &service{store: NewStore(st), tg: ft, prober: fakeProber{}}

	if _, err := svc.PrepareCaddy(ctx, "srv-1"); err != nil {
		t.Fatalf("PrepareCaddy: %v", err)
	}

	joined := joinedCalls(ft)
	mustHaveInOrder(t, joined, []string{
		"docker network inspect atomgo-proxy",
		"docker inspect atomgo-caddy",
		"docker run -d --name atomgo-caddy",
	})
	// 起容器命令必须带 --add-host host.docker.internal:host-gateway。
	runIdx := firstIndexWithPrefix(joined, "docker run -d --name atomgo-caddy")
	if runIdx < 0 || !strings.Contains(joined[runIdx], "--add-host host.docker.internal:host-gateway") {
		t.Fatalf("docker run 应含 --add-host host.docker.internal:host-gateway:\n%s", joined[runIdx])
	}
}

// TestPrepareCaddyEmptyServerID 验证空 serverID → ErrEmptyServerID。
func TestPrepareCaddyEmptyServerID(t *testing.T) {
	ctx := context.Background()
	st := storetest.OpenDB(t)
	svc := &service{store: NewStore(st), tg: &fakeTarget{}, prober: fakeProber{}}
	if _, err := svc.PrepareCaddy(ctx, "  "); err != ErrEmptyServerID {
		t.Fatalf("空 serverID 应报 ErrEmptyServerID, got %v", err)
	}
}

// TestPrepareCaddyWithResourcesPersistsAndDeploys 证「保存+部署」一步:部署请求携带资源覆盖时,
// 先落库(仅 4 个资源字段)再用覆盖后的名字部署(docker run --name <自定义>)。
func TestPrepareCaddyWithResourcesPersistsAndDeploys(t *testing.T) {
	ctx := context.Background()
	st := storetest.OpenDB(t)
	ft := caddyAbsentTarget()
	svc := &service{store: NewStore(st), tg: ft, prober: fakeProber{}}

	if _, err := svc.PrepareCaddyWithResources(ctx, "srv-1", CaddyResources{
		Network: "my-proxy-net", ContainerName: "my-caddy", DataVol: "my_data", ConfigVol: "my_config",
	}); err != nil {
		t.Fatalf("PrepareCaddyWithResources: %v", err)
	}

	// 部署用自定义容器名/网络。
	joined := joinedCalls(ft)
	mustHaveInOrder(t, joined, []string{
		"docker network inspect my-proxy-net",
		"docker inspect my-caddy",
		"docker run -d --name my-caddy",
	})
	// 资源覆盖已落库。
	got, err := NewStore(st).getServerProxySettings(ctx, "srv-1")
	if err != nil || got == nil {
		t.Fatalf("资源覆盖应已落库: got=%v err=%v", got, err)
	}
	if got.ContainerName != "my-caddy" || got.Network != "my-proxy-net" || got.DataVol != "my_data" || got.ConfigVol != "my_config" {
		t.Fatalf("落库资源不符: %+v", got)
	}
}

// TestPrepareCaddyWithResourcesPreservesOtherOverrides 证资源落库只动 4 个资源字段,
// 既有的外接覆盖字段(如 ExternalCaddyContainer)原样保留。
func TestPrepareCaddyWithResourcesPreservesOtherOverrides(t *testing.T) {
	ctx := context.Background()
	st := storetest.OpenDB(t)
	svc := &service{store: NewStore(st), tg: caddyAbsentTarget(), prober: fakeProber{}}

	// 预置一条含外接覆盖的设置行。
	if err := svc.SetServerCaddySettings(ctx, "srv-1", CaddySettings{
		Mode:                   modeExternal,
		ConfigPath:             "/etc/caddy/Caddyfile",
		ExternalCaddyContainer: "user-caddy",
	}); err != nil {
		t.Fatalf("预置设置: %v", err)
	}

	if _, err := svc.PrepareCaddyWithResources(ctx, "srv-1", CaddyResources{
		Network: "net-x", ContainerName: "ct-x",
	}); err != nil {
		t.Fatalf("PrepareCaddyWithResources: %v", err)
	}

	got, err := NewStore(st).getServerProxySettings(ctx, "srv-1")
	if err != nil || got == nil {
		t.Fatalf("读回设置: %v %v", got, err)
	}
	if got.Mode != modeExternal || got.ConfigPath != "/etc/caddy/Caddyfile" || got.ExternalCaddyContainer != "user-caddy" {
		t.Fatalf("非资源字段应保留: %+v", got)
	}
	if got.Network != "net-x" || got.ContainerName != "ct-x" {
		t.Fatalf("资源字段应更新: %+v", got)
	}
}

// TestPrepareCaddyWithResourcesInvalidName 证非法资源名(防 flag 注入)→ ErrInvalidCaddyResource 且不部署。
func TestPrepareCaddyWithResourcesInvalidName(t *testing.T) {
	ctx := context.Background()
	st := storetest.OpenDB(t)
	ft := caddyAbsentTarget()
	svc := &service{store: NewStore(st), tg: ft, prober: fakeProber{}}

	if _, err := svc.PrepareCaddyWithResources(ctx, "srv-1", CaddyResources{ContainerName: "--privileged"}); err != ErrInvalidCaddyResource {
		t.Fatalf("非法容器名应报 ErrInvalidCaddyResource, got %v", err)
	}
	if _, err := svc.PrepareCaddyWithResources(ctx, "srv-1", CaddyResources{Network: "a b"}); err != ErrInvalidCaddyResource {
		t.Fatalf("含空格网络名应报 ErrInvalidCaddyResource, got %v", err)
	}
	for _, c := range ft.execCalls {
		if len(c) > 0 && c[0] == "docker" && (c[1] == "run" || c[1] == "network") {
			t.Fatalf("校验失败不应触达 docker: %v", c)
		}
	}
}

// TestPrepareCaddyWithResourcesEmpty 证全空资源 = 不写库、直接按已存设置部署(等价 PrepareCaddy)。
func TestPrepareCaddyWithResourcesEmpty(t *testing.T) {
	ctx := context.Background()
	st := storetest.OpenDB(t)
	ft := caddyAbsentTarget()
	svc := &service{store: NewStore(st), tg: ft, prober: fakeProber{}}

	if _, err := svc.PrepareCaddyWithResources(ctx, "srv-1", CaddyResources{}); err != nil {
		t.Fatalf("全空资源应直接部署: %v", err)
	}
	joined := joinedCalls(ft)
	mustHaveInOrder(t, joined, []string{"docker run -d --name atomgo-caddy"})
	got, err := NewStore(st).getServerProxySettings(ctx, "srv-1")
	if err != nil {
		t.Fatalf("读设置: %v", err)
	}
	if got != nil {
		t.Fatalf("全空资源不应写库, got %+v", got)
	}
}

// TestPrepareCaddyPortConflict 验证 80/443 被占用时 PrepareCaddy 透出端口冲突错误。
func TestPrepareCaddyPortConflict(t *testing.T) {
	ctx := context.Background()
	st := storetest.OpenDB(t)
	ft := &fakeTarget{
		resultFor: func(cmd []string) *target.ExecResult {
			switch {
			case len(cmd) >= 2 && cmd[0] == "docker" && cmd[1] == "inspect":
				return &target.ExecResult{ExitCode: 1} // caddy 不存在
			case len(cmd) >= 1 && cmd[0] == "ss":
				return &target.ExecResult{ExitCode: 0, Stdout: "LISTEN 0 128 0.0.0.0:443 0.0.0.0:*"}
			}
			return nil
		},
	}
	svc := &service{store: NewStore(st), tg: ft, prober: fakeProber{}}
	if _, err := svc.PrepareCaddy(ctx, "srv-1"); err == nil || !strings.Contains(err.Error(), "端口被占用") {
		t.Fatalf("应报端口冲突, got %v", err)
	}
}

// TestCreateAddressUpstreamSkipsNetworkConnect 验证 address 类上游:不发 docker network connect,
// 且渲染的 Caddyfile 直接 reverse_proxy <host>:<port>。
func TestCreateAddressUpstreamSkipsNetworkConnect(t *testing.T) {
	ctx := context.Background()
	st := storetest.OpenDB(t)
	ft := caddyAbsentTarget()
	svc := &service{store: NewStore(st), tg: ft, prober: fakeProber{}}

	route, err := svc.Create(ctx, CreateInput{
		ServerID: "srv-1", Domain: "app.example.com",
		UpstreamContainer: "10.0.0.5", UpstreamPort: 9000, UpstreamKind: UpstreamKindAddress,
	})
	if err != nil {
		t.Fatalf("Create(address): %v", err)
	}
	if route.Config.UpstreamKind != UpstreamKindAddress {
		t.Fatalf("路由应记 UpstreamKind=address, got %q", route.Config.UpstreamKind)
	}

	// 绝不对 address 上游发 docker network connect。
	for _, j := range joinedCalls(ft) {
		if strings.HasPrefix(j, "docker network connect") {
			t.Fatalf("address 上游不应 docker network connect, but issued: %q", j)
		}
	}
	// 渲染的 Caddyfile 直接反代到 host:port。
	content := ft.uploadBytes["/tmp/atomgo-caddyfile"]
	if !strings.Contains(content, "reverse_proxy 10.0.0.5:9000") {
		t.Fatalf("address 上游应渲染 reverse_proxy 10.0.0.5:9000:\n%s", content)
	}
}

// TestCreateAddressUpstreamHostDockerInternal 验证 host.docker.internal 作为 address 上游被接受 + 渲染。
func TestCreateAddressUpstreamHostDockerInternal(t *testing.T) {
	ctx := context.Background()
	st := storetest.OpenDB(t)
	ft := caddyAbsentTarget()
	svc := &service{store: NewStore(st), tg: ft, prober: fakeProber{}}

	if _, err := svc.Create(ctx, CreateInput{
		ServerID: "srv-1", Domain: "host.example.com",
		UpstreamContainer: "host.docker.internal", UpstreamPort: 3000, UpstreamKind: UpstreamKindAddress,
	}); err != nil {
		t.Fatalf("Create(host.docker.internal): %v", err)
	}
	content := ft.uploadBytes["/tmp/atomgo-caddyfile"]
	if !strings.Contains(content, "reverse_proxy host.docker.internal:3000") {
		t.Fatalf("应渲染 reverse_proxy host.docker.internal:3000:\n%s", content)
	}
}

// TestCreateContainerUpstreamConnectsAndRenders 验证 container 类(默认 + 显式)上游:仍发
// docker network connect + 渲染容器名(回归)。
func TestCreateContainerUpstreamConnectsAndRenders(t *testing.T) {
	for _, kind := range []string{"", UpstreamKindContainer} {
		t.Run("kind="+kind, func(t *testing.T) {
			ctx := context.Background()
			st := storetest.OpenDB(t)
			ft := caddyAbsentTarget()
			svc := &service{store: NewStore(st), tg: ft, prober: fakeProber{}}

			if _, err := svc.Create(ctx, CreateInput{
				ServerID: "srv-1", Domain: "app.example.com",
				UpstreamContainer: "web", UpstreamPort: 8080, UpstreamKind: kind,
			}); err != nil {
				t.Fatalf("Create(container): %v", err)
			}
			joined := joinedCalls(ft)
			if firstIndexWithPrefix(joined, "docker network connect atomgo-proxy web") < 0 {
				t.Fatalf("container 上游应 docker network connect web:\n%s", strings.Join(joined, "\n"))
			}
			content := ft.uploadBytes["/tmp/atomgo-caddyfile"]
			if !strings.Contains(content, "reverse_proxy web:8080") {
				t.Fatalf("container 上游应渲染 reverse_proxy web:8080:\n%s", content)
			}
		})
	}
}

// TestValidateUpstreamRules 集中验证 address/container 上游的校验规则。
func TestValidateUpstreamRules(t *testing.T) {
	cases := []struct {
		name    string
		kind    string
		host    string
		port    int
		wantErr error
	}{
		{"address-ipv4-ok", UpstreamKindAddress, "10.0.0.5", 80, nil},
		{"address-ipv6-ok", UpstreamKindAddress, "fd00::1", 80, nil},
		{"address-fqdn-ok", UpstreamKindAddress, "svc.internal.example.com", 8080, nil},
		{"address-hostdocker-ok", UpstreamKindAddress, "host.docker.internal", 3000, nil},
		{"address-space-bad", UpstreamKindAddress, "host with space", 80, ErrInvalidUpstreamHost},
		{"address-semicolon-bad", UpstreamKindAddress, "evil;reverse_proxy x", 80, ErrInvalidUpstreamHost},
		{"address-brace-bad", UpstreamKindAddress, "host{}", 80, ErrInvalidUpstreamHost},
		{"address-colon-bad", UpstreamKindAddress, "10.0.0.5:80", 80, ErrInvalidUpstreamHost},
		{"address-slash-bad", UpstreamKindAddress, "10.0.0.5/foo", 80, ErrInvalidUpstreamHost},
		{"address-port-low", UpstreamKindAddress, "10.0.0.5", 0, ErrInvalidPort},
		{"address-port-high", UpstreamKindAddress, "10.0.0.5", 70000, ErrInvalidPort},
		{"container-ok", UpstreamKindContainer, "web_1", 8080, nil},
		{"container-bad", UpstreamKindContainer, "-flag", 8080, ErrInvalidContainer},
		{"unknown-kind", "weird", "web", 8080, ErrInvalidUpstreamKind},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := validateUpstream(c.kind, c.host, c.port); got != c.wantErr {
				t.Fatalf("validateUpstream(%q,%q,%d) = %v, want %v", c.kind, c.host, c.port, got, c.wantErr)
			}
		})
	}
}

// TestCreateAddressUpstreamRejectsBadHost 验证非法 address host 经 Create 早拒(不触网 + 不留库)。
func TestCreateAddressUpstreamRejectsBadHost(t *testing.T) {
	ctx := context.Background()
	st := storetest.OpenDB(t)
	ft := &fakeTarget{}
	svc := &service{store: NewStore(st), tg: ft, prober: fakeProber{}}

	_, err := svc.Create(ctx, CreateInput{
		ServerID: "srv-1", Domain: "app.example.com",
		UpstreamContainer: "bad host;rm", UpstreamPort: 80, UpstreamKind: UpstreamKindAddress,
	})
	if err != ErrInvalidUpstreamHost {
		t.Fatalf("非法 address host 应报 ErrInvalidUpstreamHost, got %v", err)
	}
	if len(ft.execCalls) != 0 {
		t.Fatalf("非法上游不应触网, got %d exec calls", len(ft.execCalls))
	}
	if routes, _ := svc.List(ctx, "srv-1"); len(routes) != 0 {
		t.Fatalf("非法上游不应留库, got %d 条", len(routes))
	}
}
