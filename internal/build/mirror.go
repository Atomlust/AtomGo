package build

import (
	"context"
	"fmt"
	"strings"

	"github.com/atomlust/atomgo/internal/pipeline"
	"github.com/atomlust/atomgo/internal/vault"
)

// mirror.go — 晋升期向目标仓库的条件式镜像(Epic 8 · 续:FR-7 声明侧的执行闭环)。
//
// 背景:镜像在构建期只推到「本次 run 解析到的环境」的仓库,产物 reference 指向那个仓库。
// 晋升到下一环境时,目标机默认仍从构建仓库 `docker pull <reference>`;当目标环境绑了**不同**
// 仓库(且目标机够不到构建仓库)时,需要把镜像镜像到目标仓库,并让目标机改拉目标仓库引用。
//
// 单一真相源延续:registry 归环境、镜像名归项目、版本=commit。因此镜像到目标仓库时
// **只换 registry 主机前缀**,name:tag/digest 全不变(同一个镜像出现在两个仓库)。

// MirrorImageForPromotion 按需把源镜像镜像到目标环境仓库,返回目标机应拉取的引用。
//
// 返回 (deployRef, mirrored, err):
//   - deployRef:目标机应拉取的镜像引用(mirrored 时=目标仓库引用;否则=源引用)。
//   - mirrored :是否执行了跨仓库的镜像本体搬运。
//   - err      :**仅跨仓库镜像本体**任一步 docker 失败(调用方据此跳过部署、不回滚晋级记录)。
//     通道 tag 应用为 best-effort:失败不影响 deployRef(部署拉的是不可变 commit 引用,与别名无关),
//     故通道 tag 失败**不**返回 err、不阻断部署。
//
// 判定:
//   - 源引用为空 / driver 为 nil / 拿不到项目配置 / 目标环境不存在 / 目标环境未绑定仓库
//     → 不镜像,deployRef=源引用(降级:目标机仍从构建仓库拉,沿用旧行为)。
//   - 目标仓库主机 == 源引用主机 → 不搬镜像本体;若目标环境配了通道 tag 则补打(层已在,廉价)。
//   - 目标仓库主机 != 源引用主机 → 登录目标仓库 → pull 源 → tag 目标 → push 目标 → (通道 tag)。
func MirrorImageForPromotion(ctx context.Context, driver Driver, vlt vault.Vault, settings pipeline.SettingsService,
	projectID, targetEnv, sourceRef string) (deployRef string, mirrored bool, err error) {
	sourceRef = strings.TrimSpace(sourceRef)
	if sourceRef == "" || driver == nil {
		return sourceRef, false, nil
	}
	srcHost, nameTag := splitImageRef(sourceRef)
	if srcHost == "" || nameTag == "" {
		return sourceRef, false, nil // 无 registry 主机的本地引用:无法镜像,沿用
	}

	// 目标环境配置(仓库 + 通道 tag)。拿不到即降级不镜像(不因镜像阻断晋级)。
	if settings == nil {
		return sourceRef, false, nil
	}
	st, gerr := settings.Get(ctx, projectID)
	if gerr != nil || st == nil {
		return sourceRef, false, nil
	}
	var env *pipeline.Environment
	for i := range st.Environments {
		if strings.TrimSpace(st.Environments[i].Name) == strings.TrimSpace(targetEnv) {
			env = &st.Environments[i]
			break
		}
	}
	if env == nil {
		return sourceRef, false, nil
	}
	reg := env.ImageRegistry
	if strings.TrimSpace(reg.Type) == "" || strings.TrimSpace(reg.URL) == "" {
		return sourceRef, false, nil // 目标环境未绑定仓库 → 不镜像
	}
	targetHost := normalizeRegistryHost(reg.URL)
	channelTag := strings.TrimSpace(env.ChannelTag)
	cap := &stderrCapture{}
	onLine := cap.onLine

	// 同仓库:不搬本体。仅在配了通道 tag 时补打移动别名(需先 pull 到本地再 tag,层已在仓库、廉价)。
	// 通道 tag 为 best-effort:失败不阻断部署(部署拉 sourceRef,与别名无关)。
	if targetHost == srcHost {
		if channelTag != "" {
			_ = applyChannelTag(ctx, driver, vlt, reg, srcHost, nameTag, channelTag, true, onLine)
		}
		return sourceRef, false, nil
	}

	// 异仓库:完整镜像(登录 → pull 源 → tag → push 目标 → 通道 tag)。
	targetRef := targetHost + "/" + nameTag
	if user, pass := revealRegistryUserPass(vlt, reg.CredentialID); user != "" {
		if code, lerr := driver.Login(ctx, reg.URL, user, pass, onLine); lerr != nil || code != 0 {
			return "", false, fmt.Errorf("目标仓库登录失败:%s", cap.last)
		}
	}
	if code, perr := driver.Pull(ctx, sourceRef, onLine); perr != nil || code != 0 {
		return "", false, fmt.Errorf("拉取源镜像失败:%s", cap.last)
	}
	if code, terr := driver.Tag(ctx, sourceRef, targetRef, onLine); terr != nil || code != 0 {
		return "", false, fmt.Errorf("打目标 tag 失败:%s", cap.last)
	}
	if code, perr := driver.Push(ctx, targetRef, onLine); perr != nil || code != 0 {
		return "", false, fmt.Errorf("推送目标仓库失败:%s", cap.last)
	}
	// 通道 tag 为 best-effort:目标镜像已推送成功(deployRef 有效),别名失败不阻断部署。
	if channelTag != "" {
		_ = applyChannelTag(ctx, driver, vlt, reg, targetHost, nameTag, channelTag, false, onLine)
	}
	return targetRef, true, nil
}

// applyChannelTag 在指定仓库主机上,把 nameTag 指向的镜像补打一个 <name>:<channelTag> 并推送。
// needsPull=true 时先 pull 源引用确保本地有该镜像(同仓库场景,晋升进程未必持有);
// needsPull=false 表示源引用已在本地(异仓库场景刚 push 的 targetRef),跳过 pull。
// 若该仓库需要鉴权,按需先登录。
func applyChannelTag(ctx context.Context, driver Driver, vlt vault.Vault, reg pipeline.ImageRegistry,
	host, nameTag, channelTag string, needsPull bool, onLine func(stream, line string)) error {
	name := nameWithoutTag(nameTag)
	if name == "" {
		return fmt.Errorf("无法从引用 %q 解析镜像名", nameTag)
	}
	channelRef := host + "/" + name + ":" + channelTag
	sourceRef := host + "/" + nameTag
	cap := &stderrCapture{}
	on := func(stream, line string) {
		cap.onLine(stream, line)
		if onLine != nil {
			onLine(stream, line)
		}
	}
	if user, pass := revealRegistryUserPass(vlt, reg.CredentialID); user != "" {
		if code, lerr := driver.Login(ctx, reg.URL, user, pass, on); lerr != nil || code != 0 {
			return fmt.Errorf("通道 tag 登录失败:%s", cap.last)
		}
	}
	if needsPull {
		if code, perr := driver.Pull(ctx, sourceRef, on); perr != nil || code != 0 {
			return fmt.Errorf("拉取源镜像失败:%s", cap.last)
		}
	}
	if code, terr := driver.Tag(ctx, sourceRef, channelRef, on); terr != nil || code != 0 {
		return fmt.Errorf("打通道 tag 失败:%s", cap.last)
	}
	if code, perr := driver.Push(ctx, channelRef, on); perr != nil || code != 0 {
		return fmt.Errorf("推送通道 tag 失败:%s", cap.last)
	}
	return nil
}

// splitImageRef 把镜像引用拆成 (registry 主机, name:tag)。引用形如 <host>/<name>:<tag>。
// 无 '/'(无主机)时 host 返回空。name:tag 部分原样保留(含多级路径 org/team/app)。
func splitImageRef(ref string) (host, nameTag string) {
	ref = strings.TrimSpace(ref)
	i := strings.Index(ref, "/")
	if i < 0 {
		return "", ref
	}
	return ref[:i], ref[i+1:]
}

// nameWithoutTag 去掉 name:tag 里的 :tag,返回 name(可能含 / 路径)。
func nameWithoutTag(nameTag string) string {
	if i := strings.LastIndex(nameTag, ":"); i >= 0 {
		return nameTag[:i]
	}
	return nameTag
}

// normalizeRegistryHost 归一化 registry 主机:去协议、去尾斜杠、转小写(主机大小写不敏感)。
func normalizeRegistryHost(url string) string {
	url = strings.TrimSpace(url)
	if i := strings.Index(url, "://"); i >= 0 {
		url = url[i+3:]
	}
	url = strings.TrimRight(url, "/")
	return strings.ToLower(url)
}

// revealRegistryUserPass 取 registry 凭据明文并拆成 user/password(与 Builder.revealRegistryCred 同约定:
// "user:password";无冒号时 user 空、整串作口令)。失败/未配返回空。
func revealRegistryUserPass(v vault.Vault, credID string) (string, string) {
	if v == nil || strings.TrimSpace(credID) == "" {
		return "", ""
	}
	pt, err := v.Reveal(credID)
	if err != nil || pt == "" {
		return "", ""
	}
	if i := strings.Index(pt, ":"); i >= 0 {
		return pt[:i], pt[i+1:]
	}
	return "", pt
}

// stderrCapture 捕获 docker 命令的最后一行 stderr,用于把失败原因带进错误信息。
type stderrCapture struct{ last string }

func (c *stderrCapture) onLine(stream, line string) {
	if stream == streamStderr && strings.TrimSpace(line) != "" {
		c.last = strings.TrimSpace(line)
	}
}
