package auth

import "testing"

// TestIsKnownRole 校验内置角色识别。
func TestIsKnownRole(t *testing.T) {
	for _, r := range []string{RoleAdmin, RoleOperator, RoleViewer} {
		if !IsKnownRole(r) {
			t.Errorf("IsKnownRole(%q) = false, want true", r)
		}
	}
	for _, r := range []string{"", "superuser", "Admin", "guest"} {
		if IsKnownRole(r) {
			t.Errorf("IsKnownRole(%q) = true, want false", r)
		}
	}
}

// TestHasPermissionAdminWildcard 校验 admin 对任意权限返回 true。
func TestHasPermissionAdminWildcard(t *testing.T) {
	for _, p := range []string{PermUsersManage, PermSettingsManage, PermDeployRun, PermSystemUpdate, "anything:goes"} {
		if !HasPermission(RoleAdmin, p) {
			t.Errorf("admin should have %q", p)
		}
	}
}

// TestHasPermissionOperator 校验 operator 有操作/读权限、无管理敏感权限。
func TestHasPermissionOperator(t *testing.T) {
	allow := []string{PermProjectsRead, PermProjectsWrite, PermDeployRun, PermServersRead,
		PermProxyManage, PermCredentialsRead, PermAuditRead, PermContainersManage,
		PermApprovalsApprove}
	for _, p := range allow {
		if !HasPermission(RoleOperator, p) {
			t.Errorf("operator should have %q", p)
		}
	}
	deny := []string{PermUsersManage, PermSettingsManage, PermCredentialsManage, PermServersManage, PermSystemUpdate}
	for _, p := range deny {
		if HasPermission(RoleOperator, p) {
			t.Errorf("operator should NOT have %q", p)
		}
	}
}

// TestHasPermissionViewer 校验 viewer 仅只读、无写/管理权限。
func TestHasPermissionViewer(t *testing.T) {
	if !HasPermission(RoleViewer, PermProjectsRead) {
		t.Error("viewer should have projects:read")
	}
	if HasPermission(RoleViewer, PermProjectsWrite) {
		t.Error("viewer should NOT have projects:write")
	}
	if HasPermission(RoleViewer, PermUsersManage) {
		t.Error("viewer should NOT have users:manage")
	}
}

// TestHasPermissionUnknownRoleDenies 校验未知角色默认拒绝(不 panic)。
func TestHasPermissionUnknownRoleDenies(t *testing.T) {
	if HasPermission("ghost", PermUsersManage) {
		t.Error("unknown role should be denied")
	}
}
