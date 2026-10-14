package promotion

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/atomlust/atomgo/internal/pipeline"
	"github.com/atomlust/atomgo/internal/store"
)

// Store 持久化晋级记录(run_promotions)。环境链与逐环境变量不再独立存储,
// 而是从流水线配置(pipeline_settings)派生 —— 这是合并「环境晋级链」与「环境与凭据」
// 后的单一真相源(见 pipeline.Environment 的 InChain / Gated 字段)。
type Store struct {
	db       *sql.DB
	settings pipeline.SettingsService
}

// NewStore 构造持久层。settings 用于派生环境链与逐环境变量。
func NewStore(db *sql.DB, settings pipeline.SettingsService) *Store {
	return &Store{db: db, settings: settings}
}

// GetChain 取某项目的环境晋级链(从流水线配置派生)。
// 仅纳入 InChain=true 的环境,且按 environments 数组顺序(即晋级顺序);
// 无任何 InChain 环境 → ErrChainNotConfigured。
func (s *Store) GetChain(ctx context.Context, projectID string) (Chain, error) {
	st, err := s.settings.Get(ctx, projectID)
	if err != nil {
		// 项目不存在 → 透传(上层映射为 404)。
		return Chain{}, err
	}
	stages := make([]EnvStage, 0, len(st.Environments))
	for _, e := range st.Environments {
		if e.InChain {
			stages = append(stages, EnvStage{Name: e.Name, Gated: e.Gated})
		}
	}
	if len(stages) == 0 {
		return Chain{}, ErrChainNotConfigured
	}
	return Chain{Environments: stages}, nil
}

// EnvironmentTargets 返回某环境的 TargetServerIDs(供晋级部署把源 run 产物发到目标环境机群)。
// 环境不存在 → (nil, nil)。项目不存在 → 透传错误(上层映射 404)。
func (s *Store) EnvironmentTargets(ctx context.Context, projectID, env string) ([]string, error) {
	st, err := s.settings.Get(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, e := range st.Environments {
		if e.Name == env {
			return e.TargetServerIDs, nil
		}
	}
	return nil, nil
}

// currentEnvForRun 返回某源运行已晋级到的「最高」环境(按链序)。
// 无任何 promoted 记录 → 空串(尚未晋级)。
func (s *Store) currentEnvForRun(ctx context.Context, runID string, c Chain) (string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT target_environment FROM run_promotions
		 WHERE source_run_id = ? AND status = ?`, runID, StatusPromoted)
	if err != nil {
		return "", fmt.Errorf("promotion: query promoted envs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	best := -1
	for rows.Next() {
		var env string
		if err := rows.Scan(&env); err != nil {
			return "", fmt.Errorf("promotion: scan promoted env: %w", err)
		}
		if i := c.IndexOf(env); i > best {
			best = i
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if best < 0 {
		return "", nil
	}
	return c.Environments[best].Name, nil
}

// hasActivePromotion 报告某源运行到某环境是否已有 pending/promoted 记录(防重复晋级/并发重入)。
func (s *Store) hasActivePromotion(ctx context.Context, runID, env string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM run_promotions
		 WHERE source_run_id = ? AND target_environment = ? AND status IN (?, ?)`,
		runID, env, StatusPending, StatusPromoted).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("promotion: check active promotion: %w", err)
	}
	return n > 0, nil
}

// createRecord 登记一条晋级记录(初始 status)。
func (s *Store) createRecord(ctx context.Context, rec Record) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	decided := ""
	if rec.Status == StatusPromoted || rec.Status == StatusRejected {
		decided = now
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO run_promotions
		   (id, project_id, source_run_id, from_environment, target_environment,
		    status, approval_stage, promoted_by, created_at, decided_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, rec.ProjectID, rec.SourceRunID, rec.FromEnvironment, rec.TargetEnvironment,
		rec.Status, rec.ApprovalStage, rec.PromotedBy, now, decided,
	)
	if err != nil {
		if isForeignKeyErr(err) {
			return "", ErrRunNotFound
		}
		return "", fmt.Errorf("promotion: create record: %w", err)
	}
	return id, nil
}

// decideRecord 把某记录置为终态(promoted/rejected)+ 记决定时刻 + 决定人。
func (s *Store) decideRecord(ctx context.Context, id, status, by string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE run_promotions SET status = ?, decided_at = ?, promoted_by = ? WHERE id = ?`,
		status, now, by, id)
	if err != nil {
		return fmt.Errorf("promotion: decide record: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("promotion: decide record: not found")
	}
	return nil
}

// ListRecordsForRun 返回某源运行的全部晋级记录(按 created_at 升序)。
func (s *Store) ListRecordsForRun(ctx context.Context, runID string) ([]Record, error) {
	return s.queryRecords(ctx, `WHERE source_run_id = ?`, runID)
}

// ListRecordsForProject 返回某项目的全部晋级记录(按 created_at 降序,最近在前)。
func (s *Store) ListRecordsForProject(ctx context.Context, projectID string) ([]Record, error) {
	return s.queryRecords(ctx, `WHERE project_id = ? ORDER BY created_at DESC, id DESC`, projectID)
}

func (s *Store) queryRecords(ctx context.Context, clause, arg string) ([]Record, error) {
	q := `SELECT id, project_id, source_run_id, from_environment, target_environment,
	             status, approval_stage, promoted_by, created_at, decided_at
	      FROM run_promotions ` + clause
	if !strings.Contains(clause, "ORDER BY") {
		q += " ORDER BY created_at ASC, id ASC"
	}
	rows, err := s.db.QueryContext(ctx, q, arg)
	if err != nil {
		return nil, fmt.Errorf("promotion: query records: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []Record{}
	for rows.Next() {
		var r Record
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.SourceRunID, &r.FromEnvironment, &r.TargetEnvironment,
			&r.Status, &r.ApprovalStage, &r.PromotedBy, &r.CreatedAt, &r.DecidedAt); err != nil {
			return nil, fmt.Errorf("promotion: scan record: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// isForeignKeyErr 判断错误是否为外键约束失败(modernc sqlite 文本含 FOREIGN KEY)。
func isForeignKeyErr(err error) bool { return store.IsForeignKeyErr(err) }
