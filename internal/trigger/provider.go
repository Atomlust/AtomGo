package trigger

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
)

// Provider 是支持的源码托管平台。
//
// 设计要点:**一个项目只有一套 webhook URL 与签名密钥**(`pipeline_triggers.project_id`
// 为主键),各平台在各自仓库里配置同一个 URL 与密钥,由本包按投递头自动识别来源平台,
// 并按该平台的签名契约校验。因此同一个密钥会被多个平台共用——泄露影响面也随之扩大,
// 这是刻意的取舍(换取「配一次就完事」的体验)。
type Provider string

const (
	ProviderGitee  Provider = "gitee"
	ProviderGitHub Provider = "github"
	ProviderGitLab Provider = "gitlab"
	ProviderGitea  Provider = "gitea"
)

// 验签模式标识;与 receiver.go 的 modeNone / modePassword / modeSignature 同包共用。
const (
	// modeToken:「明文 token 头 == 密钥」模式(GitLab Secret token)。
	modeToken = "token"
	// modeHMACBody:「HMAC-SHA256(body) 十六进制」模式(GitHub / Gitea)。
	modeHMACBody = "hmac_body"
)

// 各平台的投递头契约(冻结:不得随意改名,改名等于对该平台断连)。
const (
	// Gitee 的头常量沿用 receiver.go 的既有定义(此处不再重复声明)。

	// GitHub
	HeaderGitHubEvent     = "X-GitHub-Event"
	HeaderGitHubSignature = "X-Hub-Signature-256"
	HeaderGitHubDelivery  = "X-GitHub-Delivery"

	// GitLab
	HeaderGitLabEvent    = "X-Gitlab-Event"
	HeaderGitLabToken    = "X-Gitlab-Token"
	HeaderGitLabDelivery = "X-Gitlab-Event-UUID"

	// Gitea(兼容 Gogs —— 两者头前缀不同但签名算法与 payload 形态一致)
	HeaderGiteaEvent     = "X-Gitea-Event"
	HeaderGiteaSignature = "X-Gitea-Signature"
	HeaderGiteaDelivery  = "X-Gitea-Delivery"

	HeaderGogsEvent     = "X-Gogs-Event"
	HeaderGogsSignature = "X-Gogs-Signature"
	HeaderGogsDelivery  = "X-Gogs-Delivery"
)

// SniffProvider 按投递头判定来源平台;识别不出 → ok=false。
//
// 判定只看「谁的事件头存在」,不依赖 body,因此无需解密/验签即可完成分派。
// Gogs 归类为 gitea(头契约不同,但签名算法与载荷形态一致)。
func SniffProvider(h http.Header) (Provider, bool) {
	get := func(k string) string { return strings.TrimSpace(h.Get(k)) }
	switch {
	case get(HeaderGiteeEvent) != "":
		return ProviderGitee, true
	case get(HeaderGitHubEvent) != "":
		return ProviderGitHub, true
	case get(HeaderGitLabEvent) != "":
		return ProviderGitLab, true
	case get(HeaderGiteaEvent) != "", get(HeaderGogsEvent) != "":
		return ProviderGitea, true
	default:
		return "", false
	}
}

// ExtractDelivery 按 provider 的头契约取出构造 Delivery 所需的四个值。
//   - credential:各平台语义不同(Gitee/GitLab 是明文或派生 token 头;GitHub/Gitea 是签名头),
//     统一放 Delivery.Signature(GitHub/Gitea)或 Delivery.TokenHdr(Gitee/GitLab)。
//   - timestamp:仅 Gitee 契约携带,其余平台为空。
func ExtractDelivery(h http.Header) (p Provider, event, credential, timestamp, deliveryID string, ok bool) {
	get := func(k ...string) string {
		for _, key := range k {
			if v := strings.TrimSpace(h.Get(key)); v != "" {
				return v
			}
		}
		return ""
	}
	p, ok = SniffProvider(h)
	if !ok {
		return "", "", "", "", "", false
	}
	switch p {
	case ProviderGitee:
		return p, get(HeaderGiteeEvent), get(HeaderGiteeToken), get(HeaderGiteeTimestamp), get(HeaderGiteeDelivery), true
	case ProviderGitHub:
		return p, get(HeaderGitHubEvent), get(HeaderGitHubSignature), "", get(HeaderGitHubDelivery), true
	case ProviderGitLab:
		return p, get(HeaderGitLabEvent), get(HeaderGitLabToken), "", get(HeaderGitLabDelivery), true
	default: // ProviderGitea(含 Gogs)
		return p,
			get(HeaderGiteaEvent, HeaderGogsEvent),
			get(HeaderGiteaSignature, HeaderGogsSignature),
			"",
			get(HeaderGiteaDelivery, HeaderGogsDelivery),
			true
	}
}

// verifySignatureFor 按平台契约校验投递签名;返回 (命中模式, 是否通过)。
// secret 为空一律失败。Gitee 走既有实现(密码模式 / 签名模式),行为完全不变。
func verifySignatureFor(p Provider, d Delivery, secret []byte) (string, bool) {
	switch p {
	case ProviderGitee, "": // "" = 未标注来源(历史调用/测试),回落 Gitee 以保持向后兼容
		return verifySignature(d, secret) // 既有:Gitee 密码模式 + HMAC(timestamp+"\n"+secret)
	case ProviderGitLab:
		return verifyTokenPlain(d.TokenHdr, secret)
	case ProviderGitHub:
		return verifyHMACBody(d.Signature, secret, d.RawBody, true) // 必须 sha256= 前缀
	case ProviderGitea:
		return verifyHMACBody(d.Signature, secret, d.RawBody, false) // 裸 hex,容错带前缀
	default:
		return modeNone, false
	}
}

// verifyTokenPlain 校验「明文 token 头 == 密钥」的平台契约(GitLab Secret token,
// 与 Gitee 密码模式同形,但走独立 mode,便于区分来源与时间戳策略)。
func verifyTokenPlain(hdr string, secret []byte) (string, bool) {
	if len(secret) == 0 {
		return modeNone, false
	}
	got := []byte(strings.TrimSpace(hdr))
	if len(got) == 0 {
		return modeNone, false
	}
	if subtle.ConstantTimeCompare(got, secret) == 1 {
		return modeToken, true
	}
	return modeNone, false
}

// verifyHMACBody 校验 HMAC-SHA256(key=secret, msg=rawBody) 的十六进制签名。
//
//   - requirePrefix=true(GitHub):必须是 "sha256=<hex>" 形式,否则失败。
//   - requirePrefix=false(Gitea):裸 hex;同时容错带 "sha256=" 前缀的实现。
func verifyHMACBody(hdr string, secret, body []byte, requirePrefix bool) (string, bool) {
	if len(secret) == 0 {
		return modeNone, false
	}
	got := strings.TrimSpace(hdr)
	if got == "" {
		return modeNone, false
	}
	if requirePrefix && !strings.HasPrefix(got, "sha256=") {
		return modeNone, false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	candidate := strings.TrimSpace(strings.TrimPrefix(got, "sha256="))
	if subtle.ConstantTimeCompare([]byte(candidate), []byte(want)) == 1 {
		return modeHMACBody, true
	}
	return modeNone, false
}

// resolveEvent 把平台原生事件名归一化为内部四类事件,并对 PR/MR 事件应用 action 白名单。
//
// 返回 (内部事件名, 是否处理):第二值为 false 表示该事件不被本系统处理
// (不支持的事件 / PR 的元数据型 action),调用方按「忽略」处理。
func resolveEvent(p Provider, event string, body []byte) (string, string) {
	event = strings.TrimSpace(event)
	var internal string
	switch p {
	case ProviderGitee, ProviderGitLab, "": // "" = 未标注来源,回落 Gitee 语义
		// Gitee 与 GitLab 事件名一致(Xxx Hook 形式),可直接对应。
		switch event {
		case eventPush:
			internal = eventPush
		case eventTag:
			internal = eventTag
		case eventMergeRequest:
			internal = eventMergeRequest
		case eventRelease:
			internal = eventRelease
		default:
			return "", IgnoredUnsupportedEvent
		}
	case ProviderGitHub, ProviderGitea:
		switch event {
		case "push":
			// GitHub/Gitea 的 tag 推送同样是 push 事件,按 ref 前缀区分。
			if strings.HasPrefix(jsonRootString(body, "ref"), "refs/tags/") {
				internal = eventTag
			} else {
				internal = eventPush
			}
		case "create":
			// create 覆盖建分支与建 tag,只有 tag 值得触发。
			if jsonRootString(body, "ref_type") == "tag" {
				internal = eventTag
			} else {
				return "", IgnoredUnsupportedEvent
			}
		case "pull_request":
			internal = eventMergeRequest
		case "release":
			internal = eventRelease
		default:
			return "", IgnoredUnsupportedEvent
		}
	default:
		return "", IgnoredUnsupportedEvent
	}

	// PR/MR 事件:只放行「代码有变化」的三类动作,忽略标签/指派/改描述/关闭等管理动作。
	if internal == eventMergeRequest && !prActionAllowed(p, body) {
		return internal, IgnoredPRActionIgnored
	}
	return internal, ""
}

// prActionAllowed 判定 PR/MR 事件的 action 是否应当触发流水线。
//
// 放行:创建、重开、有新提交。其余(labeled/assigned/review_requested/edited/closed…)
// 一律忽略 —— 这些动作代码未变,放行会导致大量无意义构建。
//
// **取不到 action 字段时放行**(诚实降级):宁可多跑一次,也不静默吞掉推送。
func prActionAllowed(p Provider, body []byte) bool {
	var action string
	switch p {
	case ProviderGitLab:
		action = jsonRootString(body, "object_attributes", "action")
	default:
		action = jsonRootString(body, "action")
	}
	if action == "" {
		return true
	}
	switch action {
	case "opened", "open",
		"reopened", "reopen",
		"synchronize", "update":
		return true
	default:
		return false
	}
}

// jsonRootString 按路径取 JSON 根对象的字符串字段(非法/缺失/非字符串 → 空串)。
// 用于读 action / ref / ref_type 等顶层或一层嵌套字段;不做完整结构化解析,
// 因为此处只需少数几个字段,且必须容忍各平台载荷差异。
func jsonRootString(body []byte, path ...string) string {
	var cur any
	if err := json.Unmarshal(body, &cur); err != nil {
		return ""
	}
	for _, key := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur, ok = m[key]
		if !ok {
			return ""
		}
	}
	s, ok := cur.(string)
	if !ok {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(s))
}

// actorFor 返回投递来源平台的名称,写进运行的 actor 字段(运行列表用于展示触发来源)。
// Provider 为空(历史/内部调用)时回落 gitee,保持既有行为。
func actorFor(p Provider) string {
	if p == "" {
		return string(ProviderGitee)
	}
	return string(p)
}
