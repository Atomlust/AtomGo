// Package trigger 是「项目触发与规则(分支映射)」的领域层(FR-1 / FR-2 / Story 2.3)。
//
// 每个项目持有一份触发配置:webhook 接收端点 token、签名密钥、触发事件开关、
// 分支(通配)→ 环境 + 目标服务器映射、未匹配策略。webhook 签名密钥经 vault
// secretbox(master key)加密后仅以**密文 BLOB** 入库;DB dump 绝无明文(AC1 / AC-SEC)。
// 对外(REST)常态只暴露掩码;明文仅在 ResetSecret 重置时一次性返回(供粘贴到 Gitee)。
//
// master key 缺失时,涉及密钥的读取/重置降级为明确错误(ErrVaultUnconfigured),
// 平台不 panic。
//
// 本期为「最小前向兼容」:branchMappings 的 targetServerIds 列建模占位、可空;
// 服务器存在性校验留 TODO,目标服务器表(Story 4-1)落地后启用。webhook **接收**
// 端点(验签 + 创建运行)= Story 3-2,本期只做配置。
package trigger

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/atomlust/atomgo/internal/pipeline"
	"github.com/atomlust/atomgo/internal/store"
	"github.com/atomlust/atomgo/internal/vault"
	"github.com/google/uuid"
)

// 未匹配策略枚举(DB 存 snake/小写字串;JSON camelCase 字段)。
const (
	// PolicyRecord 表示未匹配任何映射的分支「记录但不部署」。
	PolicyRecord = "record"
	// PolicyIgnore 表示未匹配任何映射的分支「忽略」。
	PolicyIgnore = "ignore"
)

// webhook 签名密钥前缀(供用户在 Gitee 端识别;掩码也保留此前缀)。
const secretPrefix = "whsec_"

// 领域错误。错误体永不含密钥明文/密文/master key。
var (
	// ErrNotFound 表示触发配置不存在(通常被惰性默认生成兜住,极少触达)。
	ErrNotFound = errors.New("trigger: not found")
	// ErrProjectNotFound 表示引用的项目不存在。
	ErrProjectNotFound = errors.New("trigger: project not found")
	// ErrVaultUnconfigured 表示保险库未配置 master key,无法生成/读取签名密钥。
	ErrVaultUnconfigured = errors.New("trigger: vault unconfigured")
	// ErrInvalidBranchPattern 表示分支模式非空校验或通配语法校验失败。
	ErrInvalidBranchPattern = errors.New("trigger: invalid branch pattern")
	// ErrDuplicateBranchPattern 表示多条映射使用了同一分支模式(trim 后精确匹配)。
	// matchBranch 首条命中生效,重复模式会让后续条永久静默失效,故在入库前拦截。
	ErrDuplicateBranchPattern = errors.New("trigger: duplicate branch pattern")
	// ErrInvalidPolicy 表示未匹配策略枚举非法。
	ErrInvalidPolicy = errors.New("trigger: invalid unmatched policy")
	// ErrInvalidPathFilter 表示路径过滤 glob 非空校验失败(去空白后为空)。
	ErrInvalidPathFilter = errors.New("trigger: invalid path filter")
	// ErrEnvironmentNotFound 表示分支映射引用的 EnvironmentID 指向一个不存在的环境
	// (根治#:保存时即拦截悬空引用,避免运行时静默错配)。
	ErrEnvironmentNotFound = errors.New("trigger: referenced environment not found")
)

// Events 是触发事件开关(手动触发常开,不建模为字段)。
//
// Release 为 Story 8-11 新增(FR-8-11):向后兼容——旧 events_json 缺该字段时
// JSON 反序列化为零值 false,无需迁移(events 以 JSON 整体入库)。
type Events struct {
	Push        bool `json:"push"`
	Tag         bool `json:"tag"`
	PullRequest bool `json:"pullRequest"`
	Release     bool `json:"release"`
}

// BranchMapping 是一条分支映射:分支(通配)模式 → 环境 + 目标服务器引用 id 列表。
//
// 根治(#):环境改用**环境 ID(EnvironmentID)** 引用,而非脆弱的文本名——环境改名也不会
// 再静默错配(曾导致「配了 registry 却不推送 / 部署不自动登录」)。Environment 字段保留为
// 环境名的冗余快照 / 降级来源(存量数据无 EnvironmentID 时按名定位)。
//
// TargetServerIDs 自根治起**不再作为生效来源**:部署目标服务器统一来自被引用环境的
// Environment.TargetServerIDs(唯一真相源)。本字段仅在「环境找不到」的极端兜底时才被使用,
// 正常保存路径下前端不再维护它(选择环境时从环境继承)。
type BranchMapping struct {
	ID              string   `json:"id"`
	BranchPattern   string   `json:"branchPattern"`
	Environment     string   `json:"environment"`
	EnvironmentID   string   `json:"environmentId,omitempty"`
	TargetServerIDs []string `json:"targetServerIds"`
}

// Config 是触发配置领域模型。WebhookSecretMasked 为掩码;明文绝不在此模型常驻。
//
// PathFilters 为路径过滤 glob 列表(monorepo · P0):非空时仅当本次 push 改动文件匹配任一 glob 才触发;
// 空列表 = 不启用(放行一切,向后兼容)。glob 支持 `*`(单段)与 `**`(跨 `/`)。
type Config struct {
	ProjectID           string
	WebhookToken        string
	WebhookSecretMasked string
	Events              Events
	BranchMappings      []BranchMapping
	UnmatchedPolicy     string
	PathFilters         []string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// SaveInput 是保存配置的入参(不含 webhook 密钥;密钥只经 ResetSecret 轮换)。
type SaveInput struct {
	Events          Events
	BranchMappings  []BranchMapping
	UnmatchedPolicy string
	PathFilters     []string
}

// ResetResult 是重置密钥的结果:完整明文仅此一次返回,同时给掩码。
type ResetResult struct {
	Secret string // 完整明文,仅重置时回显一次
	Masked string
}

// RevealResult 是查看密钥的结果:完整明文 + 掩码。与 ResetResult 的关键区别是
// **不轮换密钥**——旧值继续有效,仅把当前生效值解密回显(供粘贴到代码平台的 Webhook 配置)。
type RevealResult struct {
	Secret string // 当前生效的完整明文
	Masked string
}

// Service 定义触发配置领域对外接口。
type Service interface {
	// Get 返回项目触发配置(密钥仅掩码)。首次访问无配置时惰性生成默认
	// (随机 token + 生成并加密的签名密钥 + events 全 false + 空映射 + policy=record)。
	Get(ctx context.Context, projectID string) (*Config, error)
	// Save 校验并持久化 events/分支映射/未匹配策略(不触碰密钥);返回更新后配置(掩码)。
	Save(ctx context.Context, projectID string, in SaveInput) (*Config, error)
	// ResetSecret 重新生成并加密签名密钥(旧值失效),返回完整明文(仅此一次)+ 掩码。
	ResetSecret(ctx context.Context, projectID string) (*ResetResult, error)
	// RevealSecret 返回当前生效密钥的完整明文与掩码。**不轮换**:旧值继续有效,
	// 仅解密回显(供粘贴到代码平台的 Webhook 配置)。master key 未配置 → ErrVaultUnconfigured。
	RevealSecret(ctx context.Context, projectID string) (*RevealResult, error)
}

// service 是 store + vault 支撑的 Service 实现。
type service struct {
	db       *sql.DB
	vault    vault.Vault
	settings pipeline.SettingsService
}

// Option 是 Service 的可选装配项(不破坏 New(db, v) 既有签名)。
type Option func(*service)

// WithSettings 注入构建/部署配置服务,用于保存时校验分支映射的 EnvironmentID 指向有效环境
// (根治#:拦截悬空引用)。未注入(settings=nil)时跳过该校验(向后兼容 / 测试)。
func WithSettings(svc pipeline.SettingsService) Option {
	return func(s *service) { s.settings = svc }
}

// New 构造 Service。
//   - db:经参数化 SQL 触库。
//   - v:凭据保险库,复用其 secretbox(master key)加解密 webhook 签名密钥;
//     v 为 nil 或未配置 master key 时,涉及密钥的读取/重置返回 ErrVaultUnconfigured。
//
// 不在此做任何重活(无 init() 副作用,避免抬高空载内存)。
func New(db *sql.DB, v vault.Vault, opts ...Option) Service {
	s := &service{db: db, vault: v}
	for _, o := range opts {
		o(s)
	}
	return s
}

func (s *service) Get(ctx context.Context, projectID string) (*Config, error) {
	cfg, masked, err := s.load(ctx, projectID)
	if err == nil {
		cfg.WebhookSecretMasked = masked
		return cfg, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	// 首次访问无配置:惰性生成默认(含 token + 加密签名密钥)。
	return s.createDefault(ctx, projectID)
}

func (s *service) Save(ctx context.Context, projectID string, in SaveInput) (*Config, error) {
	// 确保配置已存在(惰性默认);Get 也完成项目存在性校验。
	if _, err := s.Get(ctx, projectID); err != nil {
		return nil, err
	}

	// 根治#:若注入了 settings,校验分支映射引用的 EnvironmentID 指向有效环境。
	// 存量数据无 EnvironmentID(空)→ 跳过(降级用名)。settings 未注入 → 跳过(向后兼容)。
	if s.settings != nil {
		if err := s.validateMappingEnvironments(ctx, projectID, in.BranchMappings); err != nil {
			return nil, err
		}
	}

	policy, err := normalizePolicy(in.UnmatchedPolicy)
	if err != nil {
		return nil, err
	}
	mappings, err := normalizeMappings(in.BranchMappings)
	if err != nil {
		return nil, err
	}
	pathFilters, err := normalizePathFilters(in.PathFilters)
	if err != nil {
		return nil, err
	}

	eventsJSON, err := json.Marshal(in.Events)
	if err != nil {
		return nil, fmt.Errorf("trigger: marshal events: %w", err)
	}
	mappingsJSON, err := json.Marshal(mappings)
	if err != nil {
		return nil, fmt.Errorf("trigger: marshal branch mappings: %w", err)
	}
	pathFiltersJSON, err := json.Marshal(pathFilters)
	if err != nil {
		return nil, fmt.Errorf("trigger: marshal path filters: %w", err)
	}

	nowStr := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`UPDATE pipeline_triggers
		 SET events_json = ?, branch_mappings_json = ?, unmatched_policy = ?, path_filters_json = ?, updated_at = ?
		 WHERE project_id = ?`,
		string(eventsJSON), string(mappingsJSON), policy, string(pathFiltersJSON), nowStr, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("trigger: update config: %w", err)
	}
	return s.Get(ctx, projectID)
}

func (s *service) ResetSecret(ctx context.Context, projectID string) (*ResetResult, error) {
	// 确保配置已存在(惰性默认);也完成项目存在性校验 + vault 可用性校验。
	if _, err := s.Get(ctx, projectID); err != nil {
		return nil, err
	}

	secret, sealed, masked, err := s.newSecret()
	if err != nil {
		return nil, err
	}

	nowStr := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE pipeline_triggers SET webhook_secret_ciphertext = ?, updated_at = ? WHERE project_id = ?`,
		sealed, nowStr, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("trigger: rotate secret: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return &ResetResult{Secret: secret, Masked: masked}, nil
}

// RevealSecret 返回当前生效密钥的完整明文(不轮换,旧值继续有效)。
// 先经 Get 完成项目存在性与 vault 可用性校验,再读密文解密回显。
// master key 未配置/解密失败 → ErrVaultUnconfigured;无配置行 → ErrNotFound。
func (s *service) RevealSecret(ctx context.Context, projectID string) (*RevealResult, error) {
	if _, err := s.Get(ctx, projectID); err != nil {
		return nil, err
	}
	var sealed []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT webhook_secret_ciphertext FROM pipeline_triggers WHERE project_id = ?`, projectID,
	).Scan(&sealed)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("trigger: load secret ciphertext: %w", err)
	}
	if s.vault == nil {
		return nil, ErrVaultUnconfigured
	}
	plain, err := s.vault.OpenSecret(sealed)
	if err != nil {
		if errors.Is(err, vault.ErrVaultUnconfigured) {
			return nil, ErrVaultUnconfigured
		}
		return nil, fmt.Errorf("trigger: open secret: %w", err)
	}
	// 明文用完即弃;Secret/Masked 均为 []byte → string 的拷贝,不受清零影响。
	defer zero(plain)
	return &RevealResult{Secret: string(plain), Masked: maskSecret(string(plain))}, nil
}

// load 读取已存在的配置行;同时回算掩码。无行 → ErrNotFound。
// 永不返回密钥明文;掩码由密文解密后即时计算并即弃明文。
func (s *service) load(ctx context.Context, projectID string) (*Config, string, error) {
	var (
		cfg             Config
		sealed          []byte
		eventsJSON      string
		mappingsJSON    string
		pathFiltersJSON string
		createdStr      string
		updatedStr      string
	)
	cfg.ProjectID = projectID
	err := s.db.QueryRowContext(ctx,
		`SELECT webhook_token, webhook_secret_ciphertext, events_json, branch_mappings_json,
		        unmatched_policy, path_filters_json, created_at, updated_at
		 FROM pipeline_triggers WHERE project_id = ?`, projectID,
	).Scan(&cfg.WebhookToken, &sealed, &eventsJSON, &mappingsJSON,
		&cfg.UnmatchedPolicy, &pathFiltersJSON, &createdStr, &updatedStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", ErrNotFound
		}
		return nil, "", fmt.Errorf("trigger: load config: %w", err)
	}

	if err := json.Unmarshal([]byte(eventsJSON), &cfg.Events); err != nil {
		return nil, "", fmt.Errorf("trigger: parse events: %w", err)
	}
	cfg.BranchMappings = []BranchMapping{}
	if strings.TrimSpace(mappingsJSON) != "" {
		if err := json.Unmarshal([]byte(mappingsJSON), &cfg.BranchMappings); err != nil {
			return nil, "", fmt.Errorf("trigger: parse branch mappings: %w", err)
		}
		if cfg.BranchMappings == nil {
			cfg.BranchMappings = []BranchMapping{}
		}
	}
	cfg.PathFilters = []string{}
	if strings.TrimSpace(pathFiltersJSON) != "" {
		if err := json.Unmarshal([]byte(pathFiltersJSON), &cfg.PathFilters); err != nil {
			return nil, "", fmt.Errorf("trigger: parse path filters: %w", err)
		}
		if cfg.PathFilters == nil {
			cfg.PathFilters = []string{}
		}
	}

	created, err := time.Parse(time.RFC3339, createdStr)
	if err != nil {
		return nil, "", fmt.Errorf("trigger: parse created_at: %w", err)
	}
	updated, err := time.Parse(time.RFC3339, updatedStr)
	if err != nil {
		return nil, "", fmt.Errorf("trigger: parse updated_at: %w", err)
	}
	cfg.CreatedAt = created
	cfg.UpdatedAt = updated

	masked, err := s.maskSealed(sealed)
	if err != nil {
		return nil, "", err
	}
	return &cfg, masked, nil
}

// createDefault 惰性生成项目默认触发配置:随机 token + 生成并加密的签名密钥 +
// events 全 false + 空映射 + policy=record。项目不存在 → ErrProjectNotFound。
func (s *service) createDefault(ctx context.Context, projectID string) (*Config, error) {
	if err := s.ensureProjectExists(ctx, projectID); err != nil {
		return nil, err
	}

	token, err := randToken()
	if err != nil {
		return nil, err
	}
	_, sealed, masked, err := s.newSecret()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)
	// 并发首访同项目时,第二个 INSERT 会撞 project_id 主键 → ON CONFLICT DO NOTHING
	// 避免 500;无论本次是否真正插入,随后统一 load 当前权威行(防双方各持不同 token/密钥)。
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO pipeline_triggers
		   (project_id, webhook_token, webhook_secret_ciphertext, events_json, branch_mappings_json, unmatched_policy, created_at, updated_at)
		 VALUES (?, ?, ?, '{"push":false,"tag":false,"pullRequest":false,"release":false}', '[]', ?, ?, ?) `+
			store.DoNothingSuffix(store.DialectOf(s.db), []string{"project_id"}),
		projectID, token, sealed, PolicyRecord, nowStr, nowStr,
	)
	if err != nil {
		if isForeignKeyErr(err) {
			return nil, ErrProjectNotFound
		}
		return nil, fmt.Errorf("trigger: insert default config: %w", err)
	}

	// 不管本协程是否赢得插入,都回读权威行,保证返回的 token/掩码与库内一致
	// (并发竞态下另一协程可能已写入不同的随机 token)。
	if n, _ := res.RowsAffected(); n == 0 {
		cfg, masked, lerr := s.load(ctx, projectID)
		if lerr != nil {
			return nil, lerr
		}
		cfg.WebhookSecretMasked = masked
		return cfg, nil
	}

	return &Config{
		ProjectID:           projectID,
		WebhookToken:        token,
		WebhookSecretMasked: masked,
		Events:              Events{},
		BranchMappings:      []BranchMapping{},
		UnmatchedPolicy:     PolicyRecord,
		PathFilters:         []string{},
		CreatedAt:           now,
		UpdatedAt:           now,
	}, nil
}

// ensureProjectExists 校验项目存在(触发配置依附项目;惰性建默认前先确认)。
func (s *service) ensureProjectExists(ctx context.Context, projectID string) error {
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE id = ?`, projectID).Scan(&one)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrProjectNotFound
		}
		return fmt.Errorf("trigger: check project: %w", err)
	}
	return nil
}

// newSecret 生成新的 webhook 签名密钥:返回明文、密文(secretbox 加密)、掩码。
// vault 未配置 → ErrVaultUnconfigured(不 panic)。
func (s *service) newSecret() (secret string, sealed []byte, masked string, err error) {
	if s.vault == nil {
		return "", nil, "", ErrVaultUnconfigured
	}
	body, err := randHex(24) // 48 hex 字符的密钥体
	if err != nil {
		return "", nil, "", err
	}
	secret = secretPrefix + body
	sealed, err = s.vault.SealSecret([]byte(secret))
	if err != nil {
		if errors.Is(err, vault.ErrVaultUnconfigured) {
			return "", nil, "", ErrVaultUnconfigured
		}
		return "", nil, "", fmt.Errorf("trigger: seal secret: %w", err)
	}
	return secret, sealed, maskSecret(secret), nil
}

// maskSealed 解密密文并算掩码;明文用完即弃。vault 未配置 → ErrVaultUnconfigured。
func (s *service) maskSealed(sealed []byte) (string, error) {
	if s.vault == nil {
		return "", ErrVaultUnconfigured
	}
	plain, err := s.vault.OpenSecret(sealed)
	if err != nil {
		if errors.Is(err, vault.ErrVaultUnconfigured) {
			return "", ErrVaultUnconfigured
		}
		// 解密失败(错误 master key / 篡改):不泄漏细节,按未配置态处理。
		return "", ErrVaultUnconfigured
	}
	masked := maskSecret(string(plain))
	for i := range plain {
		plain[i] = 0 // 明文用完清零
	}
	return masked, nil
}

// maskSecret 生成签名密钥掩码:保留 whsec_ 前缀 + 末 4 位,中间点掩码。
// 如 "whsec_••••a91f"。掩码不足以还原密钥。
func maskSecret(secret string) string {
	const dots = "••••"
	body := strings.TrimPrefix(secret, secretPrefix)
	tail := body
	if len(body) > 4 {
		tail = body[len(body)-4:]
	}
	return secretPrefix + dots + tail
}

// randToken 生成 webhook 接收端点路径 token(URL 安全的 hex,非密钥)。
func randToken() (string, error) {
	return randHex(16) // 32 hex 字符
}

// randHex 返回 n 字节随机数据的 hex 编码(2n 字符)。
func randHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("trigger: read random: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// normalizePolicy 校验并归一未匹配策略;空串默认 record。
func normalizePolicy(p string) (string, error) {
	switch p {
	case "":
		return PolicyRecord, nil
	case PolicyRecord, PolicyIgnore:
		return p, nil
	default:
		return "", ErrInvalidPolicy
	}
}

// normalizeMappings 校验每条分支映射并补全 id；保证 targetServerIds 非 nil(JSON [])。
//
// branchPattern 非空 + 合法通配(见 validateBranchPattern);trim 后不可重复
// (ErrDuplicateBranchPattern — 首条命中语义下重复模式等于静默废弃后续条)。
// TODO(Story 4-1):接入 target_servers 表后,对 TargetServerIDs 做存在性校验
// (调用 validateTargetServerIDs);本期不拦(目标服务器表尚不存在)。
func normalizeMappings(in []BranchMapping) ([]BranchMapping, error) {
	out := make([]BranchMapping, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, m := range in {
		pattern := strings.TrimSpace(m.BranchPattern)
		if err := validateBranchPattern(pattern); err != nil {
			return nil, err
		}
		if _, dup := seen[pattern]; dup {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateBranchPattern, pattern)
		}
		seen[pattern] = struct{}{}
		id := strings.TrimSpace(m.ID)
		if id == "" {
			id = uuid.NewString()
		}
		ids := m.TargetServerIDs
		if ids == nil {
			ids = []string{}
		}
		// 前向兼容占位:本期不校验存在性(留 4-1)。
		if err := validateTargetServerIDs(ids); err != nil {
			return nil, err
		}
		out = append(out, BranchMapping{
			ID:              id,
			BranchPattern:   pattern,
			Environment:     strings.TrimSpace(m.Environment),
			EnvironmentID:   strings.TrimSpace(m.EnvironmentID),
			TargetServerIDs: ids,
		})
	}
	return out, nil
}

// validateMappingEnvironments 校验每条分支映射的 EnvironmentID(若存在)指向项目内真实存在的
// 环境(根治#:保存期即拦截悬空引用,避免运行时静默错配)。EnvironmentID 空(存量数据 / 旧前端)
// → 跳过(降级用名)。环境配置行不存在(sql.ErrNoRows,即项目尚未定义任何环境)→ 任何非空
// EnvironmentID 都视为无效,同样拦截。
func (s *service) validateMappingEnvironments(ctx context.Context, projectID string, mappings []BranchMapping) error {
	st, err := pipeline.LoadExistingSettings(ctx, s.db, projectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// 无构建/部署配置行:项目尚未定义任何环境 → 任何非空 EnvironmentID 都无效。
			for _, m := range mappings {
				if strings.TrimSpace(m.EnvironmentID) != "" {
					return fmt.Errorf("%w: %s", ErrEnvironmentNotFound, strings.TrimSpace(m.EnvironmentID))
				}
			}
			return nil
		}
		// 其它读取错误:不阻断保存(诚实降级,不把配置保存变成 500)。
		return nil
	}
	ids := make(map[string]struct{}, len(st.Environments))
	for _, e := range st.Environments {
		ids[strings.TrimSpace(e.ID)] = struct{}{}
	}
	for _, m := range mappings {
		id := strings.TrimSpace(m.EnvironmentID)
		if id == "" {
			continue
		}
		if _, ok := ids[id]; !ok {
			return fmt.Errorf("%w: %s", ErrEnvironmentNotFound, id)
		}
	}
	return nil
}

// normalizePathFilters 校验并规范化路径过滤 glob 列表(monorepo · P0):
//   - 每条 trim、剔空、去重(保留首次出现序)。
//   - 含空白的非法 glob → ErrInvalidPathFilter(与分支模式一致:glob 不应含空白)。
//
// 返回非 nil 切片(空列表 = 不启用路径过滤,放行一切)。
func normalizePathFilters(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, raw := range in {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue // 空行直接剔除(前端多行文本框常见尾随空行)
		}
		if strings.ContainsAny(p, " \t\n\r") {
			return nil, fmt.Errorf("%w: path filter %q must not contain whitespace", ErrInvalidPathFilter, p)
		}
		if _, dup := seen[p]; dup {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out, nil
}

// validateBranchPattern 校验分支(通配)模式:非空;仅允许 glob 通配 `*`(及普通字符)。
// 合法:main / dev / release/* / feature/*-hotfix。非法:空串。
// 校验通配语法:`*` 是允许的唯一通配元字符,任意位置任意个;不接受单独空白模式。
func validateBranchPattern(pattern string) error {
	if pattern == "" {
		return ErrInvalidBranchPattern
	}
	if strings.ContainsAny(pattern, " \t\n\r") {
		return ErrInvalidBranchPattern
	}
	// 仅含 `*` 也无意义(匹配一切但无定位);其余含至少一个非 `*` 字符即合法。
	if strings.Trim(pattern, "*") == "" {
		return ErrInvalidBranchPattern
	}
	return nil
}

// validateTargetServerIDs 是目标服务器存在性校验的占位函数。
//
// TODO(Story 4-1):target_servers 表落地后,在此对每个 id 做存在性校验
// (SELECT 1 FROM target_servers WHERE id = ?),不存在则返回错误并联动 FR-9。
// 本期目标服务器表尚不存在,故仅做基础结构校验(非空字符串),不拦截不存在性。
func validateTargetServerIDs(ids []string) error {
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			return fmt.Errorf("%w: target server id must not be empty", ErrInvalidBranchPattern)
		}
	}
	return nil
}

// isForeignKeyErr 判断错误是否为外键约束失败(modernc sqlite 文本含 FOREIGN KEY)。
func isForeignKeyErr(err error) bool { return store.IsForeignKeyErr(err) }
