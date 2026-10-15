package vault

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/atomlust/atomgo/internal/storetest"
)

// pemKey 是一段以 PEM 头开头的伪 SSH 私钥(AC-SEC-01 用)。绝不可在库 dump 中出现。
const pemKey = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
SUPERSECRETPLAINTEXTMARKER1234567890abcdef
-----END OPENSSH PRIVATE KEY-----`

func testKey() *[keySize]byte {
	var k [keySize]byte
	for i := range k {
		k[i] = byte(i + 1)
	}
	return &k
}

func wrongKey() *[keySize]byte {
	var k [keySize]byte
	for i := range k {
		k[i] = byte(255 - i)
	}
	return &k
}

func testDB(t *testing.T) *sql.DB {
	return storetest.OpenDB(t)
}

// TestSealOpenRoundTrip 验证加解密往返 == 原文。
func TestSealOpenRoundTrip(t *testing.T) {
	key := testKey()
	plaintext := []byte(pemKey)
	sealed, err := seal(key, plaintext)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if len(sealed) <= nonceSize {
		t.Fatalf("sealed too short: %d", len(sealed))
	}
	got, err := open(key, sealed)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if string(got) != pemKey {
		t.Fatalf("round trip mismatch")
	}
}

// TestOpenWrongKeyFails 验证错误 master key 解密失败(认证标签校验)。
func TestOpenWrongKeyFails(t *testing.T) {
	sealed, err := seal(testKey(), []byte("hello world"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if _, err := open(wrongKey(), sealed); err == nil {
		t.Fatal("expected decrypt failure with wrong key, got nil")
	}
}

// TestNonceUnique 验证两次 seal 同一明文产生不同密文(随机 nonce)。
func TestNonceUnique(t *testing.T) {
	key := testKey()
	a, _ := seal(key, []byte("same"))
	b, _ := seal(key, []byte("same"))
	if string(a) == string(b) {
		t.Fatal("two seals produced identical ciphertext (nonce not random)")
	}
}

// TestUnconfiguredVault 验证未配置 master key 时所有操作返回 ErrVaultUnconfigured。
func TestUnconfiguredVault(t *testing.T) {
	v := New(testDB(t), nil)
	if _, err := v.Create(CreateInput{Name: "x", Type: TypeGitToken, Secret: "ghp_abc"}); err != ErrVaultUnconfigured {
		t.Fatalf("Create err = %v, want ErrVaultUnconfigured", err)
	}
	if _, err := v.List(); err != ErrVaultUnconfigured {
		t.Fatalf("List err = %v, want ErrVaultUnconfigured", err)
	}
	if _, err := v.Get("x"); err != ErrVaultUnconfigured {
		t.Fatalf("Get err = %v, want ErrVaultUnconfigured", err)
	}
	if _, err := v.Update("x", UpdateInput{}); err != ErrVaultUnconfigured {
		t.Fatalf("Update err = %v, want ErrVaultUnconfigured", err)
	}
	if err := v.Delete("x"); err != ErrVaultUnconfigured {
		t.Fatalf("Delete err = %v, want ErrVaultUnconfigured", err)
	}
}

// TestCreateGetRoundTrip 验证 Create→Get 取回原文,并更新 last_used_at。
func TestCreateGetRoundTrip(t *testing.T) {
	v := New(testDB(t), testKey())
	cred, err := v.Create(CreateInput{Name: "deploy key", Type: TypeSSHKey, Scope: "prod", Secret: pemKey})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if cred.LastUsedAt != nil {
		t.Fatal("new credential should have nil lastUsedAt")
	}
	if strings.Contains(cred.MaskedValue, "SUPERSECRET") || strings.Contains(cred.MaskedValue, "BEGIN") {
		t.Fatalf("masked value leaks plaintext: %q", cred.MaskedValue)
	}

	plain, err := v.Get(cred.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if plain != pemKey {
		t.Fatal("Get did not return original plaintext")
	}

	// last_used_at 现已被更新。
	list, _ := v.List()
	if len(list) != 1 || list[0].LastUsedAt == nil {
		t.Fatal("lastUsedAt should be set after Get")
	}
}

// TestUpsertByNameConsolidates 验证 OAuth 同步合并:两次同名 UpsertByName 只留一条凭据,
// 明文随之刷新;被项目绑定的那条就地刷新(不新增、不误删)。
func TestUpsertByNameConsolidates(t *testing.T) {
	db := testDB(t)
	v := New(db, testKey())

	// 第一次同步:插入新凭据。
	c1, err := v.UpsertByName(CreateInput{Name: "gitee:alice", Type: TypeGitToken, Scope: "global", Secret: "tok-v1"})
	if err != nil {
		t.Fatalf("upsert1: %v", err)
	}
	// 第二次同步(同名、不同 token):应就地刷新同一条,不新增。
	c2, err := v.UpsertByName(CreateInput{Name: "gitee:alice", Type: TypeGitToken, Scope: "global", Secret: "tok-v2"})
	if err != nil {
		t.Fatalf("upsert2: %v", err)
	}
	if c2.ID != c1.ID {
		t.Fatalf("同名 upsert 应复用同一 id: got %s want %s", c2.ID, c1.ID)
	}
	if got, _ := v.Reveal(c2.ID); got != "tok-v2" {
		t.Fatalf("凭据明文应更新为 tok-v2: got %q", got)
	}
	list, _ := v.List()
	if len(list) != 1 {
		t.Fatalf("同名凭据应合并为 1 条,实际 %d 条", len(list))
	}

	// 把这条凭据绑到项目,再同步一次:应保持同一 id 并刷新,不新增、不误删。
	if _, err := db.Exec(
		`INSERT INTO projects (id,name,repo_url,default_branch,credential_id,created_at,updated_at) VALUES (?,?,?,?,?,?,?)`,
		"pid", "p", "https://gitee.com/alice/r.git", "main", c1.ID, "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	c3, err := v.UpsertByName(CreateInput{Name: "gitee:alice", Type: TypeGitToken, Scope: "global", Secret: "tok-v3"})
	if err != nil {
		t.Fatalf("upsert3: %v", err)
	}
	if c3.ID != c1.ID {
		t.Fatalf("绑定后 upsert 必须刷新被绑定的同一条: got %s want %s", c3.ID, c1.ID)
	}
	if got, _ := v.Reveal(c3.ID); got != "tok-v3" {
		t.Fatalf("绑定凭据明文应更新为 tok-v3: got %q", got)
	}
	var cnt int
	if err := db.QueryRow(`SELECT COUNT(*) FROM projects WHERE credential_id = ?`, c3.ID).Scan(&cnt); err != nil || cnt != 1 {
		t.Fatalf("项目应仍绑定该凭据: cnt=%d err=%v", cnt, err)
	}
}

// TestValidateType 验证类型枚举校验。
func TestValidateType(t *testing.T) {
	v := New(testDB(t), testKey())
	if _, err := v.Create(CreateInput{Name: "x", Type: "bogus", Secret: "s"}); err != ErrInvalidType {
		t.Fatalf("err = %v, want ErrInvalidType", err)
	}
	if _, err := v.Create(CreateInput{Name: "x", Type: TypeGitToken, Secret: ""}); err != ErrEmptySecret {
		t.Fatalf("err = %v, want ErrEmptySecret", err)
	}
	if _, err := v.Create(CreateInput{Name: "", Type: TypeGitToken, Secret: "s"}); err != ErrEmptyName {
		t.Fatalf("err = %v, want ErrEmptyName", err)
	}
}

func TestGitHTTPCredentialStoresUsernameAndSecretEncrypted(t *testing.T) {
	db := testDB(t)
	v := New(db, testKey())
	cred, err := v.Create(CreateInput{
		Name: "basic git", Type: TypeGitHTTP, Username: "alice", Secret: "account-password",
	})
	if err != nil {
		t.Fatalf("Create git_http: %v", err)
	}
	if cred.Type != TypeGitHTTP || cred.Username != "alice" {
		t.Fatalf("credential metadata = %+v", cred)
	}
	got, err := v.GetGitAuth(cred.ID)
	if err != nil {
		t.Fatalf("GetGitAuth: %v", err)
	}
	if got.Username != "alice" || got.Token != "account-password" {
		t.Fatalf("GitAuth = %+v", got)
	}
	var ciphertext string
	if err := db.QueryRow(`SELECT ciphertext FROM credentials WHERE id = ?`, cred.ID).Scan(&ciphertext); err != nil {
		t.Fatalf("read ciphertext: %v", err)
	}
	if strings.Contains(ciphertext, "account-password") {
		t.Fatal("git_http secret stored in plaintext")
	}
}

// TestUpdateRotateSecret 验证轮换 secret 后旧密文换新、Get 返回新明文、掩码更新。
func TestUpdateRotateSecret(t *testing.T) {
	v := New(testDB(t), testKey())
	cred, _ := v.Create(CreateInput{Name: "tok", Type: TypeGitToken, Secret: "ghp_oldsecret1111"})

	newSecret := "ghp_newsecret9999"
	updated, err := v.Update(cred.ID, UpdateInput{Secret: &newSecret})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !strings.HasSuffix(updated.MaskedValue, "9999") {
		t.Fatalf("masked not updated after rotation: %q", updated.MaskedValue)
	}
	plain, _ := v.Get(cred.ID)
	if plain != newSecret {
		t.Fatalf("Get after rotate = %q, want new secret", plain)
	}
}

// TestUpdateNameScope 验证仅改名/作用域不动密文。
func TestUpdateNameScope(t *testing.T) {
	v := New(testDB(t), testKey())
	cred, _ := v.Create(CreateInput{Name: "old", Type: TypeRegistry, Scope: "s1", Secret: "alice:pw"})
	newName, newScope := "new", "s2"
	updated, err := v.Update(cred.ID, UpdateInput{Name: &newName, Scope: &newScope})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "new" || updated.Scope != "s2" {
		t.Fatalf("update mismatch: %+v", updated)
	}
	plain, _ := v.Get(cred.ID)
	if plain != "alice:pw" {
		t.Fatal("secret changed unexpectedly")
	}
}

// TestDeleteNotFound 验证删除不存在的凭据返回 ErrNotFound。
func TestDeleteNotFound(t *testing.T) {
	v := New(testDB(t), testKey())
	if err := v.Delete("nope"); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// TestGetNotFound 验证取不存在的凭据返回 ErrNotFound。
func TestGetNotFound(t *testing.T) {
	v := New(testDB(t), testKey())
	if _, err := v.Get("nope"); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestGetGitAuthKeepsUsernameAndToken(t *testing.T) {
	v := New(testDB(t), testKey())
	cred, err := v.Create(CreateInput{
		Name: "gitee token", Type: TypeGitToken, Username: "actual-account", Secret: "token-value",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if cred.Username != "actual-account" {
		t.Fatalf("created username = %q", cred.Username)
	}
	auth, err := v.GetGitAuth(cred.ID)
	if err != nil {
		t.Fatalf("GetGitAuth: %v", err)
	}
	if auth.Username != "actual-account" || auth.Token != "token-value" {
		t.Fatalf("GetGitAuth = %+v", auth)
	}
	listed, err := v.List()
	if err != nil || len(listed) != 1 || listed[0].Username != "actual-account" {
		t.Fatalf("List = %+v, err = %v", listed, err)
	}
}

// TestACSEC01_NoPlaintextInDB 是 AC-SEC-01 核心回归:
// 创建含 PEM 私钥的凭据后,遍历整库**所有表所有列** dump,grep 不到明文/PEM 头。
func TestACSEC01_NoPlaintextInDB(t *testing.T) {
	storetest.SkipIfNotSQLite(t) // 整库 dump 经 sqlite_master 遍历表,SQLite 文件专有;密文属性由 vault 层保证
	db := testDB(t)
	v := New(db, testKey())
	if _, err := v.Create(CreateInput{Name: "ci key", Type: TypeSSHKey, Scope: "prod", Secret: pemKey}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	dump := dumpEntireDB(t, db)
	forbidden := []string{
		"-----BEGIN",
		"SUPERSECRETPLAINTEXTMARKER1234567890abcdef",
		"OPENSSH PRIVATE KEY",
	}
	for _, needle := range forbidden {
		if strings.Contains(dump, needle) {
			t.Fatalf("AC-SEC-01 FAILED: DB dump contains forbidden plaintext %q", needle)
		}
	}
}

// dumpEntireDB 遍历所有用户表的所有列,把每个单元格(文本/BLOB)拼成一个大字符串。
func dumpEntireDB(t *testing.T, db *sql.DB) string {
	t.Helper()
	var b strings.Builder

	tblRows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	var tables []string
	for tblRows.Next() {
		var name string
		if err := tblRows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		tables = append(tables, name)
	}
	_ = tblRows.Close()

	for _, tbl := range tables {
		rows, err := db.Query(`SELECT * FROM "` + tbl + `"`)
		if err != nil {
			t.Fatalf("select * from %s: %v", tbl, err)
		}
		cols, err := rows.Columns()
		if err != nil {
			t.Fatalf("columns %s: %v", tbl, err)
		}
		for rows.Next() {
			cells := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range cells {
				ptrs[i] = &cells[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatalf("scan row %s: %v", tbl, err)
			}
			for _, c := range cells {
				switch val := c.(type) {
				case nil:
				case []byte:
					b.Write(val)
				case string:
					b.WriteString(val)
				default:
					// 数字/时间等:不可能含明文,跳过即可。
				}
				b.WriteByte('\n')
			}
		}
		_ = rows.Close()
	}
	return b.String()
}
