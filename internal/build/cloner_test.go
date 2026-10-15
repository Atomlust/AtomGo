package build

import (
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"
)

// TestClassifyCloneErr 验证底层 go-git 错误能被安全归类为 auth/network/ref,
// 且归类逻辑绝不把底层文本带进用户消息(消息恒为固定类别说明)。
func TestClassifyCloneErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want CloneErrorCategory
	}{
		{"nil", nil, CatUnknown},
		{"auth-required", transport.ErrAuthenticationRequired, CatAuth},
		{"auth-failed", transport.ErrAuthorizationFailed, CatAuth},
		{"ref-not-found", errors.New("reference not found"), CatRef},
		{"unknown-revision", errors.New("unknown revision"), CatRef},
		{"does-not-exist", errors.New("repository 'x' does not exist"), CatRef},
		{"net-op-error", &net.OpError{Op: "dial", Err: errors.New("i/o timeout")}, CatNetwork},
		{"no-such-host", errors.New("dial tcp: lookup github.com: no such host"), CatNetwork},
		{"connection-refused", errors.New("dial tcp 1.2.3.4:22: connection refused"), CatNetwork},
		{"io-timeout", errors.New("unexpected EOF / i/o timeout"), CatNetwork},
		{"tls-handshake", errors.New("TLS handshake timeout"), CatNetwork},
		{"opaque", errors.New("some opaque internal failure"), CatUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyCloneErr(c.err); got != c.want {
				t.Fatalf("classifyCloneErr(%v) = %s, want %s", c.err, got, c.want)
			}
		})
	}
}

// TestCloneErrorMessageSafe 验证用户消息不含底层错误文本(防 URL/路径/令牌泄漏)。
func TestCloneErrorMessageSafe(t *testing.T) {
	ce := &CloneError{Category: classifyCloneErr(errors.New("dial tcp: lookup secret.internal: no such host")), Cause: ErrCloneFailed}
	const leaked = "secret.internal"
	if msg := ce.Error(); msg == "" {
		t.Fatalf("empty clone message")
	} else if strings.Contains(msg, leaked) {
		t.Fatalf("clone message leaked sensitive host: %q", msg)
	}
}
