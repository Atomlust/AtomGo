package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/atomlust/atomgo/internal/approval"
	"github.com/atomlust/atomgo/internal/audit"
	"github.com/atomlust/atomgo/internal/deploy"
	"github.com/atomlust/atomgo/internal/promotion"
	"github.com/atomlust/atomgo/internal/run"
	"github.com/atomlust/atomgo/internal/vault"
	"github.com/go-chi/chi/v5"
)

// promotion.go 装配环境晋级流(Epic 8 · Story 8-7 / FR-8-7)的 HTTP 端点与适配器。
//
// 复用:晋级审批门复用 FR-8-4 的 approval.Coordinator + run_approvals(promotionGate);
// 源运行状态/归属经 run.Service 适配(runLookup);secret 变量经 vault.Vault 解密(secretReveal)。

// promotionGateTimeout 是晋级审批门兜底超时(超时自动拒绝)。审批节奏可较慢,取较长值。
const promotionGateTimeout = 24 * time.Hour

// imageMirrorFunc 晋升镜像钩子(FR-7 执行闭环):批准后把 image 产物按需镜像到目标环境仓库,
// 返回目标机应拉取的引用(同仓库=源引用;异仓库=目标仓库引用)。由 main 注入 build.MirrorImageForPromotion;
// nil → 不镜像(目标机仍从构建仓库拉,沿用旧行为)。
type imageMirrorFunc func(ctx context.Context, projectID, targetEnv, sourceRef string) (deployRef string, err error)

// runLookup 把 run.Service 适配为 promotion.RunLookup(只取状态/归属,不引入循环依赖)。
type runLookup struct{ runs run.Service }

func (l runLookup) LookupRun(ctx context.Context, runID string) (promotion.RunInfo, error) {
	r, err := l.runs.Get(ctx, runID)
	if err != nil {
		if errors.Is(err, run.ErrNotFound) {
			return promotion.RunInfo{}, promotion.ErrRunNotFound
		}
		return promotion.RunInfo{}, err
	}
	return promotion.RunInfo{ID: r.ID, ProjectID: r.ProjectID, Status: r.Status}, nil
}

// secretReveal 把 vault.Vault 适配为 promotion.SecretResolver(Reveal 不刷新 last_used_at)。
type secretReveal struct{ v vault.Vault }

func (s secretReveal) Reveal(credentialID string) (string, error) {
	if s.v == nil {
		return "", errors.New("vault unconfigured")
	}
	return s.v.Reveal(credentialID)
}

// promotionGate 复用审批门内核(approval.Coordinator + Store)实现 promotion.Gate:
// 对 gated 目标环境,登记待批 + 阻塞协调器等待端点投递批准/拒绝(或超时)。
//
// 复用而非复制:Wait/Resolve/Cancel 全走既有 approval.Coordinator;待批/决定落 run_approvals
// (stageId = "promote:<env>"),晋级端点(approve/reject)直接复用既有 /runs/{id}/approve|reject。
type promotionGate struct {
	coord *approval.Coordinator
	store *approval.Store
	runID string // 审批门内核以 (runID, stageId) 组合键;晋级门挂在源运行上
}

func (g promotionGate) Await(ctx context.Context, stageID string, info promotion.GateInfo) (bool, string, error) {
	key := approval.Key(g.runID, stageID)
	stageName := "晋级到「" + info.TargetEnvironment + "」"
	_ = g.store.CreatePending(ctx, g.runID, stageID, stageName)
	ch := g.coord.Wait(key)
	defer g.coord.Cancel(key)

	select {
	case d := <-ch:
		// 审批决定校验(纯函数 evaluateApprovalDecision):批准者授权 + 禁止自审。
		// 任一不满足即拒绝(fail-closed,绝不擅自放行)。
		if d.Approved {
			if derr := evaluateApprovalDecision(d.Actor, info); derr != nil {
				_ = g.store.Decide(context.Background(), g.runID, stageID, approval.StatusRejected, d.Actor)
				return false, d.Actor, derr
			}
		}
		status := approval.StatusRejected
		if d.Approved {
			status = approval.StatusApproved
		}
		_ = g.store.Decide(context.Background(), g.runID, stageID, status, d.Actor)
		return d.Approved, d.Actor, nil
	case <-ctx.Done():
		_ = g.store.Decide(context.Background(), g.runID, stageID, approval.StatusRejected, "canceled")
		return false, "canceled", ctx.Err()
	case <-time.After(promotionGateTimeout):
		_ = g.store.Decide(context.Background(), g.runID, stageID, approval.StatusRejected, "timeout")
		return false, "timeout", context.DeadlineExceeded
	}
}

// approverAllowed 报告批准者是否被允许批准晋级到某环境。
// 环境未配置审批人名单(len==0)→ 不限定身份(向后兼容,行为同合并前);
// 配置了名单 → 批准者须在名单内(逐项去空白后精确匹配)。
func approverAllowed(actor string, approvers []string) bool {
	if len(approvers) == 0 {
		return true
	}
	actor = strings.TrimSpace(actor)
	for _, a := range approvers {
		if strings.TrimSpace(a) == actor {
			return true
		}
	}
	return false
}

// evaluateApprovalDecision 校验一次「批准」决定是否放行(纯函数,便于测试)。
// 返回 nil=放行;否则返回拒绝原因(调用方据此落 rejected 记录并回拒)。
//   - 批准者不在环境审批人名单内 → ErrApproverNotAllowed。
//   - 批准者 == 本次晋级发起人 → ErrSelfApproval(禁止自审,职责分离)。
func evaluateApprovalDecision(actor string, info promotion.GateInfo) error {
	if !approverAllowed(actor, info.Approvers) {
		return promotion.ErrApproverNotAllowed
	}
	if info.InitiatedBy != "" && strings.TrimSpace(actor) == strings.TrimSpace(info.InitiatedBy) {
		return promotion.ErrSelfApproval
	}
	return nil
}

// makeGetEnvironmentsHandler 返回 GET /api/projects/{id}/environments(环境晋级链)。
//
// 链由流水线配置派生(见 pipeline.Environment.InChain / Gated):仅纳入 InChain 环境、
// 按数组顺序。逐环境变量不再经此端点返回 —— 它们属于「环境与凭据」配置(pipeline_settings),
// 由 GET /api/projects/{id}/pipeline/settings 提供。
func makeGetEnvironmentsHandler(store *promotion.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if store == nil {
			writeError(w, http.StatusServiceUnavailable, "internal", "晋级服务未初始化")
			return
		}
		projectID := chi.URLParam(r, "id")
		chain, err := store.GetChain(r.Context(), projectID)
		if err != nil && !errors.Is(err, promotion.ErrChainNotConfigured) {
			writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
			return
		}
		envs := chain.Environments
		if envs == nil {
			envs = []promotion.EnvStage{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"environments": envs,
		})
	}
}

// makePromoteRunHandler 返回 POST /api/runs/{id}/promote(把源运行晋级到下一环境)。
// body {targetEnvironment?}:留空取链上下一级;给定则校验恰为下一级(防跳级)。
// gated 目标会阻塞等待审批(经既有 /runs/{id}/approve|reject 决定)。
func makePromoteRunHandler(store *promotion.Store, runs run.Service, deploySvc deploy.Service, coord *approval.Coordinator,
	astore *approval.Store, vlt vault.Vault, rec audit.Recorder, mirror imageMirrorFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if store == nil || runs == nil {
			writeError(w, http.StatusServiceUnavailable, "internal", "晋级服务未初始化")
			return
		}
		runID := chi.URLParam(r, "id")
		r.Body = http.MaxBytesReader(w, r.Body, 1<<13)
		var req struct {
			TargetEnvironment string `json:"targetEnvironment"`
		}
		// 空 body 合法(取下一级)。
		_ = json.NewDecoder(r.Body).Decode(&req)
		target := strings.TrimSpace(req.TargetEnvironment)

		var gate promotion.Gate
		if coord != nil && astore != nil {
			gate = promotionGate{coord: coord, store: astore, runID: runID}
		}
		coordinator := promotion.NewCoordinator(store, runLookup{runs: runs}, gate, secretReveal{v: vlt})

		res, err := coordinator.Promote(r.Context(), runID, target, requestActor(r))
		if err != nil {
			// 审批被拒/超时/fail-closed:仍返回记录(409),非 500。
			if res != nil && promotion.IsTerminalErr(err) {
				recordAudit(r.Context(), rec, audit.Entry{
					Actor:      auditActor,
					Action:     "run.promote.reject",
					TargetType: audit.TargetRun,
					TargetID:   runID,
					Detail:     map[string]any{"targetEnvironment": res.Record.TargetEnvironment},
					IP:         clientIP(r),
				})
				// 禁自审单独以 403 + 明确 code 呈现(而非笼统的 409),让用户知道失败原因。
				if errors.Is(err, promotion.ErrSelfApproval) {
					writeError(w, http.StatusForbidden, "self_approval", "禁止自审:晋级发起人不能亲自批准")
					return
				}
				writeJSON(w, http.StatusConflict, toPromotionDTO(res.Record))
				return
			}
			writePromotionError(w, err)
			return
		}
		recordAudit(r.Context(), rec, audit.Entry{
			Actor:      auditActor,
			Action:     "run.promote",
			TargetType: audit.TargetRun,
			TargetID:   runID,
			Detail: map[string]any{
				"targetEnvironment": res.Record.TargetEnvironment,
				"fromEnvironment":   res.Record.FromEnvironment,
			},
			IP: clientIP(r),
		})

		resp := promoteResponse{promotionDTO: toPromotionDTO(res.Record)}
		// 批准即部署(GitHub 模型):把源 run 的可发布产物发到**目标环境**机群,结果记到独立晋升 run。
		// 部署为非致命副作用:无目标机/无产物/部署异常都不回滚晋级记录(记录已落 promoted),仅在响应里注明。
		if res.Approved {
			// 解析目标环境作用域变量(非 secret 明文 + secret 经保险库即时解密),注入晋升部署的目标机 .env。
			// 解密发生在批准**之后**(时序天然满足「批准前不解密」),缺失凭据 → 降级跳过(
			// 不阻断晋级),未注入的 key 在响应里披露(unresolved,绝不含明文)。
			vars, unresolved, rerr := coordinator.ResolveEnvVars(r.Context(), res.Record.ProjectID, res.Record.TargetEnvironment)
			if rerr != nil {
				writePromotionError(w, rerr)
				return
			}
			envFile := make(map[string]string, len(vars))
			for _, v := range vars {
				envFile[v.Key] = v.Value
			}
			drID, results, derr := promoteDeploy(r.Context(), runs, deploySvc, store, mirror,
				res.Record.ProjectID, runID, res.Record.TargetEnvironment, requestActor(r), envFile)
			switch {
			case derr == nil:
				resp.DeployRunID = drID
				resp.DeployTargets = make([]targetDTO, 0, len(results))
				for _, rr := range results {
					resp.DeployTargets = append(resp.DeployTargets, targetResultToDTO(rr))
				}
				if len(unresolved) > 0 {
					resp.DeployEnvUnresolved = unresolved
				}
			case errors.Is(derr, errPromoteNoTargetServers), errors.Is(derr, errPromoteNoArtifact):
				resp.DeploySkipped = derr.Error()
			default:
				resp.DeploySkipped = "deploy failed: " + derr.Error()
			}
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// promoteResponse 是 POST /api/runs/{id}/promote 的响应:晋级记录 + 可选的目标环境部署结果。
type promoteResponse struct {
	promotionDTO
	DeployRunID   string      `json:"deployRunId,omitempty"`
	DeployTargets []targetDTO `json:"deployTargets,omitempty"`
	DeploySkipped string      `json:"deploySkipped,omitempty"`
	// DeployEnvUnresolved 是「目标环境作用域变量」中未能注入的 key(secret 凭据缺失 / 保险库未配)。
	// 绝不含明文;仅披露 key 供告警 / 排障。
	DeployEnvUnresolved []string `json:"deployEnvUnresolved,omitempty"`
}

// 晋级部署的「跳过」原因(非致命:晋级记录已落,仅未执行部署)。
var (
	errPromoteNoTargetServers = errors.New("no target servers for environment")
	errPromoteNoArtifact      = errors.New("no deployable artifact in source run")
)

// pickDeployableArtifactID 从源 run 产物中选一个可发布产物 id(优先 dist/jar/archive/image;否则首个)。
func pickDeployableArtifactID(arts []run.Artifact) string {
	priority := []string{run.ArtifactDist, run.ArtifactJar, run.ArtifactArchive, run.ArtifactImage}
	for _, t := range priority {
		for i := range arts {
			if arts[i].Type == t {
				return arts[i].ID
			}
		}
	}
	if len(arts) > 0 {
		return arts[0].ID
	}
	return ""
}

// promoteDeploy 晋级批准后把源 run 的可发布产物部署到目标环境机群(批准即部署)。
// 结果记到一条独立晋升 run(CreatePromotionDeployRun),不覆盖源 run 的部署记录。
// envFile 是已解密的目标环境作用域变量(明文 K=V),非空时注入目标机 .env。
// mirror 非 nil 且产物为 image 时,先按需把镜像镜像到目标环境仓库,并让目标机改拉目标仓库引用。
// 无目标机 / 无可用产物 → 明确错误(调用方据此在响应里标注 skipped,不阻断晋级)。
func promoteDeploy(ctx context.Context, runs run.Service, deploySvc deploy.Service, pstore *promotion.Store, mirror imageMirrorFunc,
	projectID, sourceRunID, env, actor string, envFile map[string]string) (string, []deploy.TargetResult, error) {
	if deploySvc == nil || pstore == nil || runs == nil {
		return "", nil, errors.New("deploy service uninitialized")
	}
	servers, err := pstore.EnvironmentTargets(ctx, projectID, env)
	if err != nil {
		return "", nil, err
	}
	if len(servers) == 0 {
		return "", nil, errPromoteNoTargetServers
	}
	// 取目标环境完整配置(含逐环境部署参数:容器名 / 端口 / 运行参数),透传到部署 Config,
	// 使该环境所有目标机统一使用同一套 docker run 参数(单机能 dev/prod 共存、多机 fleet 同名部署)。
	envCfg, err := pstore.GetEnvironment(ctx, projectID, env)
	if err != nil {
		return "", nil, err
	}
	arts, err := runs.ListArtifacts(ctx, sourceRunID)
	if err != nil {
		return "", nil, err
	}
	artID := pickDeployableArtifactID(arts)
	if artID == "" {
		return "", nil, errPromoteNoArtifact
	}
	// 取该产物全文(拿 Type + Reference);理论存在(刚由 pickDeployableArtifactID 选出)。
	var art *run.Artifact
	for i := range arts {
		if arts[i].ID == artID {
			a := arts[i]
			art = &a
			break
		}
	}
	recRunID, err := runs.CreatePromotionDeployRun(ctx, projectID, sourceRunID, env, actor)
	if err != nil {
		return "", nil, err
	}
	// 晋升镜像(FR-7 闭环):image 产物按需镜像到目标环境仓库,目标机改拉目标仓库引用。
	// 镜像失败 → 上抛(调用方记 DeploySkipped,不回滚已落的晋级记录)。
	imageRef := ""
	if art != nil && art.Type == run.ArtifactImage && mirror != nil {
		deployRef, merr := mirror(ctx, projectID, env, art.Reference)
		if merr != nil {
			return recRunID, nil, fmt.Errorf("镜像到目标仓库失败:%v", merr)
		}
		imageRef = deployRef
	}
	results, err := deploySvc.Deploy(ctx, deploy.DeployInput{
		RunID:       sourceRunID,
		ArtifactID:  artID,
		ServerIDs:   servers,
		RecordRunID: recRunID,
		EnvFile:     envFile,
		ImageRef:    imageRef,
		Config:      promotion.DeployConfigForEnvironment(envCfg),
	})
	if err != nil {
		return recRunID, nil, err
	}
	return recRunID, results, nil
}

// makeListRunPromotionsHandler 返回 GET /api/runs/{id}/promotions(某运行的晋级历史)。
func makeListRunPromotionsHandler(store *promotion.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if store == nil {
			writeError(w, http.StatusServiceUnavailable, "internal", "晋级服务未初始化")
			return
		}
		recs, err := store.ListRecordsForRun(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": toPromotionDTOs(recs)})
	}
}

// makeListProjectPromotionsHandler 返回 GET /api/projects/{id}/promotions(项目晋级历史,最近在前)。
func makeListProjectPromotionsHandler(store *promotion.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if store == nil {
			writeError(w, http.StatusServiceUnavailable, "internal", "晋级服务未初始化")
			return
		}
		recs, err := store.ListRecordsForProject(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": toPromotionDTOs(recs)})
	}
}

// promotionDTO 是晋级记录的对外 JSON 形状(camelCase)。
type promotionDTO struct {
	ID                string `json:"id"`
	ProjectID         string `json:"projectId"`
	SourceRunID       string `json:"sourceRunId"`
	FromEnvironment   string `json:"fromEnvironment"`
	TargetEnvironment string `json:"targetEnvironment"`
	Status            string `json:"status"`
	PromotedBy        string `json:"promotedBy"`
	CreatedAt         string `json:"createdAt"`
	DecidedAt         string `json:"decidedAt"`
}

func toPromotionDTO(r promotion.Record) promotionDTO {
	return promotionDTO{
		ID:                r.ID,
		ProjectID:         r.ProjectID,
		SourceRunID:       r.SourceRunID,
		FromEnvironment:   r.FromEnvironment,
		TargetEnvironment: r.TargetEnvironment,
		Status:            r.Status,
		PromotedBy:        r.PromotedBy,
		CreatedAt:         r.CreatedAt,
		DecidedAt:         r.DecidedAt,
	}
}

func toPromotionDTOs(recs []promotion.Record) []promotionDTO {
	out := make([]promotionDTO, 0, len(recs))
	for _, r := range recs {
		out = append(out, toPromotionDTO(r))
	}
	return out
}

// writePromotionError 把晋级领域错误映射为 HTTP 状态码(错误体绝无敏感数据)。
func writePromotionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, promotion.ErrRunNotFound):
		writeError(w, http.StatusNotFound, "not_found", "运行不存在")
	case errors.Is(err, promotion.ErrProjectNotFound):
		writeError(w, http.StatusNotFound, "not_found", "项目不存在")
	case errors.Is(err, promotion.ErrChainNotConfigured):
		writeError(w, http.StatusUnprocessableEntity, "chain_not_configured", "尚未配置环境链")
	case errors.Is(err, promotion.ErrEmptyChain):
		writeError(w, http.StatusUnprocessableEntity, "empty_chain", "环境链不能为空")
	case errors.Is(err, promotion.ErrDuplicateEnv):
		writeError(w, http.StatusUnprocessableEntity, "duplicate_env", "环境链含重复环境名")
	case errors.Is(err, promotion.ErrInvalidEnvName):
		writeError(w, http.StatusUnprocessableEntity, "invalid_env", "环境名非法")
	case errors.Is(err, promotion.ErrUnknownEnv):
		writeError(w, http.StatusUnprocessableEntity, "unknown_env", "目标环境不在链上")
	case errors.Is(err, promotion.ErrRunNotSuccessful):
		writeError(w, http.StatusConflict, "run_not_successful", "源运行未成功,不可晋级")
	case errors.Is(err, promotion.ErrSkipEnv):
		writeError(w, http.StatusConflict, "skip_env", "不可跳级:只能晋级到下一环境")
	case errors.Is(err, promotion.ErrAlreadyAtTop):
		writeError(w, http.StatusConflict, "already_at_top", "已晋级到链尾环境")
	case errors.Is(err, promotion.ErrAlreadyPromoted):
		writeError(w, http.StatusConflict, "already_promoted", "该运行已晋级到该环境")
	case errors.Is(err, promotion.ErrGateRejected):
		writeError(w, http.StatusConflict, "gate_rejected", "晋级审批被拒绝")
	case errors.Is(err, promotion.ErrApproverNotAllowed):
		writeError(w, http.StatusForbidden, "approver_not_allowed", "批准者不在该环境的审批人名单内")
	case errors.Is(err, promotion.ErrSelfApproval):
		writeError(w, http.StatusForbidden, "self_approval", "禁止自审:晋级发起人不能亲自批准")
	default:
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
	}
}
