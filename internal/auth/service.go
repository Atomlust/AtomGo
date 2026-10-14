package auth

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
)

// Authenticator 定义认证领域对外接口。(-er 命名约定)
type Authenticator interface {
	// Login 以用户名/口令认证;成功返回新 Session(绑定用户名),失败或锁定返回相应错误。
	Login(username, password string) (*Session, error)
	// Verify 校验会话 token 是否有效;有效返回 Session。
	Verify(token string) (*Session, error)
	// Logout 使指定 token 的会话失效。
	Logout(token string) error
	// AdminUsername 返回首个管理员用户名;无则空(回显兜底,非安全判定)。
	AdminUsername() (string, error)
	// UserByUsername 按用户名反查用户(含角色),供 requireAuth 注入身份。
	UserByUsername(username string) (*User, error)
}

// ErrInvalidCredentials 用户名或口令错误。
var ErrInvalidCredentials = errors.New("auth: invalid credentials")

// ErrInvalidCurrentPassword 改口令时当前口令校验失败。
var ErrInvalidCurrentPassword = errors.New("auth: invalid current password")

// ErrWeakPassword 新口令不满足强度规则(至少 8 位、非空)。
var ErrWeakPassword = errors.New("auth: weak password")

// minPasswordLen 新口令最小长度。
const minPasswordLen = 8

// ErrLockedOut 登录失败次数过多被锁定。
type ErrLockedOut struct {
	RetryAfter time.Duration
}

func (e *ErrLockedOut) Error() string {
	mins := int(e.RetryAfter.Minutes()) + 1
	return fmt.Sprintf("auth: locked out, retry after %d minutes", mins)
}

// dummyHash 是一个**预计算的静态** argon2id PHC 串(参数与真实哈希一致:
// m=64MB,t=1,p=4),用于在用户名不匹配 / 用户不存在时仍执行一次口令校验,
// 使「用户名错」与「口令错」两条失败路径耗时一致,消除时序枚举侧信道。
// 其明文不可知且永不匹配真实输入。
const dummyHash = "$argon2id$v=19$m=65536,t=1,p=4$WyWRpzFnab6vlgFLECjL9g$ZJhWU5lKWEo1T7NhxBxQf70Gk+We+sUQ8SO52b8nnxQ"

// Service 实现 Authenticator,依赖 *sql.DB(经构造函数注入)。
type Service struct {
	db       *sql.DB
	sessions *SessionStore
	lockout  *LockoutManager
}

// NewService 构造 Service;clock 为 nil 时使用 RealClock。
func NewService(db *sql.DB, clock Clock) *Service {
	return &Service{
		db:       db,
		sessions: NewSessionStore(db),
		lockout:  NewLockoutManager(clock),
	}
}

// Bootstrap 首次启动引导:若 users 为空且提供了口令则 argon2id 哈希入库(role='admin')。
// 已存在则忽略 env;无口令且无用户时明确日志提示(不 panic,不阻断 /healthz)。
func (s *Service) Bootstrap(username, password string) error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM users`).Scan(&count); err != nil {
		return fmt.Errorf("auth: bootstrap check: %w", err)
	}
	if count > 0 {
		// 已有用户,忽略 env(口令变更在 ChangePassword)。
		return nil
	}
	if password == "" {
		// 无口令且无用户:提示如何设置,但不阻断服务。
		log.Printf("[auth] 管理员账号未初始化:首次启动请浏览器打开 /setup 完成初始化向导(将创建管理员账号),或设置 ATOMGO_ADMIN_PASSWORD 环境变量后重启。")
		return nil
	}
	if username == "" {
		username = "admin"
	}
	hash, err := HashPassword(password)
	if err != nil {
		return fmt.Errorf("auth: bootstrap hash: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.Exec(
		`INSERT INTO users (id, username, password_hash, role, created_at, updated_at)
		 VALUES (?, ?, ?, 'admin', ?, ?)`,
		uuid.NewString(), username, hash, now, now,
	)
	if err != nil {
		return fmt.Errorf("auth: bootstrap insert: %w", err)
	}
	log.Printf("[auth] 管理员账号已初始化:username=%s", username)
	return nil
}

// NeedsSetup 报告是否尚未创建任何用户(users 表为空)。
// 用于启动横幅判断是否提示用户走 Web 初始化向导。
func (s *Service) NeedsSetup(ctx context.Context) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM users`).Scan(&count); err != nil {
		return false, fmt.Errorf("auth: needs-setup check: %w", err)
	}
	return count == 0, nil
}

// Login 认证用户名/口令;通过则创建新会话(绑定用户名),失败则记录失败计数。
func (s *Service) Login(username, password string) (*Session, error) {
	// 按用户名查询用户。即便不匹配也要继续走口令校验路径以消除时序枚举。
	var id, storedUsername, storedHash string
	err := s.db.QueryRow(
		`SELECT id, username, password_hash FROM users WHERE username = ?`,
		username,
	).Scan(&id, &storedUsername, &storedHash)

	usernameMatches := false
	verifyHash := dummyHash
	switch {
	case err == nil:
		// 恒定时间比较用户名,避免按字节短路泄露。
		usernameMatches = subtle.ConstantTimeCompare([]byte(storedUsername), []byte(username)) == 1
		if usernameMatches {
			verifyHash = storedHash
		}
	case errors.Is(err, sql.ErrNoRows):
		// 无此用户:用 dummy hash 校验,使耗时与正常路径一致。
		usernameMatches = false
	default:
		return nil, fmt.Errorf("auth: query user: %w", err)
	}

	// 在单次持锁内完成「检查锁定 → 口令校验 → 记失败/重置」,消除 TOCTOU。
	ok, locked, remaining, gErr := s.lockout.Guard(func() (bool, error) {
		pwOK, vErr := VerifyPassword(password, verifyHash)
		if vErr != nil {
			return false, vErr
		}
		// 用户名不匹配时即使口令校验偶然通过(dummy 永不通过)也判失败。
		return usernameMatches && pwOK, nil
	})
	if locked {
		return nil, &ErrLockedOut{RetryAfter: remaining}
	}
	if gErr != nil {
		return nil, fmt.Errorf("auth: verify password: %w", gErr)
	}
	if !ok {
		return nil, ErrInvalidCredentials
	}

	// 认证成功:创建会话(Guard 已重置计数),绑定规范用户名。
	sess, err := s.sessions.Create(storedUsername)
	if err != nil {
		return nil, fmt.Errorf("auth: create session: %w", err)
	}
	return sess, nil
}

// PurgeExpiredSessions 删除 DB 中所有已过期会话,返回删除行数。
func (s *Service) PurgeExpiredSessions() (int64, error) {
	return s.sessions.PurgeExpired()
}

// AdminUsername 返回首个管理员用户名;无管理员时返回空。
func (s *Service) AdminUsername() (string, error) {
	var u string
	err := s.db.QueryRow(
		`SELECT username FROM users WHERE role = ? ORDER BY id ASC LIMIT 1`, RoleAdmin,
	).Scan(&u)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("auth: query admin username: %w", err)
	}
	return u, nil
}

// HasAdmin 报告是否已存在任何用户(沿用旧语义:有账号即视为已初始化)。
func (s *Service) HasAdmin() (bool, error) {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM users`).Scan(&count); err != nil {
		return false, fmt.Errorf("auth: count users: %w", err)
	}
	return count > 0, nil
}

// Verify 校验会话 token;有效返回 Session。
func (s *Service) Verify(token string) (*Session, error) {
	sess, err := s.sessions.Get(token)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("auth: verify session: %w", err)
	}
	return sess, nil
}

// Logout 删除会话。
func (s *Service) Logout(token string) error {
	return s.sessions.Delete(token)
}

// UserByUsername 按用户名反查用户(含 ID 与角色),供 requireAuth 注入身份与权限判定。
func (s *Service) UserByUsername(username string) (*User, error) {
	if username == "" {
		return nil, ErrUserNotFound
	}
	var u User
	err := s.db.QueryRow(
		`SELECT id, username, role FROM users WHERE username = ?`, username,
	).Scan(&u.ID, &u.Username, &u.Role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("auth: query user: %w", err)
	}
	return &u, nil
}

// CountUsers 返回用户总数。
func (s *Service) CountUsers() (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("auth: count users: %w", err)
	}
	return n, nil
}

// ListUsers 返回全部用户(按创建序);不含口令哈希。
func (s *Service) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`SELECT id, username, role, created_at, updated_at FROM users ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("auth: list users: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []User
	for rows.Next() {
		var u User
		var createdStr, updatedStr string
		if err := rows.Scan(&u.ID, &u.Username, &u.Role, &createdStr, &updatedStr); err != nil {
			return nil, fmt.Errorf("auth: scan user: %w", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auth: iterate users: %w", err)
	}
	return out, nil
}

// GetUser 按 ID 返回用户(不含口令哈希)。
func (s *Service) GetUser(id string) (*User, error) {
	var u User
	err := s.db.QueryRow(
		`SELECT id, username, role FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.Username, &u.Role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("auth: get user: %w", err)
	}
	return &u, nil
}

// CreateUser 创建新用户(管理员操作)。username 唯一;role 须为内置角色;口令强度校验。
func (s *Service) CreateUser(username, password, role string) (*User, error) {
	if username == "" {
		return nil, ErrWeakPassword
	}
	if !IsKnownRole(role) {
		return nil, ErrInvalidRole
	}
	if len(password) < minPasswordLen {
		return nil, ErrWeakPassword
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("auth: hash: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.Exec(
		`INSERT INTO users (id, username, password_hash, role, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), username, hash, role, now, now,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicateUser
		}
		return nil, fmt.Errorf("auth: create user: %w", err)
	}
	return s.UserByUsername(username)
}

// SetUserRole 修改用户角色。拒绝把最后一位管理员改为非管理员(避免锁死)。
func (s *Service) SetUserRole(id, role string) error {
	if !IsKnownRole(role) {
		return ErrInvalidRole
	}
	u, err := s.GetUser(id)
	if err != nil {
		return err
	}
	if u.Role == RoleAdmin && role != RoleAdmin {
		remaining, err := s.countAdminsExcept(id)
		if err != nil {
			return err
		}
		if remaining == 0 {
			return ErrLastAdmin
		}
	}
	if _, err := s.db.Exec(`UPDATE users SET role = ?, updated_at = ? WHERE id = ?`, role, time.Now().UTC().Format(time.RFC3339), id); err != nil {
		return fmt.Errorf("auth: set role: %w", err)
	}
	return nil
}

// DeleteUser 删除用户。拒绝删除最后一位管理员(避免锁死)。
func (s *Service) DeleteUser(id string) error {
	u, err := s.GetUser(id)
	if err != nil {
		return err
	}
	if u.Role == RoleAdmin {
		remaining, err := s.countAdminsExcept(id)
		if err != nil {
			return err
		}
		if remaining == 0 {
			return ErrLastAdmin
		}
	}
	res, err := s.db.Exec(`DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("auth: delete user: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

// ResetUserPassword 由管理员重置某用户口令(不校验该用户原口令)。强度校验。
func (s *Service) ResetUserPassword(id, newPassword string) error {
	if len(newPassword) < minPasswordLen {
		return ErrWeakPassword
	}
	if _, err := s.GetUser(id); err != nil {
		return err
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("auth: hash: %w", err)
	}
	if _, err := s.db.Exec(`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`, hash, time.Now().UTC().Format(time.RFC3339), id); err != nil {
		return fmt.Errorf("auth: reset password: %w", err)
	}
	return nil
}

// countAdminsExcept 返回除 exceptID 外的管理员数量。
func (s *Service) countAdminsExcept(exceptID string) (int, error) {
	var n int
	if err := s.db.QueryRow(
		`SELECT COUNT(1) FROM users WHERE role = ? AND id <> ?`, RoleAdmin, exceptID,
	).Scan(&n); err != nil {
		return 0, fmt.Errorf("auth: count admins: %w", err)
	}
	return n, nil
}

// ChangePassword 校验当前口令 → 更新为新 argon2id 哈希 → 删除除当前会话外的所有会话。
//   - current 错 → ErrInvalidCurrentPassword(401)。
//   - new 不满足强度(< minPasswordLen 或空)→ ErrWeakPassword(422)。
//   - 成功:更新 users.password_hash;DELETE WHERE token != currentToken(当前不掉线)。
func (s *Service) ChangePassword(current, newPassword, currentToken string) error {
	// 当前会话 token 必须存在(requireAuth 已保证 cookie 在;空 token 属异常状态)。
	if currentToken == "" {
		return ErrInvalidCurrentPassword
	}
	if len(newPassword) < minPasswordLen {
		return ErrWeakPassword
	}

	// 由当前会话定位用户。
	sess, err := s.sessions.Get(currentToken)
	if err != nil {
		return ErrInvalidCurrentPassword
	}
	if sess.Username == "" {
		return ErrInvalidCurrentPassword
	}
	var userID string
	if err := s.db.QueryRow(`SELECT id FROM users WHERE username = ?`, sess.Username).Scan(&userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidCurrentPassword
		}
		return fmt.Errorf("auth: resolve user: %w", err)
	}

	var storedHash string
	err = s.db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, userID).Scan(&storedHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidCurrentPassword
		}
		return fmt.Errorf("auth: query user hash: %w", err)
	}

	ok, err := VerifyPassword(current, storedHash)
	if err != nil {
		return fmt.Errorf("auth: verify current password: %w", err)
	}
	if !ok {
		return ErrInvalidCurrentPassword
	}

	newHash, err := HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("auth: hash new password: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := s.db.Exec(
		`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`,
		newHash, now, userID,
	); err != nil {
		return fmt.Errorf("auth: update password hash: %w", err)
	}

	// 改口令后使其它会话失效(当前 token 保留,用户不掉线)。
	if _, err := s.sessions.DeleteOthers(currentToken); err != nil {
		return fmt.Errorf("auth: revoke other sessions: %w", err)
	}
	return nil
}

// ListSessions 返回所有未过期会话元数据(不含原 token);currentToken 标识 current。
func (s *Service) ListSessions(currentToken string) ([]SessionMeta, error) {
	return s.sessions.List(currentToken)
}

// RevokeSession 按 id(sha256 前缀)删除会话;无匹配 → ErrSessionNotFound。
func (s *Service) RevokeSession(id string) error {
	n, err := s.sessions.DeleteByIDPrefix(id)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// isUniqueViolation 报告 err 是否为唯一约束冲突(跨方言:sqlite/mysql/postgres)。
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return containsAny(msg, []string{"UNIQUE", "Duplicate", "duplicate", "23505", "constraint failed", "constraint violation"})
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && indexOf(s, sub) >= 0 {
			return true
		}
	}
	return false
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
