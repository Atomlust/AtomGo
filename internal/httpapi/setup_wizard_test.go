package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/atomlust/atomgo/internal/auth"
	"github.com/atomlust/atomgo/internal/settings"
	"github.com/atomlust/atomgo/internal/vault"
)

// TestSetupWizardFlow 端到端验证「首次启动 Web 初始化向导」:
//   - 全新空库(无管理员)→ setup 模式,/api/setup/status 返回 setupRequired=true 且带 token;
//   - setup 门在初始化未完成时把业务 /api 请求 404;
//   - POST /api/setup/submit 建管理员 + 生成 master key(一次性回显);
//   - 完成后 /api/setup/status 返回 setupRequired=false,门不再拦截(改走认证 → 401);
//   - 新凭据可登录。
//
// 使用 httptest(127.0.0.1 临时端口),不依赖外部监听绑定。
func TestSetupWizardFlow(t *testing.T) {
	st := testStoreAuth(t) // 全新空库,无管理员 → 处于 setup 模式
	svc := auth.NewService(st.DB, nil)
	vlt := vault.New(st.DB, nil) // 未配置
	settingsSvc := settings.New(st.DB)

	setupSvc, err := NewSetupServiceForWiring(svc, vlt, settingsSvc, "", "", nil)
	if err != nil {
		t.Fatalf("NewSetupServiceForWiring: %v", err)
	}
	srv := httptest.NewServer(New(testWebFSAuth(), svc, WithSetup(setupSvc)))
	t.Cleanup(srv.Close)
	client := newTestClient(t)

	// 1) status:setupRequired=true + token
	resp := doJSON(t, client, http.MethodGet, srv.URL+"/api/setup/status", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status status = %d, want 200", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	var st1 struct {
		SetupRequired bool   `json:"setupRequired"`
		Token         string `json:"token"`
	}
	if err := json.Unmarshal(raw, &st1); err != nil {
		t.Fatalf("unmarshal status: %v (%s)", err, raw)
	}
	if !st1.SetupRequired {
		t.Fatalf("want setupRequired=true on fresh db")
	}
	if st1.Token == "" {
		t.Fatalf("want non-empty setup token during setup")
	}

	// 2) 门:初始化未完成时业务 /api 一律 404
	g := doJSON(t, client, http.MethodGet, srv.URL+"/api/projects", "", "")
	if g.StatusCode != http.StatusNotFound {
		t.Fatalf("gate during setup: GET /api/projects status = %d, want 404", g.StatusCode)
	}
	if c := errorCode(t, g); c != "setup_required" {
		t.Fatalf("gate error code = %q, want setup_required", c)
	}

	// 3) 提交:建管理员 + 自动生成 master key
	submit := `{"token":"` + st1.Token + `","username":"wiz","password":"supersecret1","masterKey":"generate","publicUrl":""}`
	sr := doJSON(t, client, http.MethodPost, srv.URL+"/api/setup/submit", "", submit)
	if sr.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(sr.Body)
		t.Fatalf("submit status = %d, want 200; body=%s", sr.StatusCode, body)
	}
	var sub struct {
		OK        bool   `json:"ok"`
		MasterKey string `json:"masterKey"`
	}
	_ = json.NewDecoder(sr.Body).Decode(&sub)
	if !sub.OK {
		t.Fatalf("submit ok = false")
	}
	if sub.MasterKey == "" {
		t.Fatalf("auto-generated master key not echoed")
	}

	// 4) 完成后 status:setupRequired=false
	resp2 := doJSON(t, client, http.MethodGet, srv.URL+"/api/setup/status", "", "")
	var st2 struct {
		SetupRequired bool `json:"setupRequired"`
	}
	_ = json.NewDecoder(resp2.Body).Decode(&st2)
	if st2.SetupRequired {
		t.Fatalf("want setupRequired=false after submit")
	}

	// 5) 门不再拦截:业务 /api 改走认证 → 未认证 401
	g2 := doJSON(t, client, http.MethodGet, srv.URL+"/api/projects", "", "")
	if g2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("after setup: GET /api/projects status = %d, want 401", g2.StatusCode)
	}

	// 6) 幂等:再次提交 → 410 Gone
	submit2 := `{"token":"` + st1.Token + `","username":"wiz","password":"supersecret1","masterKey":"generate","publicUrl":""}`
	sr2 := doJSON(t, client, http.MethodPost, srv.URL+"/api/setup/submit", "", submit2)
	if sr2.StatusCode != http.StatusGone {
		t.Fatalf("repeat submit status = %d, want 410", sr2.StatusCode)
	}

	// 7) 新凭据可登录
	login := doLogin(t, srv, "wiz", "supersecret1")
	if login.StatusCode != http.StatusOK {
		t.Fatalf("login with new admin status = %d, want 200", login.StatusCode)
	}

	// 8) master key 已注入保险库:vault 现可用(配置后凭据可创建/列出不报错)。
	if !vlt.Configured() {
		t.Fatalf("vault should be configured after setup generated key")
	}
}
