package proxy

import "testing"

// TestCaddyfileNeedsPlugins 校验「stock caddy 能力边界」探测:
// 纯 HTTP-01 配置不应报需要插件;含 layer4(TCP 透传)的配置应报需要插件。
// (DNS-01 / 通配符已随 DNS 提供商集成移除,不再检测。)
func TestCaddyfileNeedsPlugins(t *testing.T) {
	// 纯 HTTP-01 路由(无 layer4)→ 不需要插件。
	httpOnly := renderCaddyfile([]Route{{
		Domain: "app.example.com", UpstreamContainer: "web", UpstreamPort: 8080, Enabled: true,
	}})
	if caddyfileNeedsPlugins(httpOnly) {
		t.Fatalf("纯 HTTP-01 配置不应报需要插件:\n%s", httpOnly)
	}

	// TCP 透传(layer4 全局块)→ 需要插件(caddy-l4)。
	tcp := renderCaddyfile([]Route{{
		Domain: "app.example.com", UpstreamContainer: "web", UpstreamPort: 8080, Enabled: true,
		Config: RouteConfig{TCPPassthrough: &TCPConfig{ListenPort: 5432, UpstreamContainer: "db", UpstreamPort: 5432}},
	}})
	if !caddyfileNeedsPlugins(tcp) {
		t.Fatalf("含 layer4 的配置应报需要插件:\n%s", tcp)
	}
}

// TestProxyImageIsStockCaddy 校验「当前镜像是否为官方 stock caddy」的判定(用于早拒提示)。
func TestProxyImageIsStockCaddy(t *testing.T) {
	cases := map[string]bool{
		"myregistry/caddy-l4:2":            false, // 含 caddy-l4 等插件的自定义镜像
		"caddy:2":                          true,  // 官方 stock
		"caddy":                            true,  // 官方 stock(latest)
		"caddy:latest":                     true,  // 官方 stock
		"docker.io/library/caddy:2":        true,  // 官方 stock 全限定
		"ghcr.io/foo/caddy-with-plugins:2": false, // 第三方含插件镜像,不应误伤
	}
	for ref, want := range cases {
		t.Setenv(caddyImageEnv, ref)
		if got := proxyImageIsStockCaddy(); got != want {
			t.Errorf("镜像 %q → stock=%v, want %v", ref, got, want)
		}
	}
}
