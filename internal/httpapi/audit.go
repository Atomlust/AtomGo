package httpapi

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/atomlust/atomgo/internal/audit"
	"github.com/atomlust/atomgo/internal/auth"
)

// auditActor 是「系统/无登录身份」路径的兜底操作者标识。已认证请求的真实操作人由
// recordAudit 从 context 的 *auth.User 覆盖;仅系统/后台路径(无会话用户)才用此默认值。
const auditActor = "admin"

// requestActor 返回当前请求的真实操作者用户名(多用户);context 无登录用户时
// 兜底为 "admin"(系统/后台路径)。用于晋升审批等需要真实身份而非硬编码常量的场景。
func requestActor(r *http.Request) string {
	if u := auth.UserFromContext(r.Context()); u != nil && u.Username != "" {
		return u.Username
	}
	return auditActor
}

// clientIP 从请求提取来源 IP,用于审计记录(非安全判定)。
//
// **默认只用 RemoteAddr**:AtomGo 常作单二进制直连暴露,无反代。若无条件采信
// X-Forwarded-For,任意客户端可发 `X-Forwarded-For: 1.2.3.4` 把伪造来源写进 append-only
// 的审计 ip 列,削弱 AC-SEC-03「不可篡改」的取证价值。仅当显式配置可信反代
// (ATOMGO_TRUST_PROXY=1/true)时才采信 XFF 首段。
func clientIP(r *http.Request) string {
	if trustForwardedHeader() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if i := strings.IndexByte(xff, ','); i >= 0 {
				return strings.TrimSpace(xff[:i])
			}
			return strings.TrimSpace(xff)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}

// trustForwardedHeader 报告是否采信 X-Forwarded-For(仅在显式配置可信反代时)。
func trustForwardedHeader() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("ATOMGO_TRUST_PROXY"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// recordAudit 在业务成功后追加一行审计;rec 为 nil 时静默跳过(不阻断业务)。
// 本地写入失败**不回滚业务**(审计不阻断),但必须**记日志**——否则敏感操作的审计缺口
// 完全静默,损害 AC-SEC-03 的「完整」保证。
//
// 审计 actor:若 context 中已注入真实登录用户(多用户),则以其用户名覆盖传入的
// Actor(即旧的硬编码 "admin" 常量在已认证请求下被真实操作人取代);未认证(系统/后台)
// 路径保持原值(默认 "admin")。这样无需逐个改写约 50 处审计调用点即可实现真实审计归因。
func recordAudit(ctx context.Context, rec audit.Recorder, e audit.Entry) {
	if u := auth.UserFromContext(ctx); u != nil {
		e.Actor = u.Username
	}
	if e.Actor == "" {
		e.Actor = "admin"
	}
	if rec == nil {
		return
	}
	if err := rec.Record(ctx, e); err != nil {
		log.Printf("[audit] 警告:写审计失败(action=%s target=%s/%s): %v", e.Action, e.TargetType, e.TargetID, err)
	}
}

// auditEntryDTO 是审计条目对外响应体(冻结契约;camelCase;detail 已脱敏,绝无明文)。
type auditEntryDTO struct {
	ID         string         `json:"id"`
	Timestamp  string         `json:"timestamp"` // RFC3339
	Actor      string         `json:"actor"`
	Action     string         `json:"action"`
	TargetType string         `json:"targetType"`
	TargetID   string         `json:"targetId"`
	Detail     map[string]any `json:"detail"`
	IP         string         `json:"ip"`
}

// auditListDTO 是审计列表响应体(冻结契约:entries + nextBefore)。
type auditListDTO struct {
	Entries    []auditEntryDTO `json:"entries"`
	NextBefore *string         `json:"nextBefore"` // null 表示到底
}

// toAuditEntryDTO 把领域 Record 转为契约 DTO(detail 已在写入时脱敏)。
func toAuditEntryDTO(r audit.Record) auditEntryDTO {
	detail := r.Detail
	if detail == nil {
		detail = map[string]any{}
	}
	return auditEntryDTO{
		ID:         r.ID,
		Timestamp:  r.Timestamp.UTC().Format(time.RFC3339),
		Actor:      r.Actor,
		Action:     r.Action,
		TargetType: r.TargetType,
		TargetID:   r.TargetID,
		Detail:     detail,
		IP:         r.IP,
	}
}

// makeListAuditHandler 返回 GET /api/audit handler(认证保护 + 分页 + 过滤;只读)。
// query:limit(默认 50,上限 200)· before(游标,上一页末条 id)· action · targetType。
func makeListAuditHandler(rec audit.Recorder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if rec == nil {
			writeError(w, http.StatusServiceUnavailable, "audit_unavailable", "审计服务未初始化")
			return
		}
		q := r.URL.Query()
		f := audit.ListFilter{
			Limit:      atoiDefault(q.Get("limit"), 0),
			Before:     strings.TrimSpace(q.Get("before")),
			Action:     strings.TrimSpace(q.Get("action")),
			TargetType: strings.TrimSpace(q.Get("targetType")),
		}
		res, err := rec.List(r.Context(), f)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
			return
		}
		entries := make([]auditEntryDTO, 0, len(res.Entries))
		for _, e := range res.Entries {
			entries = append(entries, toAuditEntryDTO(e))
		}
		var nextBefore *string
		if res.NextBefore != "" {
			nb := res.NextBefore
			nextBefore = &nb
		}
		writeJSON(w, http.StatusOK, auditListDTO{Entries: entries, NextBefore: nextBefore})
	}
}
