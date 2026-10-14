package auth

import (
	"context"
	"errors"
)

// User 是平台的一个登录账号(多用户实例)。
type User struct {
	ID       string
	Username string
	Role     string
}

// 角色常量。admin 为通配(拥有全部权限);operator/viewer 权限见 rolePermissions。
const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleViewer   = "viewer"
)

// 权限常量(资源:操作)。用于端点级拦截(requirePermission)。
const (
	PermProjectsRead      = "projects:read"
	PermProjectsWrite     = "projects:write"
	PermDeployRun         = "deploy:run"
	PermServersRead       = "servers:read"
	PermServersManage     = "servers:manage"
	PermProxyManage       = "proxy:manage"
	PermCredentialsRead   = "credentials:read"
	PermCredentialsManage = "credentials:manage"
	PermUsersManage       = "users:manage"
	PermSettingsManage    = "settings:manage"
	PermAuditRead         = "audit:read"
	PermContainersManage  = "containers:manage"
	PermTemplatesManage   = "templates:manage"
	PermCustomNodesManage = "customnodes:manage"
	PermBatchesManage     = "batches:manage"
	PermApprovalsApprove  = "approvals:approve"
	PermSystemUpdate      = "system:update"
)

// rolePermissions 是 operator/viewer 的权限映射。admin 为通配,无需列举。
// 该映射同时是 role_permissions 表的种子来源(见迁移 0056)。
var rolePermissions = map[string][]string{
	RoleOperator: {
		PermProjectsRead, PermProjectsWrite, PermDeployRun, PermServersRead,
		PermProxyManage, PermCredentialsRead, PermAuditRead, PermContainersManage,
		PermTemplatesManage, PermCustomNodesManage, PermBatchesManage, PermApprovalsApprove,
	},
	RoleViewer: {
		PermProjectsRead, PermServersRead, PermAuditRead, PermCredentialsRead,
	},
}

// KnownRoles 返回所有内置角色名(供 UI 下拉与校验)。
func KnownRoles() []string { return []string{RoleAdmin, RoleOperator, RoleViewer} }

// IsKnownRole 报告角色是否为内置角色。
func IsKnownRole(role string) bool {
	switch role {
	case RoleAdmin, RoleOperator, RoleViewer:
		return true
	}
	return false
}

// HasPermission 报告某角色是否拥有某权限。admin 通配返回 true;未知角色默认拒绝。
func HasPermission(role, perm string) bool {
	if role == RoleAdmin {
		return true
	}
	for _, p := range rolePermissions[role] {
		if p == perm {
			return true
		}
	}
	return false
}

// 用户相关错误。
var (
	// ErrDuplicateUser 表示用户名已存在(创建/改名冲突)。
	ErrDuplicateUser = errors.New("auth: user already exists")
	// ErrLastAdmin 表示操作会导致系统中无任何管理员(禁止,避免锁死)。
	ErrLastAdmin = errors.New("auth: 至少需要保留一位管理员")
	// ErrUserNotFound 表示用户不存在。
	ErrUserNotFound = errors.New("auth: user not found")
	// ErrInvalidRole 表示角色不是内置角色(admin/operator/viewer)。
	ErrInvalidRole = errors.New("auth: invalid role")
)

// userCtxKey 是 context 中存放 *User 的键。
type userCtxKey struct{}

// WithUser 将已认证用户注入 context(由 requireAuth 中间件调用)。
func WithUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, userCtxKey{}, u)
}

// UserFromContext 取出 context 中的已认证用户;未认证(无用户)时返回 nil。
func UserFromContext(ctx context.Context) *User {
	u, _ := ctx.Value(userCtxKey{}).(*User)
	return u
}
