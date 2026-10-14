// Package settings 提供系统级设置的持久化(DB 键值表)与环境变量覆盖读取。
//
// 存在的理由:首次启动 Web 初始化向导(零 env 部署)需要把「对外 URL」等系统设置
// 落到 DB;而这些设置又保留 env 覆盖能力(Docker/headless 场景 env 优先)。
// 仅存放非敏感设置;master key 等机密绝不入此表(见 internal/config + vault)。
package settings

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"time"

	"github.com/atomlust/atomgo/internal/store"
)

// 已知设置键。
const (
	// KeyPublicURL 是对外可访问 URL(webhook 回调 / OAuth 回跳 / 审批链接拼装用)。
	KeyPublicURL = "public_url"
)

// EnvPublicURL 是 KeyPublicURL 的 env 覆盖变量名。
const EnvPublicURL = "ATOMGO_PUBLIC_URL"

// Service 是设置服务:DB 持久化 + env 优先覆盖。
type Service struct {
	db *sql.DB
}

// New 构造设置服务。
func New(db *sql.DB) *Service { return &Service{db: db} }

// Get 返回设置值:env 覆盖优先 → DB 值 → 空串。
// key 为已知键时先查对应 env(见各键文档);其余键直接读 DB。
func (s *Service) Get(ctx context.Context, key string) (string, error) {
	if key == KeyPublicURL {
		if v := strings.TrimSpace(os.Getenv(EnvPublicURL)); v != "" {
			return v, nil
		}
	}
	var v string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM system_settings WHERE key = ?`, key).Scan(&v)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return v, nil
}

// Set 写入设置(upsert;跨方言经 store.UpsertSuffix)。
func (s *Service) Set(ctx context.Context, key, value string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO system_settings (key, value, updated_at) VALUES (?, ?, ?) `+
			store.UpsertSuffix(store.DialectOf(s.db), []string{"key"}, []string{"value", "updated_at"}),
		key, value, now)
	return err
}

// PublicURL 返回对外 URL:env 优先 → DB → 空串。空串表示未配置(相关功能优雅降级)。
func (s *Service) PublicURL(ctx context.Context) string {
	v, err := s.Get(ctx, KeyPublicURL)
	if err != nil {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(v), "/")
}

// All 返回 DB 中的全部设置(不含 env 覆盖值;供设置页展示)。
func (s *Service) All(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM system_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}
