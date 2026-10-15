package promotion

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/atomlust/atomgo/internal/pipeline"
	"github.com/atomlust/atomgo/internal/storetest"
	"github.com/google/uuid"
)

// ---- 测试地基:真 SQLite store + 种子项目/运行 -----------------------------

func testStore(t *testing.T) (*Store, *sql.DB) {
	db := storetest.OpenDB(t)
	// 晋级链/变量现由 pipeline_settings 派生,故 Store 需要 settings 服务。
	return NewStore(db, pipeline.NewSettingsService(db, nil)), db
}

func seedProject(t *testing.T, db *sql.DB) string {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	credID := uuid.NewString()
	if _, err := db.Exec(
		`INSERT INTO credentials (id, name, type, scope, ciphertext, masked_value, created_at, updated_at)
		 VALUES (?, 'c', 'git_token', '', `+storetest.BlobHex(storetest.Dialect(), "00")+`, 'm', ?, ?)`,
		credID, now, now,
	); err != nil {
		t.Fatalf("seed credential: %v", err)
	}
	projID := uuid.NewString()
	if _, err := db.Exec(
		`INSERT INTO projects (id, name, repo_url, default_branch, credential_id, created_at, updated_at)
		 VALUES (?, 'acme', 'https://example.com/p.git', 'main', ?, ?, ?)`,
		projID, credID, now, now,
	); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	return projID
}

func seedRun(t *testing.T, db *sql.DB, projectID, status string) string {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	id := uuid.NewString()
	if _, err := db.Exec(
		`INSERT INTO pipeline_runs
		   (id, project_id, status, trigger_type, trigger_branch, trigger_commit, trigger_actor,
		    resolved_environment, resolved_target_server_ids, params_json, created_at)
		 VALUES (?, ?, ?, 'manual', 'main', '', 'admin', '', '[]', '{}', ?)`,
		id, projectID, status, now,
	); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	return id
}

// seedChain 直接落一条 pipeline_settings,配置晋级链(InChain / Gated)与可选逐环境变量。
// 绕开 settings.Save 的规范化(便于注入 secret 引用而无需保险库)。
func seedChain(t *testing.T, db *sql.DB, proj string, envs []map[string]any) {
	t.Helper()
	raw, err := json.Marshal(envs)
	if err != nil {
		t.Fatalf("marshal chain envs: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	// 先删后插,避免与 Get 惰性创建的空 settings 行产生主键冲突(幂等)。
	_, _ = db.Exec(`DELETE FROM pipeline_settings WHERE project_id = ?`, proj)
	if _, err := db.Exec(
		`INSERT INTO pipeline_settings (project_id, build_json, environments_json, created_at, updated_at)
		 VALUES (?, '{}', ?, ?, ?)`,
		proj, string(raw), now, now,
	); err != nil {
		t.Fatalf("seed pipeline_settings: %v", err)
	}
}

func seedCredential(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(
		`INSERT INTO credentials (id, name, type, scope, ciphertext, masked_value, created_at, updated_at)
		 VALUES (?, 'c', 'git_token', '', `+storetest.BlobHex(storetest.Dialect(), "00")+`, 'm', ?, ?)`,
		id, now, now,
	); err != nil {
		t.Fatalf("seed credential: %v", err)
	}
}

// ---- 测试替身:RunLookup / Gate / SecretResolver ----------------------------

type fakeRuns map[string]RunInfo

func (f fakeRuns) LookupRun(_ context.Context, runID string) (RunInfo, error) {
	r, ok := f[runID]
	if !ok {
		return RunInfo{}, ErrRunNotFound
	}
	return r, nil
}

// approveGate 总是批准;rejectGate 总是拒绝;errGate 返回 ctx 取消错误。
type approveGate struct{ actor string }

func (g approveGate) Await(_ context.Context, _ string, _ GateInfo) (bool, string, error) {
	return true, g.actor, nil
}

type rejectGate struct{}

func (rejectGate) Await(_ context.Context, _ string, _ GateInfo) (bool, string, error) {
	return false, "boss", nil
}

type errGate struct{}

func (errGate) Await(_ context.Context, _ string, _ GateInfo) (bool, string, error) {
	return false, "timeout", context.DeadlineExceeded
}

type fakeSecret map[string]string

func (f fakeSecret) Reveal(id string) (string, error) {
	v, ok := f[id]
	if !ok {
		return "", errors.New("not found")
	}
	return v, nil
}

// ---- 链模型单测 -------------------------------------------------------------

func TestChainNextTargetAndValidation(t *testing.T) {
	c := Chain{Environments: []EnvStage{{Name: "dev"}, {Name: "staging"}, {Name: "prod", Gated: true}}}

	// 首次晋级目标 = 链首。
	if got, err := c.nextTarget(""); err != nil || got != "dev" {
		t.Fatalf("nextTarget(\"\") = %q, %v; want dev", got, err)
	}
	// 中间级。
	if got, err := c.nextTarget("dev"); err != nil || got != "staging" {
		t.Fatalf("nextTarget(dev) = %q, %v; want staging", got, err)
	}
	// 链尾无下一级。
	if _, err := c.nextTarget("prod"); !errors.Is(err, ErrAlreadyAtTop) {
		t.Fatalf("nextTarget(prod) err = %v; want ErrAlreadyAtTop", err)
	}
	// 跳级被拒。
	if err := c.validateTarget("dev", "prod"); !errors.Is(err, ErrSkipEnv) {
		t.Fatalf("validateTarget(dev→prod) = %v; want ErrSkipEnv", err)
	}
	// 未知目标。
	if err := c.validateTarget("dev", "qa"); !errors.Is(err, ErrUnknownEnv) {
		t.Fatalf("validateTarget(dev→qa) = %v; want ErrUnknownEnv", err)
	}
	// 合法下一级。
	if err := c.validateTarget("dev", "staging"); err != nil {
		t.Fatalf("validateTarget(dev→staging) = %v; want nil", err)
	}
	if c.Gated("prod") != true || c.Gated("dev") != false {
		t.Fatalf("Gated wrong: prod=%v dev=%v", c.Gated("prod"), c.Gated("dev"))
	}
}

func TestValidateChainRejectsBadInput(t *testing.T) {
	if _, err := validateChain(Chain{}); !errors.Is(err, ErrEmptyChain) {
		t.Fatalf("empty chain err = %v", err)
	}
	if _, err := validateChain(Chain{Environments: []EnvStage{{Name: "dev"}, {Name: "dev"}}}); !errors.Is(err, ErrDuplicateEnv) {
		t.Fatalf("dup chain err = %v", err)
	}
	if _, err := validateChain(Chain{Environments: []EnvStage{{Name: "  "}}}); !errors.Is(err, ErrInvalidEnvName) {
		t.Fatalf("blank name err = %v", err)
	}
	// 去首尾空白。
	out, err := validateChain(Chain{Environments: []EnvStage{{Name: " dev "}, {Name: "prod", Gated: true}}})
	if err != nil {
		t.Fatalf("valid chain err = %v", err)
	}
	if out.Environments[0].Name != "dev" || !out.Environments[1].Gated {
		t.Fatalf("normalize wrong: %+v", out.Environments)
	}
}

// ---- 链派生单测(从 pipeline_settings) ---------------------------------------

func TestGetChainDerivesFromSettings(t *testing.T) {
	st, db := testStore(t)
	proj := seedProject(t, db)
	ctx := context.Background()

	// 无 InChain 环境 → ErrChainNotConfigured。
	if _, err := st.GetChain(ctx, proj); !errors.Is(err, ErrChainNotConfigured) {
		t.Fatalf("GetChain before config = %v; want ErrChainNotConfigured", err)
	}

	// 配置三个环境:dev / staging(InChain) / prod(InChain, gated) / sandbox(非 InChain)。
	seedChain(t, db, proj, []map[string]any{
		{"id": "1", "name": "dev", "inChain": true, "gated": false, "targetServerIds": []string{"s1"}, "envVars": []any{}, "imageRegistry": map[string]any{}},
		{"id": "2", "name": "staging", "inChain": true, "gated": false, "targetServerIds": []string{"s1"}, "envVars": []any{}, "imageRegistry": map[string]any{}},
		{"id": "3", "name": "prod", "inChain": true, "gated": true, "targetServerIds": []string{"s1"}, "envVars": []any{}, "imageRegistry": map[string]any{}},
		{"id": "4", "name": "sandbox", "inChain": false, "gated": false, "targetServerIds": []string{"s1"}, "envVars": []any{}, "imageRegistry": map[string]any{}},
	})

	got, err := st.GetChain(ctx, proj)
	if err != nil {
		t.Fatalf("GetChain: %v", err)
	}
	// 仅 InChain 环境,且按数组顺序;gated 透传。
	if len(got.Environments) != 3 {
		t.Fatalf("chain len = %d; want 3 (sandbox excluded)", len(got.Environments))
	}
	want := []EnvStage{{Name: "dev"}, {Name: "staging"}, {Name: "prod", Gated: true}}
	for i := range want {
		if got.Environments[i].Name != want[i].Name || got.Environments[i].Gated != want[i].Gated {
			t.Fatalf("chain[%d] = %+v; want %+v", i, got.Environments[i], want[i])
		}
	}
}

func TestGetChainUnknownProject(t *testing.T) {
	st, _ := testStore(t)
	// settings.Get 在项目不存在时返回 ErrProjectNotFound → 透传。
	if _, err := st.GetChain(context.Background(), uuid.NewString()); !errors.Is(err, pipeline.ErrProjectNotFound) {
		t.Fatalf("GetChain unknown project = %v; want ErrProjectNotFound", err)
	}
}

// ---- 编排状态机单测 ---------------------------------------------------------

func TestPromoteRejectsNonSuccessfulRun(t *testing.T) {
	st, db := testStore(t)
	proj := seedProject(t, db)
	runID := seedRun(t, db, proj, "failed")
	seedChain(t, db, proj, []map[string]any{
		{"id": "1", "name": "dev", "inChain": true, "gated": false, "targetServerIds": []string{"s1"}, "envVars": []any{}, "imageRegistry": map[string]any{}},
	})

	co := NewCoordinator(st, fakeRuns{runID: {ID: runID, ProjectID: proj, Status: "failed"}}, nil, nil)
	_, err := co.Promote(context.Background(), runID, "", "admin")
	if !errors.Is(err, ErrRunNotSuccessful) {
		t.Fatalf("Promote failed run = %v; want ErrRunNotSuccessful", err)
	}
}

func TestPromoteSequenceAndNoSkip(t *testing.T) {
	st, db := testStore(t)
	proj := seedProject(t, db)
	runID := seedRun(t, db, proj, "success")
	ctx := context.Background()
	seedChain(t, db, proj, []map[string]any{
		{"id": "1", "name": "dev", "inChain": true, "gated": false, "targetServerIds": []string{"s1"}, "envVars": []any{}, "imageRegistry": map[string]any{}},
		{"id": "2", "name": "staging", "inChain": true, "gated": false, "targetServerIds": []string{"s1"}, "envVars": []any{}, "imageRegistry": map[string]any{}},
		{"id": "3", "name": "prod", "inChain": true, "gated": true, "targetServerIds": []string{"s1"}, "envVars": []any{}, "imageRegistry": map[string]any{}},
	})

	co := NewCoordinator(st, fakeRuns{runID: {ID: runID, ProjectID: proj, Status: "success"}}, approveGate{actor: "boss"}, nil)

	// 不可跳级:首次目标必须是 dev,直接要 prod 被拒。
	if _, err := co.Promote(ctx, runID, "prod", "admin"); !errors.Is(err, ErrSkipEnv) {
		t.Fatalf("skip to prod = %v; want ErrSkipEnv", err)
	}

	// 晋级 dev(非 gated)。
	res, err := co.Promote(ctx, runID, "", "admin")
	if err != nil || res.Record.TargetEnvironment != "dev" || res.Record.Status != StatusPromoted {
		t.Fatalf("promote dev: %+v, %v", res, err)
	}
	// 重复晋级 dev 被拒。
	if _, err := co.Promote(ctx, runID, "dev", "admin"); !errors.Is(err, ErrAlreadyPromoted) {
		t.Fatalf("re-promote dev = %v; want ErrAlreadyPromoted", err)
	}
	// 晋级 staging。
	res, err = co.Promote(ctx, runID, "staging", "admin")
	if err != nil || res.Record.FromEnvironment != "dev" || res.Record.TargetEnvironment != "staging" {
		t.Fatalf("promote staging: %+v, %v", res, err)
	}
	// 晋级 prod(gated → approveGate 批准)。
	res, err = co.Promote(ctx, runID, "prod", "admin")
	if err != nil || res.Record.Status != StatusPromoted || res.Record.PromotedBy != "boss" {
		t.Fatalf("promote prod gated: %+v, %v", res, err)
	}
	// 已在链尾:再晋级 → ErrAlreadyAtTop。
	if _, err := co.Promote(ctx, runID, "", "admin"); !errors.Is(err, ErrAlreadyAtTop) {
		t.Fatalf("promote past prod = %v; want ErrAlreadyAtTop", err)
	}
}

func TestPromoteGatedRejected(t *testing.T) {
	st, db := testStore(t)
	proj := seedProject(t, db)
	runID := seedRun(t, db, proj, "success")
	ctx := context.Background()
	seedChain(t, db, proj, []map[string]any{
		{"id": "1", "name": "prod", "inChain": true, "gated": true, "targetServerIds": []string{"s1"}, "envVars": []any{}, "imageRegistry": map[string]any{}},
	})

	co := NewCoordinator(st, fakeRuns{runID: {ID: runID, ProjectID: proj, Status: "success"}}, rejectGate{}, nil)
	res, err := co.Promote(ctx, runID, "prod", "admin")
	if !errors.Is(err, ErrGateRejected) {
		t.Fatalf("gated reject err = %v; want ErrGateRejected", err)
	}
	if res == nil || res.Record.Status != StatusRejected {
		t.Fatalf("rejected record wrong: %+v", res)
	}
	// 被拒后该环境无 active 占用,可重新晋级。
	if active, _ := st.hasActivePromotion(ctx, runID, "prod"); active {
		t.Fatalf("rejected promotion should not be active")
	}
}

func TestPromoteGatedNoGateFailsClosed(t *testing.T) {
	st, db := testStore(t)
	proj := seedProject(t, db)
	runID := seedRun(t, db, proj, "success")
	ctx := context.Background()
	seedChain(t, db, proj, []map[string]any{
		{"id": "1", "name": "prod", "inChain": true, "gated": true, "targetServerIds": []string{"s1"}, "envVars": []any{}, "imageRegistry": map[string]any{}},
	})

	// gate=nil:gated 环境 fail-closed(绝不擅自放行)。
	co := NewCoordinator(st, fakeRuns{runID: {ID: runID, ProjectID: proj, Status: "success"}}, nil, nil)
	res, err := co.Promote(ctx, runID, "prod", "admin")
	if !errors.Is(err, ErrGateRejected) {
		t.Fatalf("no-gate gated promote = %v; want ErrGateRejected", err)
	}
	if res.Record.Status != StatusRejected {
		t.Fatalf("fail-closed record status = %q", res.Record.Status)
	}
}

func TestPromoteGatedTimeout(t *testing.T) {
	st, db := testStore(t)
	proj := seedProject(t, db)
	runID := seedRun(t, db, proj, "success")
	ctx := context.Background()
	seedChain(t, db, proj, []map[string]any{
		{"id": "1", "name": "prod", "inChain": true, "gated": true, "targetServerIds": []string{"s1"}, "envVars": []any{}, "imageRegistry": map[string]any{}},
	})

	co := NewCoordinator(st, fakeRuns{runID: {ID: runID, ProjectID: proj, Status: "success"}}, errGate{}, nil)
	res, err := co.Promote(ctx, runID, "prod", "admin")
	if err == nil {
		t.Fatalf("gated timeout expected error")
	}
	if res == nil || res.Record.Status != StatusRejected {
		t.Fatalf("timeout record wrong: %+v", res)
	}
}

func TestResolveEnvVarsDecryptsSecrets(t *testing.T) {
	st, db := testStore(t)
	proj := seedProject(t, db)
	seedCredential(t, db, "cred-1")
	ctx := context.Background()
	// 经 pipeline_settings 配置逐环境变量(secret 只存 credentialId 引用)。
	seedChain(t, db, proj, []map[string]any{
		{
			"id": "1", "name": "prod", "inChain": true, "gated": false,
			"targetServerIds": []string{"s1"}, "imageRegistry": map[string]any{},
			"envVars": []any{
				map[string]any{"id": "v1", "key": "LOG_LEVEL", "secret": false, "value": "info"},
				map[string]any{"id": "v2", "key": "TOKEN", "secret": true, "credentialId": "cred-1"},
				map[string]any{"id": "v3", "key": "MISSING", "secret": true, "credentialId": "cred-absent"},
			},
		},
	})

	co := NewCoordinator(st, fakeRuns{}, nil, fakeSecret{"cred-1": "s3cr3t"})
	vars, unresolved, err := co.ResolveEnvVars(ctx, proj, "prod")
	if err != nil {
		t.Fatalf("ResolveEnvVars: %v", err)
	}
	got := map[string]string{}
	for _, v := range vars {
		got[v.Key] = v.Value
	}
	if got["LOG_LEVEL"] != "info" || got["TOKEN"] != "s3cr3t" {
		t.Fatalf("resolved vars wrong: %+v", got)
	}
	if _, ok := got["MISSING"]; ok {
		t.Fatalf("missing-credential secret should not resolve")
	}
	if len(unresolved) != 1 || unresolved[0] != "MISSING" {
		t.Fatalf("unresolved = %v; want [MISSING]", unresolved)
	}
}

func TestPromoteRunNotFound(t *testing.T) {
	st, db := testStore(t)
	proj := seedProject(t, db)
	seedChain(t, db, proj, []map[string]any{
		{"id": "1", "name": "dev", "inChain": true, "gated": false, "targetServerIds": []string{"s1"}, "envVars": []any{}, "imageRegistry": map[string]any{}},
	})
	co := NewCoordinator(st, fakeRuns{}, nil, nil)
	if _, err := co.Promote(context.Background(), uuid.NewString(), "", "admin"); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("promote unknown run = %v; want ErrRunNotFound", err)
	}
}

func TestListRecordsForRunAndProject(t *testing.T) {
	st, db := testStore(t)
	proj := seedProject(t, db)
	runID := seedRun(t, db, proj, "success")
	ctx := context.Background()
	seedChain(t, db, proj, []map[string]any{
		{"id": "1", "name": "dev", "inChain": true, "gated": false, "targetServerIds": []string{"s1"}, "envVars": []any{}, "imageRegistry": map[string]any{}},
		{"id": "2", "name": "staging", "inChain": true, "gated": false, "targetServerIds": []string{"s1"}, "envVars": []any{}, "imageRegistry": map[string]any{}},
	})
	co := NewCoordinator(st, fakeRuns{runID: {ID: runID, ProjectID: proj, Status: "success"}}, nil, nil)
	if _, err := co.Promote(ctx, runID, "dev", "admin"); err != nil {
		t.Fatalf("promote dev: %v", err)
	}
	if _, err := co.Promote(ctx, runID, "staging", "admin"); err != nil {
		t.Fatalf("promote staging: %v", err)
	}
	runRecs, err := st.ListRecordsForRun(ctx, runID)
	if err != nil || len(runRecs) != 2 {
		t.Fatalf("ListRecordsForRun = %d, %v; want 2", len(runRecs), err)
	}
	projRecs, err := st.ListRecordsForProject(ctx, proj)
	if err != nil || len(projRecs) != 2 {
		t.Fatalf("ListRecordsForProject = %d, %v; want 2", len(projRecs), err)
	}
}
