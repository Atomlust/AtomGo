package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/atomlust/atomgo/internal/audit"
	"github.com/atomlust/atomgo/internal/auth"
	"github.com/atomlust/atomgo/internal/mask"
	"github.com/atomlust/atomgo/internal/storetest"
)

// setupUsersServer 构造带用户管理 + 审计的测试 server;引导 admin/testpass。
func setupUsersServer(t *testing.T) (*httptest.Server, *auth.Service) {
	t.Helper()
	st := storetest.Open(t)
	svc := auth.NewService(st.DB, nil)
	if err := svc.Bootstrap("admin", "testpass"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	rec := audit.New(st.DB, mask.NewMasker(), nil)
	srv := httptest.NewServer(New(testWebFSAuth(), svc, WithUsers(svc), WithAudit(rec)))
	t.Cleanup(srv.Close)
	return srv, svc
}

// loginAs 以指定用户登录,返回携带 cookie 的 client 与 csrf token。
func loginAs(t *testing.T, srvURL, username, password string) (*http.Client, string) {
	t.Helper()
	client := newTestClient(t)
	body := strings.NewReader(`{"username":"` + username + `","password":"` + password + `"}`)
	resp, err := client.Post(srvURL+"/api/auth/login", "application/json", body)
	if err != nil {
		t.Fatalf("login %s: %v", username, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login %s status = %d, want 200", username, resp.StatusCode)
	}
	csrf := ""
	for _, c := range resp.Cookies() {
		if c.Name == cookieCsrf {
			csrf = c.Value
		}
	}
	return client, csrf
}

func decodeUsers(t *testing.T, resp *http.Response) ([]map[string]any, []string) {
	t.Helper()
	var body struct {
		Users []map[string]any `json:"users"`
		Roles []string         `json:"roles"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode users: %v", err)
	}
	return body.Users, body.Roles
}

// TestUsersListAdminCanManage 验证管理员可列出用户(含自身)与可选角色。
func TestUsersListAdminCanManage(t *testing.T) {
	srv, _ := setupUsersServer(t)
	client := newTestClient(t)
	loginWithClient(t, client, srv.URL)

	resp := doJSON(t, client, http.MethodGet, srv.URL+"/api/users", "", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/users status = %d, want 200", resp.StatusCode)
	}
	users, roles := decodeUsers(t, resp)
	if len(users) != 1 || users[0]["username"] != "admin" || users[0]["role"] != "admin" {
		t.Fatalf("users = %v, want single admin", users)
	}
	if len(roles) != 3 {
		t.Fatalf("roles = %v, want 3 (admin/operator/viewer)", roles)
	}
	// 绝不回传口令哈希。
	if users[0]["passwordHash"] != nil || users[0]["password_hash"] != nil {
		t.Fatal("user list must not contain password hash")
	}
}

// TestUsersCreateAndLogin 验证管理员可创建用户(缺省角色 viewer),新用户可登录。
func TestUsersCreateAndLogin(t *testing.T) {
	srv, _ := setupUsersServer(t)
	admin, adminCSRF := loginAs(t, srv.URL, "admin", "testpass")

	// 缺省角色 → viewer。
	resp := doJSON(t, admin, http.MethodPost, srv.URL+"/api/users", adminCSRF,
		`{"username":"viewer1","password":"viewerpass"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201", resp.StatusCode)
	}
	var created map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&created)
	if created["role"] != "viewer" {
		t.Fatalf("default role = %v, want viewer", created["role"])
	}

	// 新用户可登录。
	_, _ = loginAs(t, srv.URL, "viewer1", "viewerpass")

	// 重名 → 409。
	resp2 := doJSON(t, admin, http.MethodPost, srv.URL+"/api/users", adminCSRF,
		`{"username":"viewer1","password":"anotherpass","role":"operator"}`)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate create status = %d, want 409", resp2.StatusCode)
	}

	// 弱口令 → 422。
	resp3 := doJSON(t, admin, http.MethodPost, srv.URL+"/api/users", adminCSRF,
		`{"username":"viewer2","password":"short","role":"viewer"}`)
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("weak password status = %d, want 422", resp3.StatusCode)
	}
}

// TestUsersPermissionEnforced 验证端点级权限拦截:viewer 无权访问 /api/users。
func TestUsersPermissionEnforced(t *testing.T) {
	srv, _ := setupUsersServer(t)
	admin, adminCSRF := loginAs(t, srv.URL, "admin", "testpass")

	// 管理员创建一个 operator(非 viewer),验证 operator 也无 users:manage。
	resp := doJSON(t, admin, http.MethodPost, srv.URL+"/api/users", adminCSRF,
		`{"username":"op1","password":"operatorpass","role":"operator"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create operator status = %d, want 201", resp.StatusCode)
	}

	viewer, _ := loginAs(t, srv.URL, "op1", "operatorpass")
	r1 := doJSON(t, viewer, http.MethodGet, srv.URL+"/api/users", "", "")
	defer r1.Body.Close()
	if r1.StatusCode != http.StatusForbidden {
		t.Fatalf("operator GET /api/users status = %d, want 403", r1.StatusCode)
	}

	// 管理员仍可访问。
	r2 := doJSON(t, admin, http.MethodGet, srv.URL+"/api/users", "", "")
	defer r2.Body.Close()
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("admin GET /api/users status = %d, want 200", r2.StatusCode)
	}
}

// TestEndpointRBACOperatorVsViewer 端到端验证端点级 RBAC:operator 有 projects:write 可建项目;
// viewer 无 → 403;但读端点(GET /api/projects)对 viewer 仍开放。
func TestEndpointRBACOperatorVsViewer(t *testing.T) {
	srv, _, _, _, _, _, asvc := setupPromotionServer(t, nil)

	// admin 先建一个 git 凭据(credentials:manage,admin 可),供后续建项目引用。
	adminClient := newTestClient(t)
	adminCSRF := loginWithClient(t, adminClient, srv.URL)
	credID := newGitCred(t, adminClient, srv.URL, adminCSRF, "ghp_rbac")

	if _, err := asvc.CreateUser("op", "operatorpass", auth.RoleOperator); err != nil {
		t.Fatalf("create operator: %v", err)
	}
	if _, err := asvc.CreateUser("vw", "viewerpass", auth.RoleViewer); err != nil {
		t.Fatalf("create viewer: %v", err)
	}
	opClient, opCSRF := loginAs(t, srv.URL, "op", "operatorpass")
	vwClient, _ := loginAs(t, srv.URL, "vw", "viewerpass")

	// operator 可建项目(projects:write)。
	opResp := doJSON(t, opClient, http.MethodPost, srv.URL+"/api/projects", opCSRF,
		`{"name":"op-proj","repoUrl":"https://gitee.com/acme/op.git","credentialId":"`+credID+`"}`)
	opResp.Body.Close()
	if opResp.StatusCode != http.StatusCreated {
		t.Fatalf("operator create project = %d, want 201", opResp.StatusCode)
	}

	// viewer 不可建项目(403)。
	vwResp := doJSON(t, vwClient, http.MethodPost, srv.URL+"/api/projects", "",
		`{"name":"vw-proj","repoUrl":"https://gitee.com/acme/vw.git","credentialId":"`+credID+`"}`)
	vwResp.Body.Close()
	if vwResp.StatusCode != http.StatusForbidden {
		t.Fatalf("viewer create project = %d, want 403", vwResp.StatusCode)
	}

	// viewer 可读项目列表(200;读端点不做权限拦截)。
	listResp := doJSON(t, vwClient, http.MethodGet, srv.URL+"/api/projects", "", "")
	listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("viewer list projects = %d, want 200", listResp.StatusCode)
	}
}

// TestLastAdminProtection 验证禁止删除/降级最后一位管理员。
func TestLastAdminProtection(t *testing.T) {
	srv, _ := setupUsersServer(t)
	admin, adminCSRF := loginAs(t, srv.URL, "admin", "testpass")

	// 找到 admin 自己的 id。
	listResp := doJSON(t, admin, http.MethodGet, srv.URL+"/api/users", "", "")
	users, _ := decodeUsers(t, listResp)
	listResp.Body.Close()
	adminID, _ := users[0]["id"].(string)
	if adminID == "" {
		t.Fatal("admin id missing")
	}

	// 删除唯一管理员 → 409 last_admin。
	delResp := doJSON(t, admin, http.MethodDelete, srv.URL+"/api/users/"+adminID, adminCSRF, "")
	defer delResp.Body.Close()
	if delResp.StatusCode != http.StatusConflict {
		t.Fatalf("delete last admin status = %d, want 409", delResp.StatusCode)
	}

	// 降级唯一管理员 → 409 last_admin。
	roleResp := doJSON(t, admin, http.MethodPut, srv.URL+"/api/users/"+adminID+"/role", adminCSRF, `{"role":"viewer"}`)
	defer roleResp.Body.Close()
	if roleResp.StatusCode != http.StatusConflict {
		t.Fatalf("demote last admin status = %d, want 409", roleResp.StatusCode)
	}
}
