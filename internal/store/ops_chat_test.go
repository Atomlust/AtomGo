package store

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// TestOpsChatSchemaBothDialects 校验三方言基线(0001_baseline.sql)都包含完整的
// ops_chat 系列表与关键约束(含 ops_chat_calls(run_id,ordinal) 唯一约束、级联删除等),
// 并在真实 SQLite 库上验证 schema 可用、唯一约束与 ordinal 顺序正确。
func TestOpsChatSchemaBothDialects(t *testing.T) {
	names := []string{"ops_chat_sessions", "ops_chat_entries", "ops_chat_runs", "ops_chat_calls", "ops_chat_approvals", "ops_chat_state"}
	for dialect, fsys := range map[string]fs.FS{"sqlite": sqliteMigrationFS, "mysql": mysqlMigrationFS, "postgres": postgresMigrationFS} {
		body, err := fs.ReadFile(fsys, "migrations/"+dialect+"/0001_baseline.sql")
		if err != nil {
			t.Fatal(err)
		}
		// 表存在性:容忍 `IF NOT EXISTS`、双引号/反引号 以及列定义中的方言差异。
		for _, name := range names {
			// 容忍双引号 / 反引号 包裹的标识符,以及 PostgreSQL 的 "public". 前缀。
			// 反引号不能出现在 Go 原生字符串里,故用普通字符串拼接出可选的引号字符类。
			pat := `(?i)CREATE TABLE\s+(?:IF NOT EXISTS\s+)?(?:"[A-Za-z0-9_]+"\.)?["` + "`" + `]?` + regexp.QuoteMeta(name) + `["` + "`" + `]?\s*\(`
			re := regexp.MustCompile(pat)
			if !re.Match(body) {
				t.Fatalf("%s: 基线缺少 CREATE TABLE %s", dialect, name)
			}
		}
		// 关键约束 / 列 必须存在(仅做子串包含判断,避免方言引号/空格差异)。
		for _, clause := range []string{
			"request_id", "payload_hash", "args_hash", "target_hash",
			"context_allowed", "keycheck", "lease_until",
			"UNIQUE", "ON DELETE CASCADE", "ops_chat_calls_run",
		} {
			if !strings.Contains(string(body), clause) {
				t.Fatalf("%s: 基线缺少约束/列 %q", dialect, clause)
			}
		}
	}

	s, err := Open(t.TempDir() + "/ops.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, name := range names {
		var count int
		if err = s.DB.QueryRow("SELECT COUNT(*) FROM " + name).Scan(&count); err != nil {
			t.Fatal(name, err)
		}
	}
	var n int
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version='0001_baseline'").Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err = s.DB.Exec(`INSERT INTO ops_chat_sessions(id,revision,body,created_at,updated_at) VALUES('session',0,X'01',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(`INSERT INTO ops_chat_runs(id,session_id,request_id,payload_hash,status,body,created_at) VALUES('run','session','request','hash','queued',X'01',1)`); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO ops_chat_calls(id,session_id,run_id,server_id,tool_id,status,args_hash,target_hash,body,created_at,ordinal) VALUES(?,'session','run','server','systemd_action','queued','hash','target',X'01',1,?)`
	for i, id := range []string{"z-stop", "a-start"} {
		if _, err = s.DB.Exec(insert, id, i); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.DB.Exec(insert, "duplicate", 0); err == nil {
		t.Fatal("duplicate plan ordinal accepted")
	}
	rows, err := s.DB.Query(`SELECT id FROM ops_chat_calls WHERE run_id='run' ORDER BY ordinal`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if rows.Err() != nil || strings.Join(ids, ",") != "z-stop,a-start" {
		t.Fatal("schema did not preserve ordinal order", ids, rows.Err())
	}
}
