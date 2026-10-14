package httpapi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atomlust/atomgo/internal/auth"
	"github.com/atomlust/atomgo/internal/settings"
	"github.com/atomlust/atomgo/internal/vault"
)

// TestSetupBuildDSN 单测 buildDSN:默认端口、字段必填、原始 DSN 优先。
func TestSetupBuildDSN(t *testing.T) {
	cases := []struct {
		driver, host, port, user, pass, name, raw, wantContains string
		wantErr                                      bool
	}{
		{"mysql", "dbhost", "", "u", "p", "atomgo", "", "tcp(dbhost:3306)", false},
		{"postgres", "dbhost", "5433", "u", "p", "atomgo", "", "dbhost:5433", false},
		{"mysql", "", "3306", "u", "p", "atomgo", "", "", true}, // 缺主机
		{"mysql", "h", "", "u", "p", "", "", "", true},          // 缺库名
		{"mysql", "h", "", "u", "p", "n", "my-raw-dsn", "my-raw-dsn", false}, // 原始 DSN 优先
	}
	for _, c := range cases {
		got, err := buildDSN(c.driver, c.host, c.port, c.user, c.pass, c.name, c.raw)
		if c.wantErr {
			if err == nil {
				t.Fatalf("buildDSN(%+v) 期望错误,但得到 nil", c)
			}
			continue
		}
		if err != nil {
			t.Fatalf("buildDSN(%+v) 意外错误:%v", c, err)
		}
		if !strings.Contains(got, c.wantContains) {
			t.Fatalf("buildDSN(%+v) = %q,期望包含 %q", c, got, c.wantContains)
		}
	}
}

// TestSetupDBConfiguredSkipsDBBlock 模拟「重启后已固化 MySQL」:env 文件含
// ATOMGO_DB_DRIVER 时 dbConfigured=true,Submit 应跳过数据库块、直接走管理员初始化,
// 不触发重启、不改写 env 文件。
func TestSetupDBConfiguredSkipsDBBlock(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "atomgo.env")
	if err := os.WriteFile(envFile, []byte("ATOMGO_DB_DRIVER=mysql\nATOMGO_DB_DSN=mysql://x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	st := testStoreAuth(t) // 全新空库,无管理员 → 处于 setup 模式
	svc := auth.NewService(st.DB, nil)
	vlt := vault.New(st.DB, nil)
	settingsSvc := settings.New(st.DB)
	setupSvc, err := NewSetupServiceForWiring(svc, vlt, settingsSvc, "", envFile, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Status:dbConfigured=true,dbDriver 反映当前生效驱动(sqlite 默认,因测试进程未设 env)。
	st0, err := setupSvc.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !st0.DBConfigured {
		t.Fatalf("want dbConfigured=true when env file has ATOMGO_DB_DRIVER")
	}

	// Submit:dbConfigured=true → 跳过数据库块,走管理员初始化,不重启。
	res, err := setupSvc.Submit(context.Background(), SetupInput{
		Username: "admin", Password: "supersecret1", MasterKey: "generate",
	})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if !res.OK {
		t.Fatalf("submit ok=false")
	}
	if res.RestartRequired {
		t.Fatalf("dbConfigured 时不应触发重启")
	}
	// env 文件未被改写为 sqlite(保持原 mysql 配置)。
	if data, _ := os.ReadFile(envFile); strings.Contains(string(data), "sqlite") {
		t.Fatalf("不应在 dbConfigured 时改写数据库驱动")
	}
}

// TestSetupMySQLInvalidConnectionRejected 选 MySQL 但连接不通时,Submit 应返回错误,
// 不固化、不重启(避免把坏配置写死)。
func TestSetupMySQLInvalidConnectionRejected(t *testing.T) {
	st := testStoreAuth(t)
	svc := auth.NewService(st.DB, nil)
	vlt := vault.New(st.DB, nil)
	settingsSvc := settings.New(st.DB)
	dir := t.TempDir()
	envFile := filepath.Join(dir, "atomgo.env")
	setupSvc, err := NewSetupServiceForWiring(svc, vlt, settingsSvc, "", envFile, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = setupSvc.Submit(context.Background(), SetupInput{
		Username: "admin", Password: "supersecret1", MasterKey: "generate",
		DBDriver: "mysql", DBHost: "127.0.0.1", DBPort: "1", DBUser: "u", DBName: "n",
	})
	if err == nil {
		t.Fatalf("期望连接失败错误,但得到 nil")
	}
	if envHasKey(envFile, "ATOMGO_DB_DRIVER") {
		t.Fatalf("连接失败不应固化数据库配置")
	}
}
