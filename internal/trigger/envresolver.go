package trigger

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/atomlust/atomgo/internal/pipeline"
)

// EnvironmentResolver 把(项目, 分支)解析为映射的环境名 + 目标服务器引用 id 列表,
// 复用与 webhook 接收路径**完全相同**的分支映射 + glob 匹配(matchBranch)。
//
// 用途(#56):手动 / 定时 / 串联触发经 run.Service.Create 创建运行时不走 webhook 接收器,
// 此前恒不解析环境(ResolvedEnvironment 空)→ build_image 不知推哪个 registry、环境变量 /
// 目标服务器全落空。注入本解析器后,这些触发也按项目分支映射补解析,与 webhook 一致。
//
// 只读 branch_mappings_json / pipeline_settings(不碰 webhook 签名密钥 / master key);
// 实现 run.EnvResolver。
type EnvironmentResolver struct {
	db *sql.DB
}

// NewEnvironmentResolver 构造分支→环境解析器(db 经参数化 SQL 触库)。
func NewEnvironmentResolver(db *sql.DB) *EnvironmentResolver {
	return &EnvironmentResolver{db: db}
}

// ResolveEnv 返回 branch 在项目分支映射中首条命中映射所解析出的环境名 + 目标服务器 id 列表。
//
// 环境定位优先用映射的 EnvironmentID(根治#:环境改名也不怕静默错配),缺失 / 失效时降级用
// Environment(名);目标服务器**强制来自被引用环境的 Environment.TargetServerIDs**(唯一真相源,
// 根治#),仅在「环境找不到」的极端兜底才回退用映射自带 TargetServerIDs(完全向后兼容)。
// 无配置 / 空分支 / 无命中 / 解析失败 → ("", nil)(调用方据此保持「未解析」语义)。
func (r *EnvironmentResolver) ResolveEnv(ctx context.Context, projectID, branch string) (string, []string) {
	branch = strings.TrimSpace(branch)
	projectID = strings.TrimSpace(projectID)
	if r == nil || r.db == nil || branch == "" || projectID == "" {
		return "", nil
	}
	m, ok := r.matchInDB(ctx, projectID, branch)
	if !ok {
		return "", nil
	}
	return r.ResolveMapping(ctx, projectID, m)
}

// matchInDB 从库读取分支映射并按 branch 首条 glob 命中(复用 matchBranch)。
func (r *EnvironmentResolver) matchInDB(ctx context.Context, projectID, branch string) (BranchMapping, bool) {
	var mappingsJSON string
	err := r.db.QueryRowContext(ctx,
		`SELECT branch_mappings_json FROM pipeline_triggers WHERE project_id = ?`, projectID,
	).Scan(&mappingsJSON)
	if err != nil || strings.TrimSpace(mappingsJSON) == "" {
		return BranchMapping{}, false
	}
	var mappings []BranchMapping
	if jerr := json.Unmarshal([]byte(mappingsJSON), &mappings); jerr != nil {
		return BranchMapping{}, false
	}
	return matchBranch(branch, mappings)
}

// ResolveMapping 把一条命中映射解析为(环境名, 目标服务器 id 列表)。
//
// 环境定位:优先 EnvironmentID(根治#,改名安全),降级 Environment(名);二者都找不到 →
// 兜底返回 (m.Environment, m.TargetServerIDs)(完全向后兼容旧行为,不静默丢部署)。
// 目标服务器:环境命中即取该环境的 Environment.TargetServerIDs(唯一真相源,根治#——分支映射
// 不再承载生效的服务器列表);仅兜底分支才用映射自带 TargetServerIDs。
func (r *EnvironmentResolver) ResolveMapping(ctx context.Context, projectID string, m BranchMapping) (string, []string) {
	if env, ok := r.findEnvironment(ctx, projectID, m); ok {
		servers := env.TargetServerIDs
		if servers == nil {
			servers = []string{}
		}
		return env.Name, servers
	}
	// 兜底:环境引用失效(悬空 ID / 名字不符 / 项目无环境配置),保留旧语义。
	servers := m.TargetServerIDs
	if servers == nil {
		servers = []string{}
	}
	return m.Environment, servers
}

// findEnvironment 按映射引用定位环境:**优先 EnvironmentID**,降级 Environment(名)。
// 任何读取错误 / 无配置 / 查不到 → (nil, false),交由 ResolveMapping 兜底。
func (r *EnvironmentResolver) findEnvironment(ctx context.Context, projectID string, m BranchMapping) (*pipeline.Environment, bool) {
	st, err := pipeline.LoadExistingSettings(ctx, r.db, projectID)
	if err != nil {
		return nil, false
	}
	id := strings.TrimSpace(m.EnvironmentID)
	name := strings.TrimSpace(m.Environment)
	if id != "" {
		for i := range st.Environments {
			if strings.TrimSpace(st.Environments[i].ID) == id {
				return &st.Environments[i], true
			}
		}
	}
	if name != "" {
		for i := range st.Environments {
			if strings.TrimSpace(st.Environments[i].Name) == name {
				return &st.Environments[i], true
			}
		}
	}
	return nil, false
}
