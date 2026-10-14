package store

import (
	"database/sql"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestMigrationSetsMatch 防漂移:三套方言( sqlite / mysql / postgres )的迁移版本键集合必须完全一致。
//
// 整合(squash)后,每个方言仅保留一个 0001_baseline.sql,故此处断言:
//  1. 三方言集合相等;
//  2. 唯一版本键为 0001_baseline(即 v1.0.0 的完整 schema),不得出现任何增量文件残留;
//  3. 任一方言多写/漏写文件即在此红。
func TestMigrationSetsMatch(t *testing.T) {
	sq := migrationVersions(t, sqliteMigrationFS, "migrations/sqlite/*.sql")
	my := migrationVersions(t, mysqlMigrationFS, "migrations/mysql/*.sql")
	pg := migrationVersions(t, postgresMigrationFS, "migrations/postgres/*.sql")

	const baseline = "0001_baseline"
	for label, got := range map[string]map[string]bool{"sqlite": sq, "mysql": my, "postgres": pg} {
		if len(got) == 0 {
			t.Fatalf("%s: 未找到任何迁移", label)
		}
		if _, ok := got[baseline]; !ok {
			t.Errorf("%s: 缺少基线迁移版本 %q", label, baseline)
		}
		for v := range got {
			if v != baseline {
				t.Errorf("%s: 整合后不应存在增量迁移版本 %q(只允许 %q)", label, v, baseline)
			}
		}
	}
	// 三方言必须完全对称
	for v := range sq {
		if !my[v] || !pg[v] {
			t.Errorf("版本 %s 三方言不对称(sqlite/mysql/postgres)", v)
		}
	}
}

// TestMigrationPrefixesUnique 防回归:同一方言内不得有两个迁移共用数字前缀。
//
// schema_migrations.version 存的是**完整文件名**(migrationVersion 取 basename 去 .sql),
// 故重复前缀技术上不会互相覆盖、也不会跳过——但它破坏了"编号唯一 = 版本序"的隐含约定:
// 两条迁移的执行先后退化为文件名字典序(纯巧合),后人极易据此写出错误依赖。
// 历史上确实出现过 0053 重号(0053_drop_env_promotion + 0053_system_settings),
// 故加此断言锁死。整合为单文件 baseline 后,该约束仍对未来的增量迁移生效。
func TestMigrationPrefixesUnique(t *testing.T) {
	for _, c := range []struct {
		dialect string
		fsys    fs.FS
	}{
		{"sqlite", sqliteMigrationFS},
		{"mysql", mysqlMigrationFS},
		{"postgres", postgresMigrationFS},
	} {
		entries, err := fs.Glob(c.fsys, "migrations/"+c.dialect+"/*.sql")
		if err != nil {
			t.Fatalf("glob %s: %v", c.dialect, err)
		}
		seen := make(map[string]string, len(entries))
		for _, e := range entries {
			name := filepath.Base(e)
			prefix, _, ok := strings.Cut(name, "_")
			if !ok {
				t.Errorf("%s: 迁移文件名 %q 缺少 <序号>_<名称> 约定", c.dialect, name)
				continue
			}
			// 序号必须是「纯数字 + 定长零填充」。零填充不是风格问题:migrate 用
			// sort.Strings(字典序)排序执行顺序,只有定长补零才能让字典序 == 数值序。
			// 写成 "54_x.sql" 会被排到 "0055" 之后(即永远最后执行),顺序静默错乱。
			if !isFixedWidthDigits(prefix) {
				t.Errorf("%s: 迁移编号 %q(%s)不是定长零填充的纯数字;请写成 %s 形式",
					c.dialect, prefix, name, zeroPad(len(prefix)))
				continue
			}
			if prev, dup := seen[prefix]; dup {
				t.Errorf("%s: 迁移编号 %s 重复(%s 与 %s);请把后者改为下一个未占用序号",
					c.dialect, prefix, prev, name)
				continue
			}
			seen[prefix] = name
		}
	}
}

// isFixedWidthDigits 判断 s 是否为定长零填充的纯数字序号(定长由 s 自身长度决定,
// 故 "0054" 与 "54" 都能通过各自的定长检查 —— 但宽度不一致会导致排序错乱,
// 故本函数只负责「非数字 / 空 / 含非数字字符」,宽度一致性由 TestMigrationPrefixWidth 校验)。
func isFixedWidthDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func zeroPad(width int) string {
	return strings.Repeat("0", width) + "1"
}

// TestMigrationPrefixWidth 校验各方言的迁移序号宽度一致(当前全库为 4 位)。
//
// 宽度不一致是零填充失效的隐蔽形式:"54_x" 与 "0055_y" 各自都"合法",
// 但字典序把 "54_x" 排到 "0055_y" 之后 —— 顺序静默错乱,且不触发任何重复编号告警。
func TestMigrationPrefixWidth(t *testing.T) {
	const want = 4
	for _, c := range []struct {
		dialect string
		fsys    fs.FS
	}{
		{"sqlite", sqliteMigrationFS},
		{"mysql", mysqlMigrationFS},
		{"postgres", postgresMigrationFS},
	} {
		entries, err := fs.Glob(c.fsys, "migrations/"+c.dialect+"/*.sql")
		if err != nil {
			t.Fatalf("glob %s: %v", c.dialect, err)
		}
		for _, e := range entries {
			name := filepath.Base(e)
			prefix, _, ok := strings.Cut(name, "_")
			if !ok || !isFixedWidthDigits(prefix) {
				continue // 非编号类文件名已由上一个测试报错
			}
			if len(prefix) != want {
				t.Errorf("%s: 迁移 %s 的序号宽度为 %d,期望 %d 位(与其余迁移不一致会打乱字典序)",
					c.dialect, name, len(prefix), want)
			}
		}
	}
}

// TestBaselineExecutionModeContract 校验 squash 后的基线语义:
//
// 原 0050 增量迁移(pipeline_runs.execution_mode 默认 'pending' 且历史记录回填
// 'legacy_unknown'、pipeline_configs.saved_at 可空)在整合为 0001_baseline 后,
// v1.0.0 起新记录直接默认 'pending'(不再需要历史回填步骤),saved_at 保持可空。
// 该测试锁定这一收敛后的契约,防止后续 baseline 生成时把列约束改回。
func TestBaselineExecutionModeContract(t *testing.T) {
	s, err := Open(t.TempDir() + "/baseline.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// 满足外键链:credentials -> projects -> pipeline_runs / pipeline_configs。
	if _, err := s.DB.Exec(`INSERT INTO credentials (id,name,type,scope,ciphertext,masked_value,created_at,updated_at) VALUES ('c','c','git_token','',X'00','m','now','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(`INSERT INTO projects (id,name,repo_url,default_branch,credential_id,created_at,updated_at) VALUES ('p','p','https://example.com/r','main','c','now','now')`); err != nil {
		t.Fatal(err)
	}

	// 不显式指定 execution_mode,应当落库为 'pending'(DEFAULT)。
	if _, err := s.DB.Exec(`INSERT INTO pipeline_runs (id,project_id,status,created_at) VALUES ('new','p','success','now')`); err != nil {
		t.Fatal(err)
	}
	var mode string
	if err := s.DB.QueryRow(`SELECT execution_mode FROM pipeline_runs WHERE id='new'`).Scan(&mode); err != nil || mode != "pending" {
		t.Fatalf("new run execution_mode = %q, %v", mode, err)
	}

	// saved_at 可空:pipeline_configs 插入时不给 saved_at 不应报错。
	if _, err := s.DB.Exec(`INSERT INTO pipeline_configs (project_id,created_at,updated_at) VALUES ('p','now','now')`); err != nil {
		t.Fatal(err)
	}
	var saved sql.NullString
	if err := s.DB.QueryRow(`SELECT saved_at FROM pipeline_configs WHERE project_id='p'`).Scan(&saved); err != nil || saved.Valid {
		t.Fatalf("saved_at 应为 NULL: valid=%v, %v", saved.Valid, err)
	}
}

func migrationVersions(t *testing.T, fsys fs.FS, glob string) map[string]bool {
	t.Helper()
	entries, err := fs.Glob(fsys, glob)
	if err != nil {
		t.Fatalf("glob %s: %v", glob, err)
	}
	out := make(map[string]bool, len(entries))
	for _, e := range entries {
		out[migrationVersion(e)] = true
	}
	return out
}

func TestSplitStatements(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"comment only", "-- just a comment\n", nil},
		{
			"two statements with comments",
			"-- header\nCREATE TABLE a (id INT);\n-- mid\nCREATE INDEX i ON a (id);\n",
			[]string{"CREATE TABLE a (id INT)", "CREATE INDEX i ON a (id)"},
		},
		{
			"semicolon inside single quote not split",
			"INSERT INTO t VALUES ('a;b');",
			[]string{"INSERT INTO t VALUES ('a;b')"},
		},
		{
			"trigger single statement",
			"CREATE TRIGGER x BEFORE UPDATE ON t FOR EACH ROW\nSIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'no: update';",
			[]string{"CREATE TRIGGER x BEFORE UPDATE ON t FOR EACH ROW\nSIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'no: update'"},
		},
		{
			"inline trailing comment stripped",
			"SELECT 1; -- trailing\nSELECT 2;",
			[]string{"SELECT 1", "SELECT 2"},
		},
	}
	for _, c := range cases {
		got := splitStatements(c.in)
		if len(got) != len(c.want) {
			t.Errorf("%s: got %d stmts %q, want %d %q", c.name, len(got), got, len(c.want), c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s[%d]: got %q want %q", c.name, i, got[i], c.want[i])
			}
		}
	}
}
