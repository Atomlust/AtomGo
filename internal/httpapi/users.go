package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/atomlust/atomgo/internal/audit"
	"github.com/atomlust/atomgo/internal/auth"
)

// userManagementService 是用户管理 handler 依赖的窄接口(由 *auth.Service 实现)。
type userManagementService interface {
	ListUsers() ([]auth.User, error)
	CreateUser(username, password, role string) (*auth.User, error)
	SetUserRole(id, role string) error
	DeleteUser(id string) error
	ResetUserPassword(id, newPassword string) error
}

// userDTO 是用户对外响应体(**绝不含口令哈希**)。
type userDTO struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// userListDTO 是用户列表响应体。
type userListDTO struct {
	Users []userDTO `json:"users"`
	Roles []string  `json:"roles"` // 可选角色(供前端下拉)
}

func toUserDTO(u auth.User) userDTO {
	return userDTO{ID: u.ID, Username: u.Username, Role: u.Role}
}

// requirePermission 端点级权限拦截:要求已认证用户拥有 perm。须套在 requireAuth 之后
// (依赖 context 中的 *auth.User)。admin 角色通配;否则按 role_permissions 判定。
func requirePermission(perm string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := auth.UserFromContext(r.Context())
			if u == nil {
				writeError(w, http.StatusUnauthorized, "unauthorized", "请先登录")
				return
			}
			if !auth.HasPermission(u.Role, perm) {
				writeError(w, http.StatusForbidden, "forbidden", "无权执行此操作")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// writeUserError 把用户管理领域错误映射为 HTTP 响应。
func writeUserError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrDuplicateUser):
		writeError(w, http.StatusConflict, "user_exists", "用户名已存在")
	case errors.Is(err, auth.ErrInvalidRole):
		writeError(w, http.StatusUnprocessableEntity, "invalid_role", "非法角色")
	case errors.Is(err, auth.ErrWeakPassword):
		writeError(w, http.StatusUnprocessableEntity, "weak_password", "口令至少 8 位")
	case errors.Is(err, auth.ErrLastAdmin):
		writeError(w, http.StatusConflict, "last_admin", "至少需要保留一位管理员")
	case errors.Is(err, auth.ErrUserNotFound):
		writeError(w, http.StatusNotFound, "user_not_found", "用户不存在")
	default:
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
	}
}

// mountUsers 挂载 /api/users 路由,全部要求 users:manage 权限(仅管理员)。
func mountUsers(ar chi.Router, svc userManagementService, aud audit.Recorder) {
	ar.With(requirePermission(auth.PermUsersManage)).Route("/users", func(r chi.Router) {
		r.Get("/", makeListUsersHandler(svc))
		r.Post("/", makeCreateUserHandler(svc, aud))
		r.Put("/{id}/role", makeSetUserRoleHandler(svc, aud))
		r.Post("/{id}/reset-password", makeResetUserPasswordHandler(svc, aud))
		r.Delete("/{id}", makeDeleteUserHandler(svc, aud))
	})
}

// makeListUsersHandler 返回 GET /api/users handler(users:manage)。
func makeListUsersHandler(svc userManagementService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "no_users", "用户服务未初始化")
			return
		}
		users, err := svc.ListUsers()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
			return
		}
		out := make([]userDTO, 0, len(users))
		for _, u := range users {
			out = append(out, toUserDTO(u))
		}
		writeJSON(w, http.StatusOK, userListDTO{Users: out, Roles: auth.KnownRoles()})
	}
}

// makeCreateUserHandler 返回 POST /api/users handler(users:manage)。
func makeCreateUserHandler(svc userManagementService, aud audit.Recorder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "no_users", "用户服务未初始化")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Role     string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "请求体格式错误")
			return
		}
		role := req.Role
		if role == "" {
			role = auth.RoleViewer // 缺省最小权限角色(安全默认)
		}
		u, err := svc.CreateUser(req.Username, req.Password, role)
		if err != nil {
			writeUserError(w, err)
			return
		}
		recordAudit(r.Context(), aud, audit.Entry{
			Action:     audit.ActionUserCreate,
			TargetType: "user",
			TargetID:   u.Username,
			Detail:     map[string]any{"role": u.Role},
			IP:         clientIP(r),
		})
		writeJSON(w, http.StatusCreated, toUserDTO(*u))
	}
}

// makeSetUserRoleHandler 返回 PUT /api/users/{id}/role handler(users:manage)。
func makeSetUserRoleHandler(svc userManagementService, aud audit.Recorder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "no_users", "用户服务未初始化")
			return
		}
		id := chi.URLParam(r, "id")
		r.Body = http.MaxBytesReader(w, r.Body, 1<<10)
		var req struct {
			Role string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "请求体格式错误")
			return
		}
		if err := svc.SetUserRole(id, req.Role); err != nil {
			writeUserError(w, err)
			return
		}
		recordAudit(r.Context(), aud, audit.Entry{
			Action:     audit.ActionUserRoleChange,
			TargetType: "user",
			TargetID:   id,
			Detail:     map[string]any{"role": req.Role},
			IP:         clientIP(r),
		})
		w.WriteHeader(http.StatusNoContent)
	}
}

// makeResetUserPasswordHandler 返回 POST /api/users/{id}/reset-password handler(users:manage)。
func makeResetUserPasswordHandler(svc userManagementService, aud audit.Recorder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "no_users", "用户服务未初始化")
			return
		}
		id := chi.URLParam(r, "id")
		r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
		var req struct {
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "请求体格式错误")
			return
		}
		if err := svc.ResetUserPassword(id, req.Password); err != nil {
			writeUserError(w, err)
			return
		}
		recordAudit(r.Context(), aud, audit.Entry{
			Action:     audit.ActionUserPasswordReset,
			TargetType: "user",
			TargetID:   id,
			Detail:     map[string]any{},
			IP:         clientIP(r),
		})
		w.WriteHeader(http.StatusNoContent)
	}
}

// makeDeleteUserHandler 返回 DELETE /api/users/{id} handler(users:manage)。
func makeDeleteUserHandler(svc userManagementService, aud audit.Recorder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "no_users", "用户服务未初始化")
			return
		}
		id := chi.URLParam(r, "id")
		if err := svc.DeleteUser(id); err != nil {
			writeUserError(w, err)
			return
		}
		recordAudit(r.Context(), aud, audit.Entry{
			Action:     audit.ActionUserDelete,
			TargetType: "user",
			TargetID:   id,
			Detail:     map[string]any{},
			IP:         clientIP(r),
		})
		w.WriteHeader(http.StatusNoContent)
	}
}
