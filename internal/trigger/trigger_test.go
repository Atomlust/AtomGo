package trigger

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/atomlust/atomgo/internal/storetest"
	"github.com/atomlust/atomgo/internal/vault"
	"github.com/google/uuid"
)

// testMasterKey 返回确定性测试用 master key。
func testMasterKey() *[32]byte {
	var k [32]byte
	for i := range k {
		k[i] = byte(i + 7)
	}
	return &k
}

// testDB 打开临时 SQLite(含全部迁移),返回 *sql.DB 与库文件路径(供整库 dump)。
func testDB(t *testing.T) (*sql.DB, string) {
	return storetest.OpenDBWithPath(t)
}

// seedProject 直接插一个项目(本测试不关心仓库校验),返回 project id。
// 先建一个最小凭据满足外键。
func seedProject(t *testing.T, db *sql.DB) string {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	credID := uuid.NewString()
	_, err := db.Exec(
		`INSERT INTO credentials (id, name, type, scope, ciphertext, masked_value, created_at, updated_at)
		 VALUES (?, 'c', 'git_token', '', `+storetest.BlobHex(storetest.Dialect(), "00")+`, 'm', ?, ?)`,
		credID, now, now,
	)
	if err != nil {
		t.Fatalf("seed credential: %v", err)
	}
	projID := uuid.NewString()
	_, err = db.Exec(
		`INSERT INTO projects (id, name, repo_url, default_branch, credential_id, created_at, updated_at)
		 VALUES (?, 'p', 'https://example.com/p.git', 'main', ?, ?, ?)`,
		projID, credID, now, now,
	)
	if err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return projID
}

func newSvc(t *testing.T) (Service, *sql.DB, string, string) {
	t.Helper()
	db, dbPath := testDB(t)
	v := vault.New(db, testMasterKey())
	svc := New(db, v)
	projID := seedProject(t, db)
	return svc, db, dbPath, projID
}

func TestGetLazyDefault(t *testing.T) {
	svc, _, _, projID := newSvc(t)
	cfg, err := svc.Get(context.Background(), projID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if cfg.WebhookToken == "" {
		t.Fatal("默认应生成 webhook token")
	}
	if cfg.Events.Push || cfg.Events.Tag || cfg.Events.PullRequest {
		t.Fatalf("默认 events 应全 false, got %+v", cfg.Events)
	}
	if len(cfg.BranchMappings) != 0 {
		t.Fatalf("默认映射应为空, got %d", len(cfg.BranchMappings))
	}
	if cfg.UnmatchedPolicy != PolicyRecord {
		t.Fatalf("默认策略应为 record, got %s", cfg.UnmatchedPolicy)
	}
	if !strings.HasPrefix(cfg.WebhookSecretMasked, secretPrefix) || !strings.Contains(cfg.WebhookSecretMasked, "••••") {
		t.Fatalf("密钥应掩码, got %q", cfg.WebhookSecretMasked)
	}
	if strings.Contains(cfg.WebhookSecretMasked, "whsec_"+strings.Repeat("0", 10)) {
		t.Fatal("掩码不应含完整密钥体")
	}

	// 二次 Get 幂等:不应重新生成 token。
	cfg2, err := svc.Get(context.Background(), projID)
	if err != nil {
		t.Fatalf("Get 2: %v", err)
	}
	if cfg2.WebhookToken != cfg.WebhookToken {
		t.Fatalf("重复 Get 不应换 token: %s != %s", cfg2.WebhookToken, cfg.WebhookToken)
	}
}

func TestGetProjectNotFound(t *testing.T) {
	svc, _, _, _ := newSvc(t)
	_, err := svc.Get(context.Background(), uuid.NewString())
	if err != ErrProjectNotFound {
		t.Fatalf("err = %v, want ErrProjectNotFound", err)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	svc, _, _, projID := newSvc(t)
	in := SaveInput{
		Events: Events{Push: true, Tag: false, PullRequest: true},
		BranchMappings: []BranchMapping{
			{BranchPattern: "main", Environment: "生产", TargetServerIDs: []string{"srv-1", "srv-2"}},
			{BranchPattern: "release/*", Environment: "预发"},
		},
		UnmatchedPolicy: PolicyIgnore,
	}
	saved, err := svc.Save(context.Background(), projID, in)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !saved.Events.Push || saved.Events.Tag || !saved.Events.PullRequest {
		t.Fatalf("events 未持久化正确: %+v", saved.Events)
	}
	if saved.UnmatchedPolicy != PolicyIgnore {
		t.Fatalf("policy = %s, want ignore", saved.UnmatchedPolicy)
	}
	if len(saved.BranchMappings) != 2 {
		t.Fatalf("映射数 = %d, want 2", len(saved.BranchMappings))
	}
	for _, m := range saved.BranchMappings {
		if m.ID == "" {
			t.Fatal("映射应补全 id")
		}
		if m.TargetServerIDs == nil {
			t.Fatal("targetServerIds 应非 nil")
		}
	}

	// 回读验证持久化。
	got, err := svc.Get(context.Background(), projID)
	if err != nil {
		t.Fatalf("Get after save: %v", err)
	}
	if len(got.BranchMappings) != 2 || got.BranchMappings[0].Environment != "生产" {
		t.Fatalf("回读映射不符: %+v", got.BranchMappings)
	}
}

// TestSavePathFiltersRoundTrip 验证路径过滤 glob 往返:trim、剔空、去重、保序;回读一致。
func TestSavePathFiltersRoundTrip(t *testing.T) {
	svc, _, _, projID := newSvc(t)
	in := SaveInput{
		Events:          Events{Push: true},
		UnmatchedPolicy: PolicyRecord,
		PathFilters:     []string{" backend/** ", "frontend/**", "backend/**", "  ", "*.go"},
	}
	saved, err := svc.Save(context.Background(), projID, in)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	want := []string{"backend/**", "frontend/**", "*.go"}
	if len(saved.PathFilters) != len(want) {
		t.Fatalf("path filters = %v, want %v", saved.PathFilters, want)
	}
	for i, p := range want {
		if saved.PathFilters[i] != p {
			t.Fatalf("path filter[%d] = %q, want %q (order/trim/dedup)", i, saved.PathFilters[i], p)
		}
	}
	got, err := svc.Get(context.Background(), projID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.PathFilters) != len(want) {
		t.Fatalf("回读路径过滤不符: %v", got.PathFilters)
	}
}

// TestSaveInvalidPathFilter:含空白的 glob 被拒(ErrInvalidPathFilter)。
func TestSaveInvalidPathFilter(t *testing.T) {
	svc, _, _, projID := newSvc(t)
	_, err := svc.Save(context.Background(), projID, SaveInput{
		Events:          Events{Push: true},
		UnmatchedPolicy: PolicyRecord,
		PathFilters:     []string{"back end/**"},
	})
	if !errors.Is(err, ErrInvalidPathFilter) {
		t.Fatalf("err = %v, want ErrInvalidPathFilter", err)
	}
}

func TestSaveInvalidBranchPattern(t *testing.T) {
	svc, _, _, projID := newSvc(t)
	cases := []string{"", "  ", "feature branch", "*", "**"}
	for _, p := range cases {
		_, err := svc.Save(context.Background(), projID, SaveInput{
			BranchMappings:  []BranchMapping{{BranchPattern: p, Environment: "e"}},
			UnmatchedPolicy: PolicyRecord,
		})
		if err != ErrInvalidBranchPattern {
			t.Fatalf("pattern %q: err = %v, want ErrInvalidBranchPattern", p, err)
		}
	}
}

func TestSaveValidWildcards(t *testing.T) {
	svc, _, _, projID := newSvc(t)
	for _, p := range []string{"main", "dev", "release/*", "feature/*-hotfix", "v*.*"} {
		_, err := svc.Save(context.Background(), projID, SaveInput{
			BranchMappings:  []BranchMapping{{BranchPattern: p, Environment: "e"}},
			UnmatchedPolicy: PolicyRecord,
		})
		if err != nil {
			t.Fatalf("pattern %q should be valid, got %v", p, err)
		}
	}
}

// TestSaveDuplicateBranchPattern:trim 后重复的分支模式被拒(ErrDuplicateBranchPattern)。
// matchBranch 首条命中生效,重复条等于静默废弃,必须在入库前拦截。
func TestSaveDuplicateBranchPattern(t *testing.T) {
	svc, _, _, projID := newSvc(t)
	cases := [][]BranchMapping{
		{{BranchPattern: "release/*", Environment: "a"}, {BranchPattern: "release/*", Environment: "b"}},
		{{BranchPattern: "main", Environment: "a"}, {BranchPattern: " main ", Environment: "b"}},
		{{BranchPattern: "v*.*", Environment: "a"}, {BranchPattern: "v*.*", Environment: "b"}, {BranchPattern: "main", Environment: "c"}},
	}
	for i, mappings := range cases {
		_, err := svc.Save(context.Background(), projID, SaveInput{
			BranchMappings:  mappings,
			UnmatchedPolicy: PolicyRecord,
		})
		if !errors.Is(err, ErrDuplicateBranchPattern) {
			t.Fatalf("case %d: err = %v, want ErrDuplicateBranchPattern", i, err)
		}
	}
	// 不同模式(即使都含通配)不视为重复。
	if _, err := svc.Save(context.Background(), projID, SaveInput{
		BranchMappings:  []BranchMapping{{BranchPattern: "release/*", Environment: "a"}, {BranchPattern: "release/1.*", Environment: "b"}},
		UnmatchedPolicy: PolicyRecord,
	}); err != nil {
		t.Fatalf("distinct wildcard patterns should be valid, got %v", err)
	}
}

func TestSaveInvalidPolicy(t *testing.T) {
	svc, _, _, projID := newSvc(t)
	_, err := svc.Save(context.Background(), projID, SaveInput{UnmatchedPolicy: "deploy"})
	if err != ErrInvalidPolicy {
		t.Fatalf("err = %v, want ErrInvalidPolicy", err)
	}
}

func TestResetSecretInvalidatesOld(t *testing.T) {
	svc, db, _, projID := newSvc(t)

	// 触发惰性默认,拿到首版密文。
	if _, err := svc.Get(context.Background(), projID); err != nil {
		t.Fatalf("Get: %v", err)
	}
	oldCipher := readCipher(t, db, projID)

	res, err := svc.ResetSecret(context.Background(), projID)
	if err != nil {
		t.Fatalf("ResetSecret: %v", err)
	}
	if !strings.HasPrefix(res.Secret, secretPrefix) {
		t.Fatalf("回显密钥应带前缀: %q", res.Secret)
	}
	if !strings.Contains(res.Masked, "••••") {
		t.Fatalf("应同时给掩码: %q", res.Masked)
	}

	newCipher := readCipher(t, db, projID)
	if string(oldCipher) == string(newCipher) {
		t.Fatal("重置后密文应改变(旧值失效)")
	}

	// 新密文应能解密回显的明文。
	v := vault.New(db, testMasterKey())
	plain, err := v.OpenSecret(newCipher)
	if err != nil {
		t.Fatalf("OpenSecret: %v", err)
	}
	if string(plain) != res.Secret {
		t.Fatalf("新密文解密 != 回显明文")
	}
	// 旧密文解出的明文与回显明文不同(旧值确已失效)。
	oldPlain, err := v.OpenSecret(oldCipher)
	if err != nil {
		t.Fatalf("OpenSecret old: %v", err)
	}
	if string(oldPlain) == res.Secret {
		t.Fatal("旧密钥应已失效")
	}
}

// TestSecAfterDBNoPlaintext 断言:整库二进制内容不含任何 webhook 密钥明文(AC-SEC)。
func TestSecAfterDBNoPlaintext(t *testing.T) {
	svc, db, dbPath, projID := newSvc(t)

	// 默认密钥的明文(经 Get 惰性生成),再 reset 一次确保覆盖两版。
	if _, err := svc.Get(context.Background(), projID); err != nil {
		t.Fatalf("Get: %v", err)
	}
	res, err := svc.ResetSecret(context.Background(), projID)
	if err != nil {
		t.Fatalf("ResetSecret: %v", err)
	}
	// 检出点 + WAL 全部落盘后再 dump:用 wal_checkpoint 强制写回主库文件。
	_, _ = db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)

	if dbPath != "" {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			raw := readFileMaybe(t, dbPath+suffix)
			if strings.Contains(string(raw), res.Secret) {
				t.Fatalf("整库文件 %s 含 webhook 密钥明文!", dbPath+suffix)
			}
			// 密钥体(去前缀)也不应出现。
			body := strings.TrimPrefix(res.Secret, secretPrefix)
			if strings.Contains(string(raw), body) {
				t.Fatalf("整库文件 %s 含密钥体明文!", dbPath+suffix)
			}
		}
	}
}

func TestResetVaultUnconfigured(t *testing.T) {
	db, _ := testDB(t)
	projID := seedProject(t, db)
	// vault 未配置 master key。
	svc := New(db, vault.New(db, nil))
	if _, err := svc.Get(context.Background(), projID); err != ErrVaultUnconfigured {
		t.Fatalf("Get err = %v, want ErrVaultUnconfigured", err)
	}
	if _, err := svc.ResetSecret(context.Background(), projID); err != ErrVaultUnconfigured {
		t.Fatalf("Reset err = %v, want ErrVaultUnconfigured", err)
	}
}

func TestDeleteProjectCascadesTrigger(t *testing.T) {
	svc, db, _, projID := newSvc(t)
	if _, err := svc.Get(context.Background(), projID); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM projects WHERE id = ?`, projID); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(1) FROM pipeline_triggers WHERE project_id = ?`, projID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("删项目应级联删触发配置, 残留 %d 行", n)
	}
}

// readFileMaybe 读取文件;不存在返回 nil(WAL/SHM 可能尚未生成)。
func readFileMaybe(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

func readCipher(t *testing.T, db *sql.DB, projID string) []byte {
	t.Helper()
	var c []byte
	if err := db.QueryRow(
		`SELECT webhook_secret_ciphertext FROM pipeline_triggers WHERE project_id = ?`, projID,
	).Scan(&c); err != nil {
		t.Fatalf("read cipher: %v", err)
	}
	return c
}

// TestRevealSecretDoesNotRotate 验证 RevealSecret 回显当前生效密钥的明文且**不轮换**:
// 连续两次 reveal 得到同一明文;reset 之后 reveal 才跟着变成新值。
// 这是「UI 复制密钥」按钮的核心保证:取明文不能影响仓库里已配好的 webhook。
func TestRevealSecretDoesNotRotate(t *testing.T) {
	svc, _, _, projID := newSvc(t)
	ctx := context.Background()

	// 惰性生成默认配置(token + 加密密钥)。
	if _, err := svc.Get(ctx, projID); err != nil {
		t.Fatalf("Get: %v", err)
	}

	first, err := svc.RevealSecret(ctx, projID)
	if err != nil {
		t.Fatalf("RevealSecret: %v", err)
	}
	if !strings.HasPrefix(first.Secret, secretPrefix) {
		t.Fatalf("回显密钥应带 %s 前缀, got %q", secretPrefix, first.Secret)
	}
	if strings.Contains(first.Secret, "••••") {
		t.Fatalf("回显必须是明文,不得为掩码: %q", first.Secret)
	}
	if first.Masked == first.Secret {
		t.Fatal("掩码不应等于明文")
	}

	again, err := svc.RevealSecret(ctx, projID)
	if err != nil {
		t.Fatalf("reveal again: %v", err)
	}
	if again.Secret != first.Secret {
		t.Fatalf("reveal 不得轮换密钥: 首次 %q, 再次 %q", first.Secret, again.Secret)
	}

	// 重置后才应换成新值。
	rotated, err := svc.ResetSecret(ctx, projID)
	if err != nil {
		t.Fatalf("ResetSecret: %v", err)
	}
	if rotated.Secret == first.Secret {
		t.Fatal("reset 后密钥应当改变")
	}
	after, err := svc.RevealSecret(ctx, projID)
	if err != nil {
		t.Fatalf("reveal after reset: %v", err)
	}
	if after.Secret != rotated.Secret {
		t.Fatalf("reveal 应回显 reset 后的新值: got %q, want %q", after.Secret, rotated.Secret)
	}
}
