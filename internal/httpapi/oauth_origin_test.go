package httpapi

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRequestOriginDefault 默认(publicOrigin 为空)按请求直连推导,
// 且不信任客户端伪造的 X-Forwarded-*。
func TestRequestOriginDefault(t *testing.T) {
	r := newReqWithHost("GET", "http://localhost:7088/foo", "localhost:7088")
	if got := requestOrigin(r, ""); got != "http://localhost:7088" {
		t.Fatalf("default origin = %q, want http://localhost:7088", got)
	}

	// 即便伪造 X-Forwarded-Host/Proto,默认行为也应忽略,仍以 r.Host 为准。
	r2 := newReqWithHost("GET", "http://localhost:7088/foo", "localhost:7088")
	r2.Header.Set("X-Forwarded-Host", "evil.example.com")
	r2.Header.Set("X-Forwarded-Proto", "https")
	if got := requestOrigin(r2, ""); got != "http://localhost:7088" {
		t.Fatalf("forwarded headers must be ignored, got %q", got)
	}

	// TLS 在场 → https。
	r3 := newReqWithHost("GET", "https://localhost:7088/foo", "localhost:7088")
	r3.TLS = &tls.ConnectionState{}
	if got := requestOrigin(r3, ""); got != "https://localhost:7088" {
		t.Fatalf("tls origin = %q, want https://localhost:7088", got)
	}
}

// TestRequestOriginPublicOrigin 显式 publicOrigin 优先,且去除首尾空白与尾斜杠。
func TestRequestOriginPublicOrigin(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://ci.example.com", "https://ci.example.com"},
		{"https://ci.example.com/", "https://ci.example.com"},
		{" http://ci.example.com/ ", "http://ci.example.com"},
	}
	for _, c := range cases {
		r := newReqWithHost("GET", "http://localhost:7088/foo", "localhost:7088")
		if got := requestOrigin(r, c.in); got != c.want {
			t.Fatalf("requestOrigin(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	// 空/纯空白 publicOrigin 视为未配置,回退默认推导。
	r := newReqWithHost("GET", "http://localhost:7088/foo", "localhost:7088")
	if got := requestOrigin(r, "   "); strings.TrimSpace(got) == "" || got != "http://localhost:7088" {
		t.Fatalf("blank public origin must fall back, got %q", got)
	}
}

// newReqWithHost 构造带指定 Host 的请求。
func newReqWithHost(method, target, host string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	r.Host = host
	return r
}
