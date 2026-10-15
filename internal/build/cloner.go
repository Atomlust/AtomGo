package build

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"

	"github.com/atomlust/atomgo/internal/gitauth"
)

// cloneTimeout 是单次克隆的硬超时(防黑洞 IP / 慢 DNS 把构建 goroutine 挂死)。
const cloneTimeout = 5 * time.Minute

// 克隆领域错误(不泄漏 URL 密钥/底层细节)。
var (
	// ErrRepoBlocked 表示仓库地址被 SSRF 收口拒绝(云元数据/链路本地/回环等)。
	ErrRepoBlocked = errors.New("build: repo url blocked by ssrf guard")
	// ErrCloneFailed 表示克隆/检出失败(鉴权/网络/ref 不存在等;不泄漏底层文本)。
	ErrCloneFailed = errors.New("build: clone failed")
)

// CloneErrorCategory 是克隆失败的可安全暴露类别(刻意不含 URL/密钥/底层文本)。
// 用途:前端/用户仅凭类别即可定位是「凭据」「网络」「ref」还是「SSRF」,无需看原始错误。
type CloneErrorCategory string

const (
	CatAuth    CloneErrorCategory = "auth"    // 鉴权被拒(凭据过期/吊销/权限不足)
	CatNetwork CloneErrorCategory = "network" // 网络不可达(DNS/代理/被墙/超时)
	CatRef     CloneErrorCategory = "ref"     // 引用不存在(分支被删/commit 被覆盖)
	CatSSRF    CloneErrorCategory = "ssrf"    // 仓库地址被 SSRF 收口拒绝
	CatUnknown CloneErrorCategory = "unknown" // 无法归类
)

// Message 返回对应用户可读、不含敏感信息的失败说明(定位用)。
func (k CloneErrorCategory) Message() string {
	switch k {
	case CatAuth:
		return "源码克隆失败:鉴权被拒 — 请检查项目绑定的 Git 凭据/令牌是否过期、被吊销或权限不足(私仓需可读权限)"
	case CatNetwork:
		return "源码克隆失败:网络不可达 — 请检查本机到仓库主机的网络/DNS/代理;公网仓库(GitHub 等)在国内可能需代理或被墙"
	case CatRef:
		return "源码克隆失败:引用(ref)不存在 — 请检查本次触发指定的分支名或 commit 是否仍存在(分支被删/commit 被 force-push 覆盖)"
	case CatSSRF:
		return "源码克隆失败:仓库地址被 SSRF 收口拒绝 — 仅允许 http(s)/git-ssh,拒云元数据/链路本地/回环;检查仓库地址是否合法(勿在 ssh:// 中嵌密码)"
	default:
		return "源码克隆失败(鉴权/网络/ref 不存在或被 SSRF 拒绝)"
	}
}

// CloneError 是克隆失败的统一错误:携带类别(可安全暴露)+ 底层原因(仅用于 errors.Is/日志,不进用户消息)。
// Unwrap 透传 ErrRepoBlocked / ErrCloneFailed 哨兵,保持既有 errors.Is 语义。
type CloneError struct {
	Category CloneErrorCategory
	Cause    error
}

func (e *CloneError) Error() string { return e.Category.Message() }
func (e *CloneError) Unwrap() error { return e.Cause }

// classifyCloneErr 把 go-git 底层错误归类为可安全暴露的类别。
// 安全约束:仅用底层错误做「分类判断」,绝不把其文本拼进用户消息(可能含主机/路径细节)。
func classifyCloneErr(err error) CloneErrorCategory {
	if err == nil {
		return CatUnknown
	}
	// 鉴权类:go-git 显式类型错误,最可靠。
	if errors.Is(err, transport.ErrAuthenticationRequired) ||
		errors.Is(err, transport.ErrAuthorizationFailed) {
		return CatAuth
	}
	msg := strings.ToLower(err.Error())
	// 引用/ref 不存在(分支/commit 检索失败)。
	if strings.Contains(msg, "reference not found") ||
		strings.Contains(msg, "unknown revision") ||
		strings.Contains(msg, "does not exist") {
		return CatRef
	}
	// 网络类:Go 标准错误类型。
	var netErr *net.OpError
	var urlErr *url.Error
	var sysErr *os.SyscallError
	if errors.As(err, &netErr) || errors.As(err, &urlErr) || errors.As(err, &sysErr) {
		return CatNetwork
	}
	// 网络类:常见文本特征(go-git HTTP/Smart 传输包裹后的描述)。
	if strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "dial tcp") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "tls handshake") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "no route to host") ||
		strings.Contains(msg, "proxy") {
		return CatNetwork
	}
	return CatUnknown
}

// Cloner 把项目仓库在指定 commit/branch 上克隆到**磁盘临时工作区**(Docker build 需 `.` 上下文落盘)。
//
// 与 2-5/3-6 的内存克隆(memfs)不同:构建上下文须是真实目录树供容器 CLI 读取。工作区由调用方
// (Builder)经 MkdirTemp 建、defer RemoveAll 销(宿主零污染,FR-5)。SSRF 收口复用全平台同款策略:
// 仅 HTTP(S) 或 Git SSH;拒云元数据/链路本地/回环;私网放行(自托管内网 Git 友好)。
type Cloner struct {
	// allowInsecure 仅供测试:为 true 时跳过 SSRF scheme/host 校验(放行 file:// 本地夹具)。
	// 生产路径绝不设置(NewCloner 默认 false)。
	allowInsecure bool
}

// NewCloner 构造生产 Cloner(严格 SSRF 收口)。
func NewCloner() *Cloner { return &Cloner{} }

// CloneResolved 是克隆结果:实际检出的 commit 短 sha(供产物 tag/引用),空则未解析出。
type CloneResolved struct {
	CommitShort string
}

// Clone 把 repoURL 在 ref(commit sha 优先,否则分支名;皆空则默认分支)上克隆到 destDir。
// token 经 BasicAuth.Password 传入(绝不进 URL/日志/错误)。失败统一映射干净错误。
//
// 策略:先克隆默认/指定分支(Depth:1 浅克隆省带宽),若指定了 commit 则再 checkout 到该 commit
//
//	(commit 不在浅克隆历史里时回退为不限深克隆重试一次,best-effort)。
func (c *Cloner) Clone(ctx context.Context, repoURL, username, token, branch, commit, destDir string) (*CloneResolved, error) {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return nil, &CloneError{Category: CatUnknown, Cause: ErrCloneFailed}
	}
	if !c.allowInsecure && !validRepoURL(repoURL) {
		return nil, &CloneError{Category: CatSSRF, Cause: ErrRepoBlocked}
	}

	cctx, cancel := context.WithTimeout(ctx, cloneTimeout)
	defer cancel()

	auth, err := gitauth.AuthMethod(repoURL, username, token)
	if err != nil {
		return nil, ErrCloneFailed
	}
	commit = strings.TrimSpace(commit)
	branch = strings.TrimSpace(branch)

	opts := &gogit.CloneOptions{
		URL:  repoURL,
		Auth: auth,
		Tags: gogit.NoTags,
	}
	// 指定了具体 commit 时不浅克隆(浅克隆默认分支可能不含该 commit);否则浅克隆指定/默认分支。
	if commit == "" {
		opts.Depth = 1
		opts.SingleBranch = true
		if branch != "" {
			opts.ReferenceName = plumbing.NewBranchReferenceName(branch)
		}
	}

	repo, err := gogit.PlainCloneContext(cctx, destDir, false, opts)
	if err != nil {
		// 浅克隆 + 指定分支失败时不再重试(可能是鉴权/不可达);映射干净错误(带安全类别)。
		return nil, &CloneError{Category: classifyCloneErr(err), Cause: ErrCloneFailed}
	}

	resolved := &CloneResolved{}
	if commit != "" {
		wt, werr := repo.Worktree()
		if werr != nil {
			return nil, &CloneError{Category: classifyCloneErr(werr), Cause: ErrCloneFailed}
		}
		hash := plumbing.NewHash(commit)
		if cerr := wt.Checkout(&gogit.CheckoutOptions{Hash: hash, Force: true}); cerr != nil {
			return nil, &CloneError{Category: classifyCloneErr(cerr), Cause: ErrCloneFailed}
		}
		resolved.CommitShort = shortSHA(commit)
	} else if head, herr := repo.Head(); herr == nil {
		resolved.CommitShort = shortSHA(head.Hash().String())
	}
	return resolved, nil
}

// IsRepoURLAllowed 是 validRepoURL 的导出包装(供 repocache 等复用同款 SSRF 收口,不重复实现)。
func IsRepoURLAllowed(repoURL string) bool { return validRepoURL(repoURL) }

// validRepoURL 对仓库地址做 SSRF 收口(生产路径),复用全平台同款策略:
// 仅 http/https;拒云元数据/链路本地/回环;私网放行(自托管内网 Git 友好)。
func validRepoURL(repoURL string) bool {
	return gitauth.AllowedRepoURL(repoURL)
}

// shortSHA 取 commit 的前 7 位(短 sha);不足 7 位原样返回。
func shortSHA(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// mkTempWorkspace 建一个临时构建工作区目录(调用方 defer RemoveAll 销毁;宿主零污染,FR-5)。
func mkTempWorkspace() (string, error) {
	return os.MkdirTemp("", "atomgo-build-*")
}
