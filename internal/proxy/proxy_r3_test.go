package proxy

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/atomlust/atomgo/internal/storetest"
	"github.com/atomlust/atomgo/internal/target"
)

// --- 渲染:路径路由 golden -------------------------------------------------

func TestRenderPathRules(t *testing.T) {
	out := renderCaddyfile([]Route{{
		Domain: "app.example.com", UpstreamContainer: "web", UpstreamPort: 8080,
		Config: RouteConfig{PathRules: []PathRule{
			{Path: "/api/*", UpstreamContainer: "api_c", UpstreamPort: 8080},
			{Path: "/static/*", UpstreamContainer: "cdn_c", UpstreamPort: 80},
		}},
	}})
	for _, want := range []string{
		"    handle /api/* {\n        reverse_proxy api_c:8080\n    }\n",
		"    handle /static/* {\n        reverse_proxy cdn_c:80\n    }\n",
		"    handle {\n        reverse_proxy web:8080\n    }\n",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("路径路由块缺 %q:\n%s", want, out)
		}
	}
	// 有 PathRules 时不应再有裸 reverse_proxy(全部走 handle)。
	if strings.Contains(out, "\n    reverse_proxy web:8080\n") {
		t.Fatalf("有 PathRules 时主上游应在默认 handle 内,不应裸 reverse_proxy:\n%s", out)
	}
}

// --- 校验:通配符已不再支持(需 DNS-01,而 DNS 提供商已移除) -----------------

func TestWildcardRejected(t *testing.T) {
	ctx := context.Background()
	st := storetest.OpenDB(t)
	ft := &fakeTarget{}
	svc := &service{store: NewStore(st), tg: ft, prober: fakeProber{}}

	_, err := svc.Create(ctx, CreateInput{
		ServerID: "srv-1", Domain: "*.example.com", UpstreamContainer: "web", UpstreamPort: 80,
	})
	if err == nil || err != ErrWildcardUnsupported {
		t.Fatalf("通配符应报 ErrWildcardUnsupported, got %v", err)
	}
	if len(ft.execCalls) != 0 {
		t.Fatalf("非法早拒不应触网, got %d exec calls", len(ft.execCalls))
	}
}

func TestWildcardAliasRejected(t *testing.T) {
	ctx := context.Background()
	st := storetest.OpenDB(t)
	s := NewStore(st)
	r := newRoute(CreateInput{ServerID: "srv-1", Domain: "app.example.com", UpstreamContainer: "web", UpstreamPort: 80})
	if err := s.insert(ctx, r); err != nil {
		t.Fatalf("insert: %v", err)
	}
	svc := &service{store: s, tg: &fakeTarget{}, prober: fakeProber{}}
	_, err := svc.Update(ctx, r.ID, UpdateInput{Config: RouteConfig{Aliases: []string{"*.example.com"}}})
	if err == nil || err != ErrWildcardUnsupported {
		t.Fatalf("通配符别名应报 ErrWildcardUnsupported, got %v", err)
	}
}

func TestUpdateRejectsBadPathRule(t *testing.T) {
	ctx := context.Background()
	st := storetest.OpenDB(t)
	s := NewStore(st)
	r := newRoute(CreateInput{ServerID: "srv-1", Domain: "app.example.com", UpstreamContainer: "web", UpstreamPort: 80})
	if err := s.insert(ctx, r); err != nil {
		t.Fatalf("insert: %v", err)
	}
	svc := &service{store: s, tg: &fakeTarget{}, prober: fakeProber{}}

	bad := []PathRule{
		{Path: "api/*", UpstreamContainer: "api_c", UpstreamPort: 8080},   // 未以 / 起
		{Path: "/api/ x", UpstreamContainer: "api_c", UpstreamPort: 8080}, // 含空白
		{Path: "/api/*", UpstreamContainer: "-evil", UpstreamPort: 8080},  // 容器 flag 注入
		{Path: "/api/*", UpstreamContainer: "api_c", UpstreamPort: 70000}, // 端口越界
	}
	for i, pr := range bad {
		_, err := svc.Update(ctx, r.ID, UpdateInput{Config: RouteConfig{PathRules: []PathRule{pr}}})
		if err != ErrInvalidPathRule {
			t.Fatalf("第 %d 条非法路径规则应报 ErrInvalidPathRule, got %v", i, err)
		}
	}
}

// --- 自定义镜像:docker pull 配置镜像 + env 覆盖 --------------------------

func TestEnsureCaddyPullsConfiguredImage(t *testing.T) {
	ctx := context.Background()
	st := storetest.OpenDB(t)
	ft := &fakeTarget{
		resultFor: func(cmd []string) *target.ExecResult {
			if len(cmd) >= 2 && cmd[0] == "docker" && cmd[1] == "inspect" {
				return &target.ExecResult{ExitCode: 1, Stderr: "No such object"}
			}
			return nil
		},
	}
	svc := &service{store: NewStore(st), tg: ft, prober: fakeProber{}}
	if _, err := svc.Create(ctx, CreateInput{ServerID: "srv-1", Domain: "a.example.com", UpstreamContainer: "web", UpstreamPort: 80}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	joined := make([]string, len(ft.execCalls))
	for i, c := range ft.execCalls {
		joined[i] = cmdJoin(c)
	}
	// 默认镜像被 pull,且 docker run 用同一镜像。
	if firstIndexWithPrefix(joined, "docker pull "+defaultCaddyImage) == -1 {
		t.Fatalf("应 docker pull 默认镜像 %s:\n%s", defaultCaddyImage, strings.Join(joined, "\n"))
	}
	runIdx := firstIndexWithPrefix(joined, "docker run -d --name atomgo-caddy")
	pullIdx := firstIndexWithPrefix(joined, "docker pull "+defaultCaddyImage)
	if runIdx == -1 || pullIdx == -1 || pullIdx > runIdx {
		t.Fatalf("docker pull 应在 docker run 之前:pull=%d run=%d", pullIdx, runIdx)
	}
	// 起容器命令里应含默认镜像引用作为位置参数。
	if !sliceContains(ft.execCalls, defaultCaddyImage) {
		t.Fatalf("docker run 应使用默认镜像 %s", defaultCaddyImage)
	}
}

func TestEnsureCaddyEnvOverridesImage(t *testing.T) {
	const custom = "registry.example.com/myteam/caddy:2.8"
	t.Setenv(caddyImageEnv, custom)

	ctx := context.Background()
	st := storetest.OpenDB(t)
	ft := &fakeTarget{
		resultFor: func(cmd []string) *target.ExecResult {
			if len(cmd) >= 2 && cmd[0] == "docker" && cmd[1] == "inspect" {
				return &target.ExecResult{ExitCode: 1}
			}
			return nil
		},
	}
	svc := &service{store: NewStore(st), tg: ft, prober: fakeProber{}}
	if _, err := svc.Create(ctx, CreateInput{ServerID: "srv-1", Domain: "b.example.com", UpstreamContainer: "web", UpstreamPort: 80}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	joined := make([]string, len(ft.execCalls))
	for i, c := range ft.execCalls {
		joined[i] = cmdJoin(c)
	}
	if firstIndexWithPrefix(joined, "docker pull "+custom) == -1 {
		t.Fatalf("ATOMGO_CADDY_IMAGE 应被尊重 pull %s:\n%s", custom, strings.Join(joined, "\n"))
	}
	if !sliceContains(ft.execCalls, custom) {
		t.Fatalf("docker run 应使用覆盖镜像 %s", custom)
	}
}

func TestCaddyImageRefRejectsInjection(t *testing.T) {
	// 含 shell 元字符/空格的 env 值非法 → 回退默认(不让 env 注入 docker flag/参数)。
	for _, bad := range []string{"caddy:2 --privileged", "caddy:2; rm -rf /", "-evilflag", "caddy:2|sh"} {
		os.Setenv(caddyImageEnv, bad)
		if got := caddyImageRef(); got != defaultCaddyImage {
			t.Fatalf("非法镜像 env %q 应回退默认, got %q", bad, got)
		}
	}
	os.Unsetenv(caddyImageEnv)
}

// sliceContains 报告某条命令的某个参数恰为 want(用于断言镜像位置参数)。
func sliceContains(calls [][]string, want string) bool {
	for _, c := range calls {
		for _, a := range c {
			if a == want {
				return true
			}
		}
	}
	return false
}
