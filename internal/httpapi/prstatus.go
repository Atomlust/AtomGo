package httpapi

import (
	"context"
	"log"
	"strings"

	"github.com/atomlust/atomgo/internal/project"
	"github.com/atomlust/atomgo/internal/prstatus"
	"github.com/atomlust/atomgo/internal/run"
	"github.com/atomlust/atomgo/internal/vault"
)

// prstatus.go 装配「运行结果回写代码平台提交状态」的终态钩子(Story 8-9 / FR-8-9)。
//
// 钩子由 main 在 run 终态时调用(复用通知钩子的终态时机,组合执行):取本次运行的 commit +
// 项目仓库/凭据 → 识别平台(GitHub/Gitee)→ 经项目凭据回写提交状态(success/failure)+ 详情链接。
// best-effort:无 commit / 不支持平台 / 无凭据 / 回写失败 一律静默跳过或仅记日志,绝不影响运行终态。

// NewPRStatusHook 构造 PR 状态回写终态钩子。publicBaseURL 为平台对外访问地址的**惰性提供者**
// (用于 target_url;每次回写时读取,故首次启动 Web 向导里设置的 public_url 即时生效,无需重启;
// 返回空则不带链接)。token 经 vault 即取即用,绝不进 URL/日志。
//
// 钩子始终挂载,但仅在该 run 的项目**开启了 PR 状态检查**(projects.pr_status_enabled)时才回写;
// globalOverride=true(历史全局 env ATOMGO_PR_STATUS=1)则无视每项目开关,对所有项目回写。
// 默认(开关关 + 无全局强开)→ 不回写,行为与历史 env 未设时一致。
func NewPRStatusHook(
	runs run.Service,
	projects project.Service,
	v vault.Vault,
	reporter *prstatus.Reporter,
	publicBaseURL func() string,
	globalOverride bool,
) func(ctx context.Context, runID, finalStatus string) {
	return func(ctx context.Context, runID, finalStatus string) {
		r, err := runs.Get(ctx, runID)
		if err != nil {
			return
		}
		commit := strings.TrimSpace(r.Trigger.Commit)
		if commit == "" {
			return // 无 commit(如手动触发未指定):无处可回写
		}
		proj, err := projects.Get(ctx, r.ProjectID)
		if err != nil {
			return
		}
		// 每项目开关门控:未开启且无全局强开 → 静默跳过(老项目默认行为)。
		if !globalOverride && !proj.PRStatusEnabled {
			return
		}
		target, ok := prstatus.Detect(proj.RepoURL)
		if !ok {
			return // 不支持的代码平台
		}
		token := revealPRToken(v, proj.CredentialID)
		if token == "" {
			return // 无可用凭据
		}

		state := prstatus.StateForRunStatus(finalStatus)
		desc := prDescription(finalStatus)
		targetURL := ""
		if publicBaseURL != nil {
			if base := strings.TrimRight(strings.TrimSpace(publicBaseURL()), "/"); base != "" {
				targetURL = base + "/runs/" + runID
			}
		}
		if err := reporter.Report(ctx, target, commit, state, desc, targetURL, token); err != nil {
			log.Printf("[prstatus] 回写 %s/%s 失败(run %s):%v", target.Owner, target.Repo, runID, err)
			return
		}
		log.Printf("[prstatus] 已回写 %s %s/%s @ %s = %s", target.Platform, target.Owner, target.Repo, shortSHA(commit), state)
	}
}

// revealPRToken 取项目凭据明文(失败/未配 → 空,best-effort)。
func revealPRToken(v vault.Vault, credID string) string {
	if v == nil || strings.TrimSpace(credID) == "" {
		return ""
	}
	tok, err := v.Reveal(credID)
	if err != nil {
		return ""
	}
	return tok
}

func prDescription(finalStatus string) string {
	if finalStatus == run.StatusSuccess {
		return "AtomGo 流水线成功"
	}
	return "AtomGo 流水线失败"
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
