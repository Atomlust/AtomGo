package trigger

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/atomlust/atomgo/internal/pipeline"
	"github.com/atomlust/atomgo/internal/vault"
)

// seedEnvironments 直接插入 pipeline_settings(含环境),供 EnvironmentResolver 经
// LoadExistingSettings 读取。环境含 id/name/targetServerIds 用于验证「服务器来自环境」。
func seedEnvironments(t *testing.T, db *sql.DB, projID string, envs []pipeline.Environment) {
	t.Helper()
	blob, err := json.Marshal(envs)
	if err != nil {
		t.Fatalf("marshal envs: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(
		`INSERT INTO pipeline_settings (project_id, build_json, environments_json, steps_json, created_at, updated_at)
		 VALUES (?, '{}', ?, '[]', ?, ?)
		 ON CONFLICT(project_id) DO UPDATE SET environments_json = excluded.environments_json, updated_at = excluded.updated_at`,
		projID, string(blob), now, now,
	)
	if err != nil {
		t.Fatalf("seed pipeline_settings: %v", err)
	}
}

// saveTriggerMappings 经真实 Save 落地分支映射(含 webhook_token + 加密密钥)。
func saveTriggerMappings(t *testing.T, svc Service, projID string, maps []BranchMapping) {
	t.Helper()
	_, err := svc.Save(context.Background(), projID, SaveInput{
		Events:          Events{Push: true},
		BranchMappings:  maps,
		UnmatchedPolicy: PolicyRecord,
	})
	if err != nil {
		t.Fatalf("Save mappings: %v", err)
	}
}

func TestResolveEnvUsesEnvironmentIDAndServersFromEnv(t *testing.T) {
	svc, db, _, projID := newSvc(t)
	const envID, envName = "env-prod", "生产"
	seedEnvironments(t, db, projID, []pipeline.Environment{
		{ID: envID, Name: envName, TargetServerIDs: []string{"srv-a", "srv-b"}},
	})
	// 映射用 EnvironmentID 引用;Environment(名)故意写错,证明 ID 优先且服务器取自环境。
	saveTriggerMappings(t, svc, projID, []BranchMapping{
		{BranchPattern: "main", Environment: "错名", EnvironmentID: envID},
	})

	r := &EnvironmentResolver{db: db}
	env, servers := r.ResolveEnv(context.Background(), projID, "main")
	if env != envName {
		t.Fatalf("解析出的环境名应为 %q(来自被引用环境的 Name), got %q", envName, env)
	}
	if strings.Join(servers, ",") != "srv-a,srv-b" {
		t.Fatalf("目标服务器应来自环境 TargetServerIDs, got %v", servers)
	}
}

func TestResolveEnvFallsBackToName(t *testing.T) {
	svc, db, _, projID := newSvc(t)
	seedEnvironments(t, db, projID, []pipeline.Environment{
		{ID: "env-stg", Name: "预发", TargetServerIDs: []string{"srv-stg"}},
	})
	// 存量数据:仅用名引用,无 EnvironmentID。
	saveTriggerMappings(t, svc, projID, []BranchMapping{
		{BranchPattern: "release/*", Environment: "预发"},
	})

	r := &EnvironmentResolver{db: db}
	env, servers := r.ResolveEnv(context.Background(), projID, "release/1.2")
	if env != "预发" {
		t.Fatalf("降级按名解析应为 %q, got %q", "预发", env)
	}
	if strings.Join(servers, ",") != "srv-stg" {
		t.Fatalf("服务器应来自预发环境, got %v", servers)
	}
}

func TestResolveEnvDanglingIDFallsBackToMappingServers(t *testing.T) {
	svc, db, _, projID := newSvc(t)
	seedEnvironments(t, db, projID, []pipeline.Environment{
		{ID: "env-real", Name: "生产", TargetServerIDs: []string{"srv-real"}},
	})
	// 悬空 ID + 名不符 + 自带兜底服务器:环境找不到时应回退用映射自带服务器。
	saveTriggerMappings(t, svc, projID, []BranchMapping{
		{BranchPattern: "main", Environment: "旧名", EnvironmentID: "ghost", TargetServerIDs: []string{"srv-fallback"}},
	})

	r := &EnvironmentResolver{db: db}
	env, servers := r.ResolveEnv(context.Background(), projID, "main")
	if env != "旧名" {
		t.Fatalf("兜底环境名应为映射自带 %q, got %q", "旧名", env)
	}
	if strings.Join(servers, ",") != "srv-fallback" {
		t.Fatalf("兜底服务器应为映射自带, got %v", servers)
	}
}

func TestResolveMappingDirect(t *testing.T) {
	db, _ := testDB(t)
	r := &EnvironmentResolver{db: db}
	projID := seedProject(t, db)
	seedEnvironments(t, db, projID, []pipeline.Environment{
		{ID: "e1", Name: "生产", TargetServerIDs: []string{"a"}},
		{ID: "e2", Name: "预发", TargetServerIDs: []string{"b"}},
	})

	// 1) ID 优先。
	env, servers := r.ResolveMapping(context.Background(), projID, BranchMapping{EnvironmentID: "e2", Environment: "生产"})
	if env != "预发" || strings.Join(servers, ",") != "b" {
		t.Fatalf("ID 优先失败: env=%q servers=%v", env, servers)
	}

	// 2) ID 缺失 → 名匹配。
	env, servers = r.ResolveMapping(context.Background(), projID, BranchMapping{Environment: "生产"})
	if env != "生产" || strings.Join(servers, ",") != "a" {
		t.Fatalf("名匹配失败: env=%q servers=%v", env, servers)
	}

	// 3) 都找不到 → 兜底映射自带。
	env, servers = r.ResolveMapping(context.Background(), projID, BranchMapping{Environment: "消失", TargetServerIDs: []string{"f"}})
	if env != "消失" || strings.Join(servers, ",") != "f" {
		t.Fatalf("兜底失败: env=%q servers=%v", env, servers)
	}
}

func TestSaveRejectsInvalidEnvironmentID(t *testing.T) {
	db, _ := testDB(t)
	v := vault.New(db, testMasterKey())
	// 注入 settings 以启用 EnvironmentID 校验。
	svc := New(db, v, WithSettings(pipeline.NewSettingsService(db, v)))
	projID := seedProject(t, db)
	seedEnvironments(t, db, projID, []pipeline.Environment{
		{ID: "e1", Name: "生产", TargetServerIDs: []string{"a"}},
	})

	// 悬空 ID → 应被保存期拦截。
	_, err := svc.Save(context.Background(), projID, SaveInput{
		Events:          Events{Push: true},
		BranchMappings:  []BranchMapping{{BranchPattern: "main", Environment: "生产", EnvironmentID: "ghost"}},
		UnmatchedPolicy: PolicyRecord,
	})
	if !errors.Is(err, ErrEnvironmentNotFound) {
		t.Fatalf("悬空 EnvironmentID 应返回 ErrEnvironmentNotFound, got %v", err)
	}

	// 有效 ID → 通过。
	_, err = svc.Save(context.Background(), projID, SaveInput{
		Events:          Events{Push: true},
		BranchMappings:  []BranchMapping{{BranchPattern: "main", Environment: "生产", EnvironmentID: "e1"}},
		UnmatchedPolicy: PolicyRecord,
	})
	if err != nil {
		t.Fatalf("有效 EnvironmentID 应保存成功, got %v", err)
	}
}

func TestSaveStaleNoIDStillSaves(t *testing.T) {
	svc, _, _, projID := newSvc(t)
	// 存量/旧前端:无 EnvironmentID、甚至 Environment 也空,应仍保存成功(向后兼容)。
	_, err := svc.Save(context.Background(), projID, SaveInput{
		Events:          Events{Push: true},
		BranchMappings:  []BranchMapping{{BranchPattern: "main"}},
		UnmatchedPolicy: PolicyRecord,
	})
	if err != nil {
		t.Fatalf("无引用映射应向后兼容保存, got %v", err)
	}
}

func TestReceiverResolveUsesID(t *testing.T) {
	db, _ := testDB(t)
	v := vault.New(db, testMasterKey())
	projID := seedProject(t, db)
	svc := New(db, v)
	saveTriggerMappings(t, svc, projID, []BranchMapping{
		{BranchPattern: "main", Environment: "生产", EnvironmentID: "e1"},
	})
	seedEnvironments(t, db, projID, []pipeline.Environment{
		{ID: "e1", Name: "生产", TargetServerIDs: []string{"srv-x"}},
	})
	rc := NewReceiver(db, v, nil)
	if rc.envResolver == nil {
		t.Fatal("Receiver 应装配 envResolver")
	}
	env, servers := rc.envResolver.ResolveEnv(context.Background(), projID, "main")
	if env != "生产" || strings.Join(servers, ",") != "srv-x" {
		t.Fatalf("webhook 路径解析失败: env=%q servers=%v", env, servers)
	}
}
