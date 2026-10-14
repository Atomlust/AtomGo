package httpapi

// setup.go — 首次启动 Web 初始化向导(setup wizard)。
//
// 零 env 全新部署:进程起 → 检测无管理员 → 进入 setup 模式(除 /api/setup/*、
// /healthz、/version 与 SPA 静态资源外一律 404)→ 浏览器打开即进向导 →
// 提交后建管理员 + 注入 master key + 落系统设置 → 进登录页。
//
// 安全边界:
//   - setup 端点仅在「无管理员」期间开放;管理员一旦存在即 410 Gone。
//   - POST 需 GET /api/setup/status 下发的一次性 token(进程级随机 secret 的 HMAC),
//     防 CSRF / 重放;进程重启后 token 失效(重新获取)。
//   - 密码/master key 绝不入日志;自动生成的 master key 仅在提交响应体回显一次。
//   - 数据库选择属向导范围:默认 SQLite(免配置);选 MySQL/PostgreSQL 时由向导收集连接
//     信息,校验通过后写入 atomgo.env 并触发进程自重启以应用,无需用户手动改配置。

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/atomlust/atomgo/internal/settings"
	"github.com/atomlust/atomgo/internal/store"
	"github.com/atomlust/atomgo/internal/vault"
	"github.com/go-chi/chi/v5"
)

// setupMinPasswordLen / setupMaxPasswordLen 是向导口令长度边界。
const (
	setupMinPasswordLen = 8
	setupMaxPasswordLen = 72
)

// setupMasterKeyFileMode 是落盘 master key 的文件权限(仅属主可读写)。
const setupMasterKeyFileMode = 0o600

// SetupService 是首次启动初始化向导领域服务(由 NewSetupServiceForWiring 构造)。
type SetupService interface {
	// SetupRequired 报告当前是否处于 setup 模式(无管理员)。
	SetupRequired(ctx context.Context) bool
	// Status 返回向导状态;setupRequired 期间附带一次性 token。
	Status(ctx context.Context) (*SetupStatus, error)
	// Submit 提交向导:建管理员 + 注入 master key + 落系统设置。
	Submit(ctx context.Context, in SetupInput) (*SetupResult, error)
}

// SetupStatus 是向导状态 DTO。
type SetupStatus struct {
	SetupRequired   bool          `json:"setupRequired"`
	VaultConfigured bool          `json:"vaultConfigured"`
	Defaults        SetupDefaults `json:"defaults"`
	Token           string        `json:"token"` // 仅 setupRequired 时非空
	// DBConfigured 表示数据库已由 atomgo.env 固化(首次选择 MySQL/PostgreSQL 并保存后为真)。
	// 为真时向导只展示管理员步骤,数据库区块锁定。
	DBConfigured bool `json:"dbConfigured"`
	// DBDriver 是当前生效的数据库驱动(sqlite/mysql/postgres),仅用于信息展示。
	DBDriver string `json:"dbDriver"`
}

// SetupDefaults 是向导表单预填默认值。
type SetupDefaults struct {
	Username  string `json:"username"`
	PublicURL string `json:"publicUrl"`
}

// SetupInput 是向导提交 DTO。
type SetupInput struct {
	Username  string
	Password  string
	MasterKey string // "generate"(默认/空)或 base64 编码的 32 字节
	PublicURL string
	// 数据库选择(仅当 DBConfigured=false 时受理;选 mysql/postgres 会固化并触发重启)。
	DBDriver   string // "sqlite"(默认) | "mysql" | "postgres"
	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBDSN      string // 可选:直接提供完整 DSN,优先于上面各字段
}

// SetupResult 是向导提交结果。MasterKey 仅在「自动生成」时非空(一次性回显)。
// RestartRequired 为 true 表示数据库配置已固化、进程正在自重启以应用,前端应轮询
// /api/setup/status 直至 dbConfigured=true 后进入管理员步骤。
type SetupResult struct {
	OK              bool   `json:"ok"`
	MasterKey       string `json:"masterKey,omitempty"`
	RestartRequired bool   `json:"restartRequired,omitempty"`
}

// SetupAdminBootstrapper 抽象 setup 消费的认证能力(由 *auth.Service 实现)。
type SetupAdminBootstrapper interface {
	Bootstrap(username, password string) error
	HasAdmin() (bool, error)
}

// setupService 实现 SetupService。
type setupService struct {
	auth       SetupAdminBootstrapper
	vlt        vault.Vault
	settings   *settings.Service
	masterKeyF string // master key 落盘路径("" = 不落盘,仅进程内)
	envFile    string // atomgo.env 路径(固化数据库配置用;"" 表示不持久化)
	selfRestart func() // 数据库固化后触发进程自重启(如为 nil 则不重启,仅返回 RestartRequired)
	secret     []byte // 进程级随机 secret(token = HMAC(secret, "setup"))
}

// NewSetupServiceForWiring 构造向导服务(供 main 装配层调用)。
// masterKeyFilePath 为空表示不落盘(此时重启后需重新经 env 提供 key;
// SQLite 部署由 main 传 <db 目录>/master.key)。
// envFilePath 为 atomgo.env 路径("" 表示向导不持久化数据库选择);selfRestart 为
// 数据库固化后的自重启回调(nil 表示不重启,仅返回 RestartRequired)。
func NewSetupServiceForWiring(auth SetupAdminBootstrapper, vlt vault.Vault, st *settings.Service, masterKeyFilePath, envFilePath string, selfRestart func()) (SetupService, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	return &setupService{auth: auth, vlt: vlt, settings: st, masterKeyF: masterKeyFilePath, envFile: envFilePath, selfRestart: selfRestart, secret: secret}, nil
}

// SetupRequired 报告是否处于 setup 模式(无管理员)。
func (s *setupService) SetupRequired(ctx context.Context) bool {
	if s.auth == nil {
		return false
	}
	has, err := s.auth.HasAdmin()
	if err != nil {
		// 查询失败时不进入 setup 模式(避免把故障放大成全站 404)。
		return false
	}
	return !has
}

func (s *setupService) Status(ctx context.Context) (*SetupStatus, error) {
	required := s.SetupRequired(ctx)
	st := &SetupStatus{
		SetupRequired:   required,
		VaultConfigured: s.vlt != nil && s.vlt.Configured(),
		Defaults:        SetupDefaults{Username: "admin"},
		DBConfigured:    s.dbConfigured(),
		DBDriver:        currentDBDriver(),
	}
	if required {
		st.Token = s.token()
		if s.settings != nil {
			if v, err := s.settings.Get(ctx, settings.KeyPublicURL); err == nil {
				st.Defaults.PublicURL = v
			}
		}
	}
	return st, nil
}

// currentDBDriver 返回当前生效的数据库驱动(环境变量或默认 sqlite)。
func currentDBDriver() string {
	if d := strings.TrimSpace(os.Getenv("ATOMGO_DB_DRIVER")); d != "" {
		return d
	}
	return "sqlite"
}

// dbConfigured 报告数据库配置是否已由 atomgo.env 固化(选过 MySQL/PostgreSQL 并保存)。
func (s *setupService) dbConfigured() bool {
	return envHasKey(s.envFile, "ATOMGO_DB_DRIVER")
}

func (s *setupService) Submit(ctx context.Context, in SetupInput) (*SetupResult, error) {
	if !s.SetupRequired(ctx) {
		return nil, errSetupAlreadyDone
	}

	// 数据库选择:仅在尚未固化(dbConfigured=false)时受理。选 MySQL/PostgreSQL 会校验
	// 连接、写入 atomgo.env 并触发自重启;SQLite(默认)无需任何配置,直接进入管理员步骤。
	if !s.dbConfigured() {
		switch strings.TrimSpace(in.DBDriver) {
		case "mysql", "postgres":
			dsn, berr := buildDSN(strings.TrimSpace(in.DBDriver), in.DBHost, in.DBPort, in.DBUser, in.DBPassword, in.DBName, in.DBDSN)
			if berr != nil {
				return nil, setupInvalid(berr.Error())
			}
			// 校验连接并应用对应方言迁移(空库需用户预先建好);失败明确报错,不固化、不重启。
			testStore, terr := store.OpenWithConfig(store.OpenConfig{Driver: strings.TrimSpace(in.DBDriver), DSN: dsn})
			if terr != nil {
				return nil, setupInvalid("数据库连接失败:" + terr.Error())
			}
			if cerr := testStore.Close(); cerr != nil {
				log.Printf("[setup] 警告:测试数据库连接关闭失败:%v", cerr)
			}
			if err := persistEnvKV(s.envFile, "ATOMGO_DB_DRIVER", strings.TrimSpace(in.DBDriver)); err != nil {
				return nil, fmt.Errorf("setup: 固化数据库驱动失败:%w", err)
			}
			if err := persistEnvKV(s.envFile, "ATOMGO_DB_DSN", dsn); err != nil {
				return nil, fmt.Errorf("setup: 固化数据库连接串失败:%w", err)
			}
			res := &SetupResult{OK: true, RestartRequired: true}
			if s.selfRestart != nil {
				// 先让响应送达客户端,再自重启以加载新的数据库配置。
				time.AfterFunc(800*time.Millisecond, s.selfRestart)
			}
			return res, nil
		case "", "sqlite":
			// 默认 SQLite:不改动数据库配置,继续下面的管理员初始化。
		default:
			return nil, setupInvalid("不支持的数据库驱动:" + strings.TrimSpace(in.DBDriver))
		}
	}

	username := strings.TrimSpace(in.Username)
	if username == "" {
		username = "admin"
	}
	if len(username) > 64 {
		return nil, setupInvalid("username too long")
	}
	password := in.Password // 口令不去空格(前后空格可能是用户有意为之?不,统一 trim 防误操作)
	if len(password) < setupMinPasswordLen || len(password) > setupMaxPasswordLen {
		return nil, setupInvalid("password length must be 8..72")
	}
	publicURL := strings.TrimRight(strings.TrimSpace(in.PublicURL), "/")
	if publicURL != "" && !strings.HasPrefix(publicURL, "http://") && !strings.HasPrefix(publicURL, "https://") {
		return nil, setupInvalid("publicUrl must start with http:// or https://")
	}

	// master key:保险库已配(env 注入)则跳过;否则生成或解析用户提供值。
	var generatedKey string
	if s.vlt != nil && !s.vlt.Configured() {
		var key *[32]byte
		switch strings.TrimSpace(in.MasterKey) {
		case "", "generate":
			k, err := generateMasterKey()
			if err != nil {
				return nil, err
			}
			key = k
			generatedKey = base64.StdEncoding.EncodeToString(key[:])
		default:
			raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(in.MasterKey))
			if err != nil || len(raw) != 32 {
				return nil, setupInvalid("masterKey must be base64-encoded 32 bytes")
			}
			var k [32]byte
			copy(k[:], raw)
			key = &k
		}
		if err := s.vlt.ConfigureKey(key); err != nil {
			if errors.Is(err, vault.ErrKeyAlreadyConfigured) {
				return nil, errSetupAlreadyDone
			}
			return nil, err
		}
		// 落盘(重启后可经文件重新加载;失败不致命——进程内已可用,告警提示手动保存)。
		if s.masterKeyF != "" {
			if err := persistMasterKey(s.masterKeyF, key); err != nil {
				log.Printf("[setup] 警告:master key 落盘 %s 失败:%v(请手动保存返回的 key)", s.masterKeyF, err)
			}
		}
	}

	if err := s.auth.Bootstrap(username, password); err != nil {
		return nil, err
	}
	if publicURL != "" && s.settings != nil {
		if err := s.settings.Set(ctx, settings.KeyPublicURL, publicURL); err != nil {
			return nil, err
		}
	}
	return &SetupResult{OK: true, MasterKey: generatedKey}, nil
}

// token 返回当前 setup token(进程 secret 的 HMAC;setup 期间恒定)。
func (s *setupService) token() string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte("atomgo-setup-v1"))
	return hex.EncodeToString(mac.Sum(nil))
}

// verifyToken 常量时间比较提交 token。
func (s *setupService) verifyToken(token string) bool {
	if token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.token())) == 1
}

// errSetupInvalid / errSetupAlreadyDone 是向导错误。
var (
	errSetupInvalid     = errors.New("setup: invalid input")
	errSetupAlreadyDone = errors.New("setup: already completed")
)

// setupInvalid 包装 errSetupInvalid 并附带具体原因(供 errors.Is 判定)。
func setupInvalid(msg string) error { return fmt.Errorf("%w: %s", errSetupInvalid, msg) }

func generateMasterKey() (*[32]byte, error) {
	var k [32]byte
	if _, err := rand.Read(k[:]); err != nil {
		return nil, err
	}
	return &k, nil
}

// persistMasterKey 把 base64 master key 写入文件(0600,覆盖写)。
func persistMasterKey(path string, key *[32]byte) error {
	encoded := base64.StdEncoding.EncodeToString(key[:])
	if err := os.WriteFile(path, []byte(encoded+"\n"), setupMasterKeyFileMode); err != nil {
		return err
	}
	return os.Chmod(path, setupMasterKeyFileMode)
}

// buildDSN 由向导收集的连接字段拼出数据库 DSN。
// 若 rawDSN 非空则直接采用(优先级最高);否则按 driver 用 host/port/user/password/name 组装。
// MySQL 默认端口 3306、PostgreSQL 默认 5432;host/user/name 为必填。
func buildDSN(driver, host, port, user, password, name, rawDSN string) (string, error) {
	if strings.TrimSpace(rawDSN) != "" {
		return strings.TrimSpace(rawDSN), nil
	}
	host = strings.TrimSpace(host)
	user = strings.TrimSpace(user)
	name = strings.TrimSpace(name)
	if host == "" || user == "" || name == "" {
		return "", fmt.Errorf("数据库主机、用户名、库名均为必填")
	}
	switch driver {
	case "mysql":
		if strings.TrimSpace(port) == "" {
			port = "3306"
		}
		return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&charset=utf8mb4", user, password, host, port, name), nil
	case "postgres":
		if strings.TrimSpace(port) == "" {
			port = "5432"
		}
		return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, password, host, port, name), nil
	default:
		return "", fmt.Errorf("不支持的数据库驱动:%s", driver)
	}
}

// persistEnvKV 把 KEY=VALUE 写入 atomgo.env(0600):已存在的同名键覆盖,其余行(含注释)保留。
// path 为空表示不持久化(直接返回 nil,交由调用方决定是否报错)。
func persistEnvKV(path, key, value string) error {
	if path == "" {
		return nil
	}
	value = strings.ReplaceAll(value, "\n", " ")
	var lines []string
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			trim := strings.TrimSpace(line)
			if trim == "" || strings.HasPrefix(trim, "#") {
				lines = append(lines, line)
				continue
			}
			if eq := strings.IndexByte(trim, '='); eq > 0 && strings.TrimSpace(trim[:eq]) == key {
				continue // 丢弃旧值,稍后统一在末尾追加新值
			}
			lines = append(lines, line)
		}
	}
	lines = append(lines, key+"="+value)
	out := strings.Join(lines, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
		return err
	}
	return nil
}

// envHasKey 报告 atomgo.env 中是否存在指定键(忽略注释/空行)。文件不存在视为 false。
func envHasKey(path, key string) bool {
	if path == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		if eq := strings.IndexByte(trim, '='); eq > 0 && strings.TrimSpace(trim[:eq]) == key {
			return true
		}
	}
	return false
}

// ---- HTTP 层 ----

// setupSubmitBody 是 POST /api/setup/submit 的请求体。
type setupSubmitBody struct {
	Token     string `json:"token"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	MasterKey string `json:"masterKey"`
	PublicURL string `json:"publicUrl"`
	DBDriver  string `json:"dbDriver"`
	DBHost    string `json:"dbHost"`
	DBPort    string `json:"dbPort"`
	DBUser    string `json:"dbUser"`
	DBPassword string `json:"dbPassword"`
	DBName    string `json:"dbName"`
	DBDSN     string `json:"dbDSN"`
}

// makeSetupStatusHandler: GET /api/setup/status(公开;setup 期间下发一次性 token)。
func makeSetupStatusHandler(svc SetupService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "setup_unavailable", "初始化服务未配置")
			return
		}
		st, err := svc.Status(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "setup_status_failed", "读取初始化状态失败")
			return
		}
		writeJSON(w, http.StatusOK, st)
	}
}

// makeSetupSubmitHandler: POST /api/setup/submit(需 token;幂等保护见领域层)。
func makeSetupSubmitHandler(svc SetupService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if svc == nil {
			writeError(w, http.StatusServiceUnavailable, "setup_unavailable", "初始化服务未配置")
			return
		}
		var body setupSubmitBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "bad_json", "请求体不是合法 JSON")
			return
		}
		impl, ok := svc.(*setupService)
		if !ok || !impl.verifyToken(body.Token) {
			writeError(w, http.StatusForbidden, "setup_token_invalid", "初始化令牌无效,请重新获取状态")
			return
		}
		res, err := svc.Submit(r.Context(), SetupInput{
			Username:   body.Username,
			Password:   body.Password,
			MasterKey:  body.MasterKey,
			PublicURL:  body.PublicURL,
			DBDriver:   body.DBDriver,
			DBHost:     body.DBHost,
			DBPort:     body.DBPort,
			DBUser:     body.DBUser,
			DBPassword: body.DBPassword,
			DBName:     body.DBName,
			DBDSN:      body.DBDSN,
		})
		if err != nil {
			switch {
			case errors.Is(err, errSetupAlreadyDone):
				writeError(w, http.StatusGone, "setup_already_done", "系统已完成初始化")
			case errors.Is(err, errSetupInvalid):
				writeError(w, http.StatusBadRequest, "setup_invalid", err.Error())
			default:
				writeError(w, http.StatusInternalServerError, "setup_failed", "初始化失败")
			}
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}

// mountSetup 挂载 setup 端点(公开,必须在受保护 /api 组之外注册)。
func mountSetup(r chi.Router, svc SetupService) {
	r.Get("/api/setup/status", makeSetupStatusHandler(svc))
	r.Post("/api/setup/submit", makeSetupSubmitHandler(svc))
}

// setupGateMiddleware 在 setup 模式下把除白名单外的请求全部 404。
// 白名单:/healthz、/version、/api/setup/*、以及一切非 /api 路径(SPA 自行重定向 /setup)。
func setupGateMiddleware(svc SetupService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if svc == nil || !svc.SetupRequired(r.Context()) {
				next.ServeHTTP(w, r)
				return
			}
			p := r.URL.Path
			switch {
			case p == "/healthz" || p == "/version":
				next.ServeHTTP(w, r)
			case strings.HasPrefix(p, "/api/setup"):
				next.ServeHTTP(w, r)
			case !strings.HasPrefix(p, "/api/"):
				next.ServeHTTP(w, r) // SPA:前端守卫重定向到 /setup
			default:
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":{"code":"setup_required","message":"系统尚未初始化,请先完成初始化向导"}}`))
			}
		})
	}
}
