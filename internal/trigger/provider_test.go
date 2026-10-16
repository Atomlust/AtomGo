package trigger

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
)

// testSecret 是测试用签名密钥(形态与真实 webhook 密钥一致)。
func testSecret() []byte { return []byte("whsec_0123456789abcdef") }

// ghSignature 按 GitHub 契约计算 X-Hub-Signature-256 的值("sha256=" + hex HMAC(body))。
func ghSignature(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func headerOf(kv map[string]string) http.Header {
	h := http.Header{}
	for k, v := range kv {
		h.Set(k, v)
	}
	return h
}

// ─── 平台识别 ────────────────────────────────────────────────────────────────

func TestSniffProvider(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
		want    Provider
		wantOK  bool
	}{
		{"gitee", map[string]string{HeaderGiteeEvent: "Push Hook"}, ProviderGitee, true},
		{"github", map[string]string{HeaderGitHubEvent: "push"}, ProviderGitHub, true},
		{"gitlab", map[string]string{HeaderGitLabEvent: "Push Hook"}, ProviderGitLab, true},
		{"gitea", map[string]string{HeaderGiteaEvent: "push"}, ProviderGitea, true},
		{"gogs 归为 gitea", map[string]string{HeaderGogsEvent: "push"}, ProviderGitea, true},
		{"无法识别", map[string]string{"X-Unknown-Event": "push"}, "", false},
		{"全空", map[string]string{}, "", false},
	}
	for _, c := range cases {
		got, ok := SniffProvider(headerOf(c.headers))
		if got != c.want || ok != c.wantOK {
			t.Errorf("%s: got (%q,%v), want (%q,%v)", c.name, got, ok, c.want, c.wantOK)
		}
	}
}

func TestExtractDeliveryPerPlatform(t *testing.T) {
	cases := []struct {
		name           string
		headers        map[string]string
		wantP          Provider
		wantEvent      string
		wantCredential string
		wantTimestamp  string
		wantDeliveryID string
	}{
		{
			name: "gitee 取四个头",
			headers: map[string]string{
				HeaderGiteeEvent: "Push Hook", HeaderGiteeToken: "plain",
				HeaderGiteeTimestamp: "1700000000000", HeaderGiteeDelivery: "d-1",
			},
			wantP: ProviderGitee, wantEvent: "Push Hook", wantCredential: "plain",
			wantTimestamp: "1700000000000", wantDeliveryID: "d-1",
		},
		{
			name: "github 取签名头且无 timestamp",
			headers: map[string]string{
				HeaderGitHubEvent: "push", HeaderGitHubSignature: "sha256=abc",
				HeaderGitHubDelivery: "d-2", HeaderGiteeTimestamp: "ignored",
			},
			wantP: ProviderGitHub, wantEvent: "push", wantCredential: "sha256=abc",
			wantTimestamp: "", wantDeliveryID: "d-2",
		},
		{
			name: "gitlab 取 Secret token 与 Event-UUID",
			headers: map[string]string{
				HeaderGitLabEvent: "Push Hook", HeaderGitLabToken: "tok",
				HeaderGitLabDelivery: "uuid-1",
			},
			wantP: ProviderGitLab, wantEvent: "Push Hook", wantCredential: "tok",
			wantTimestamp: "", wantDeliveryID: "uuid-1",
		},
		{
			name: "gitea 优先 gitea 头",
			headers: map[string]string{
				HeaderGiteaEvent: "push", HeaderGiteaSignature: "deadbeef",
				HeaderGiteaDelivery: "d-3",
			},
			wantP: ProviderGitea, wantEvent: "push", wantCredential: "deadbeef",
			wantDeliveryID: "d-3",
		},
		{
			name: "gogs 复用 gitea 分支",
			headers: map[string]string{
				HeaderGogsEvent: "push", HeaderGogsSignature: "cafe",
				HeaderGogsDelivery: "d-4",
			},
			wantP: ProviderGitea, wantEvent: "push", wantCredential: "cafe",
			wantDeliveryID: "d-4",
		},
	}
	for _, c := range cases {
		p, event, credential, ts, deliveryID, ok := ExtractDelivery(headerOf(c.headers))
		if !ok {
			t.Fatalf("%s: ExtractDelivery 未能识别平台", c.name)
		}
		if p != c.wantP || event != c.wantEvent || credential != c.wantCredential ||
			ts != c.wantTimestamp || deliveryID != c.wantDeliveryID {
			t.Errorf("%s: got (%q,%q,%q,%q,%q), want (%q,%q,%q,%q,%q)", c.name,
				p, event, credential, ts, deliveryID,
				c.wantP, c.wantEvent, c.wantCredential, c.wantTimestamp, c.wantDeliveryID)
		}
	}
}

// ─── 验签分派 ────────────────────────────────────────────────────────────────

func TestVerifySignatureGitHubAndGitea(t *testing.T) {
	secret := testSecret()
	body := []byte(`{"ref":"refs/heads/main","after":"abc123"}`)
	sig := ghSignature(secret, body)
	rawHex := strings.TrimPrefix(sig, "sha256=")

	// GitHub:要求 sha256= 前缀。
	d := Delivery{Provider: ProviderGitHub, Signature: sig, RawBody: body}
	if mode, ok := verifySignatureFor(ProviderGitHub, d, secret); !ok || mode != modeHMACBody {
		t.Fatalf("合法 GitHub 签名应通过, got mode=%q ok=%v", mode, ok)
	}
	d.Signature = rawHex
	if _, ok := verifySignatureFor(ProviderGitHub, d, secret); ok {
		t.Fatal("GitHub 缺少 sha256= 前缀时不应通过")
	}
	// Gitea:裸 hex 或带前缀都接受。
	dGitea := Delivery{Provider: ProviderGitea, Signature: rawHex, RawBody: body}
	if _, ok := verifySignatureFor(ProviderGitea, dGitea, secret); !ok {
		t.Fatal("Gitea 应接受裸 hex 签名")
	}
	dGitea.Signature = sig
	if _, ok := verifySignatureFor(ProviderGitea, dGitea, secret); !ok {
		t.Fatal("Gitea 应容错接受 sha256= 前缀")
	}
	// 签名错误(换个密钥)一律拒绝。
	wrong := Delivery{Provider: ProviderGitHub, Signature: ghSignature([]byte("wrong"), body), RawBody: body}
	if _, ok := verifySignatureFor(ProviderGitHub, wrong, secret); ok {
		t.Fatal("错误签名不应通过")
	}
	// 签名必须绑定 body:改 body 后原签名失效。
	tampered := Delivery{Provider: ProviderGitHub, Signature: sig, RawBody: []byte(`{"ref":"refs/heads/other"}`)}
	if _, ok := verifySignatureFor(ProviderGitHub, tampered, secret); ok {
		t.Fatal("签名应对 body 生效,篡改后必须失败")
	}
}

func TestVerifyTokenGitLab(t *testing.T) {
	secret := testSecret()
	d := Delivery{Provider: ProviderGitLab, TokenHdr: string(secret)}
	if _, ok := verifySignatureFor(ProviderGitLab, d, secret); !ok {
		t.Fatal("GitLab 明文 Secret token 应通过")
	}
	d.TokenHdr = "wrong-token"
	if _, ok := verifySignatureFor(ProviderGitLab, d, secret); ok {
		t.Fatal("错误的 Secret token 不应通过")
	}
}

func TestTimestampFreshPerMode(t *testing.T) {
	// GitHub/Gitea(GitLab)契约不带 timestamp 头 → 必须放行,否则整个平台会被误判为重放。
	if !timestampFresh("", modeHMACBody) {
		t.Fatal("body HMAC 模式缺 timestamp 应放行(否则 GitHub/Gitea 永远被 401)")
	}
	if !timestampFresh("", modeToken) {
		t.Fatal("token 模式缺 timestamp 应放行(GitLab 契约不带)")
	}
	// Gitee 密码模式:历史行为,允许缺失。
	if !timestampFresh("", modePassword) {
		t.Fatal("密码模式缺 timestamp 应放行")
	}
	// Gitee 签名模式:仍要求 timestamp(且与历史行为一致)。
	if timestampFresh("", modeSignature) {
		t.Fatal("签名模式缺 timestamp 应拒绝(否则重放窗口失效)")
	}
}

// ─── 事件归一化与 PR action 白名单 ───────────────────────────────────────────

func TestResolveEventMapping(t *testing.T) {
	cases := []struct {
		name         string
		p            Provider
		event        string
		body         string
		wantInternal string
		wantSkip     string
	}{
		{"github push", ProviderGitHub, "push", `{"ref":"refs/heads/main"}`, eventPush, ""},
		{"github 推 tag 归为 tag", ProviderGitHub, "push", `{"ref":"refs/tags/v1.2.0"}`, eventTag, ""},
		{"github create tag", ProviderGitHub, "create", `{"ref_type":"tag"}`, eventTag, ""},
		{"github create branch 忽略", ProviderGitHub, "create", `{"ref_type":"branch"}`, "", IgnoredUnsupportedEvent},
		{"github release", ProviderGitHub, "release", `{}`, eventRelease, ""},
		{"github 不支持的事件", ProviderGitHub, "issues", `{}`, "", IgnoredUnsupportedEvent},
		{"gitea pull_request", ProviderGitea, "pull_request", `{"action":"opened"}`, eventMergeRequest, ""},
		{"gitlab Push Hook", ProviderGitLab, eventPush, `{}`, eventPush, ""},
		{"gitlab Tag Push Hook", ProviderGitLab, eventTag, `{}`, eventTag, ""},
		{"gitlab Release Hook", ProviderGitLab, eventRelease, `{}`, eventRelease, ""},
		{"gitee Push Hook(回归)", ProviderGitee, eventPush, `{}`, eventPush, ""},
		{"未标注来源回落 gitee", "", eventPush, `{}`, eventPush, ""},
	}
	for _, c := range cases {
		got, skip := resolveEvent(c.p, c.event, []byte(c.body))
		if got != c.wantInternal || skip != c.wantSkip {
			t.Errorf("%s: got (%q,%q), want (%q,%q)", c.name, got, skip, c.wantInternal, c.wantSkip)
		}
	}
}

func TestPRActionWhitelist(t *testing.T) {
	// GitHub / Gitea:action 在顶层。
	githubPass := []string{"opened", "reopened", "synchronize"}
	githubSkip := []string{"labeled", "unlabeled", "assigned", "review_requested", "edited", "closed", "converted_to_draft"}
	for _, a := range githubPass {
		if _, skip := resolveEvent(ProviderGitHub, "pull_request", []byte(`{"action":"`+a+`"}`)); skip != "" {
			t.Errorf("GitHub action=%q 应放行, got skip=%q", a, skip)
		}
	}
	for _, a := range githubSkip {
		if _, skip := resolveEvent(ProviderGitHub, "pull_request", []byte(`{"action":"`+a+`"}`)); skip != IgnoredPRActionIgnored {
			t.Errorf("GitHub action=%q 应被忽略, got skip=%q", a, skip)
		}
	}
	// GitLab:action 嵌在 object_attributes 里。
	for _, a := range []string{"open", "reopen", "update"} {
		if _, skip := resolveEvent(ProviderGitLab, eventMergeRequest, []byte(`{"object_attributes":{"action":"`+a+`"}}`)); skip != "" {
			t.Errorf("GitLab action=%q 应放行, got skip=%q", a, skip)
		}
	}
	for _, a := range []string{"merge", "close"} {
		if _, skip := resolveEvent(ProviderGitLab, eventMergeRequest, []byte(`{"object_attributes":{"action":"`+a+`"}}`)); skip != IgnoredPRActionIgnored {
			t.Errorf("GitLab action=%q 应被忽略, got skip=%q", a, skip)
		}
	}
	// 取不到 action 时放行(诚实降级:宁可多跑一次,也不静默吞掉推送)。
	if _, skip := resolveEvent(ProviderGitHub, "pull_request", []byte(`{}`)); skip != "" {
		t.Errorf("缺失 action 时应放行, got skip=%q", skip)
	}
}

func TestActorFor(t *testing.T) {
	if got := actorFor(ProviderGitHub); got != "github" {
		t.Errorf("github actor got %q", got)
	}
	if got := actorFor(ProviderGitLab); got != "gitlab" {
		t.Errorf("gitlab actor got %q", got)
	}
	// 空 Provider(历史调用)回落 gitee,保持向后兼容。
	if got := actorFor(""); got != "gitee" {
		t.Errorf("空 provider 应回落 gitee, got %q", got)
	}
}
