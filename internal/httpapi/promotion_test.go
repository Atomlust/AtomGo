package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/atomlust/atomgo/internal/approval"
	"github.com/atomlust/atomgo/internal/auth"
	"github.com/atomlust/atomgo/internal/pipeline"
	"github.com/atomlust/atomgo/internal/project"
	"github.com/atomlust/atomgo/internal/promotion"
	"github.com/atomlust/atomgo/internal/run"
	"github.com/atomlust/atomgo/internal/vault"
)

// setupPromotionServer 构造带 auth + project + run + approvals + promotion + (stub)deploy 的测试 server。
// 返回的 *stubDeploy 记录最近一次晋级部署的入参(供断言)。
func setupPromotionServer(t *testing.T, mirror imageMirrorFunc) (*httptest.Server, *http.Client, string, string, run.Service, *stubDeploy, *auth.Service) {
	t.Helper()
	st := testStoreAuth(t)
	svc := auth.NewService(st.DB, nil)
	if err := svc.Bootstrap("admin", "testpass"); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	v := vault.New(st.DB, testMasterKey())
	psvc := project.New(st.DB, v, stubProber{branch: "main"})
	rsvc := run.New(st.DB)
	coord := approval.New()
	astore := approval.NewStore(st.DB)
	// 构建/部署配置(环境与晋级链单一真相源)与晋级 store 共用同一 settings 服务。
	setsvc := pipeline.NewSettingsService(st.DB, v)
	pstore := promotion.NewStore(st.DB, setsvc)
	pool := run.NewWorkerPool(rsvc)
	pool.Start()
	t.Cleanup(func() { pool.Stop(context.Background()) })

	dep := &stubDeploy{}
	srv := httptest.NewServer(New(testWebFSAuth(), svc,
		WithVault(v), WithProjects(psvc), WithRuns(rsvc, pool),
		WithApprovals(coord, astore), WithPipelineSettings(setsvc), WithPromotion(pstore),
		WithImageMirror(mirror), WithDeploy(dep)))
	t.Cleanup(srv.Close)

	client := newTestClient(t)
	csrf := loginWithClient(t, client, srv.URL)

	credID := newGitCred(t, client, srv.URL, csrf, "ghp_promo")
	resp := doJSON(t, client, http.MethodPost, srv.URL+"/api/projects", csrf,
		`{"name":"acme","repoUrl":"https://gitee.com/acme/shop.git","credentialId":"`+credID+`"}`)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var p map[string]any
	_ = json.Unmarshal(raw, &p)
	projID, _ := p["id"].(string)
	if projID == "" {
		t.Fatalf("create project failed: %s", raw)
	}
	return srv, client, csrf, projID, rsvc, dep, svc
}

// seedSuccessRun 直接落一个 success 运行(绕过 worker;晋级只需 status=success)。
func seedSuccessRun(t *testing.T, rsvc run.Service, projID string) string {
	t.Helper()
	r, err := rsvc.Create(context.Background(), projID, run.Trigger{Type: run.TriggerManual})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	// 推进到 success(queued→running→success)以满足晋级前置。
	if err := rsvc.MarkWaitingApproval(context.Background(), r.ID); err == nil {
		_ = rsvc.ResumeFromApproval(context.Background(), r.ID)
	}
	// 强制置 success:复用 SetDeployTerminal(直接覆盖终态)。
	if err := rsvc.SetDeployTerminal(context.Background(), r.ID, run.StatusSuccess); err != nil {
		t.Fatalf("force success: %v", err)
	}
	return r.ID
}

func TestEnvironmentsConfigAndPromoteNonGated(t *testing.T) {
	srv, client, csrf, projID, rsvc, _, _ := setupPromotionServer(t, nil)

	// 配置链 dev→prod(prod gated)。链现统一在「环境与凭据」(pipeline_settings)配置,
	// 经真实 PUT /pipeline/settings 落库,GET /environments 再派生链。
	settingsBody := `{"build":{"model":"dockerfile","dockerfilePath":"Dockerfile","artifactType":"image","vars":[],"cache":{"enabled":false,"paths":[]}},` +
		`"environments":[` +
		`{"id":"","name":"dev","inChain":true,"gated":false,"targetServerIds":["s1"],"envVars":[],"imageRegistry":{"type":"","url":"","credentialId":""}},` +
		`{"id":"","name":"prod","inChain":true,"gated":true,"targetServerIds":["s1"],"envVars":[],"imageRegistry":{"type":"","url":"","credentialId":""}}` +
		`],"steps":[]}`
	resp := doJSON(t, client, http.MethodPut, srv.URL+"/api/projects/"+projID+"/pipeline/settings", csrf, settingsBody)
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT pipeline/settings = %d: %s", resp.StatusCode, raw)
	}
	resp.Body.Close()

	// GET /environments 派生链(仅 InChain 环境、按数组顺序)。
	gresp, gerr := client.Get(srv.URL + "/api/projects/" + projID + "/environments")
	if gerr != nil {
		t.Fatalf("GET environments: %v", gerr)
	}
	defer gresp.Body.Close()
	graw, _ := io.ReadAll(gresp.Body)
	var got struct {
		Environments []map[string]any `json:"environments"`
	}
	_ = json.Unmarshal(graw, &got)
	if len(got.Environments) != 2 {
		t.Fatalf("environments len = %d: %s", len(got.Environments), graw)
	}

	// 晋级 success 运行到 dev(非 gated → 同步 promoted)。
	runID := seedSuccessRun(t, rsvc, projID)
	presp := doJSON(t, client, http.MethodPost, srv.URL+"/api/runs/"+runID+"/promote", csrf, `{"targetEnvironment":"dev"}`)
	defer presp.Body.Close()
	praw, _ := io.ReadAll(presp.Body)
	if presp.StatusCode != http.StatusOK {
		t.Fatalf("POST promote = %d: %s", presp.StatusCode, praw)
	}
	var rec map[string]any
	_ = json.Unmarshal(praw, &rec)
	if rec["status"] != "promoted" || rec["targetEnvironment"] != "dev" {
		t.Fatalf("promote record wrong: %s", praw)
	}

	// 历史列表含一条。
	hresp, herr := client.Get(srv.URL + "/api/runs/" + runID + "/promotions")
	if herr != nil {
		t.Fatalf("GET promotions: %v", herr)
	}
	defer hresp.Body.Close()
	hraw, _ := io.ReadAll(hresp.Body)
	var hist struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal(hraw, &hist)
	if len(hist.Items) != 1 {
		t.Fatalf("promotion history len = %d: %s", len(hist.Items), hraw)
	}
}

func TestPromoteGatedWaitsForApproval(t *testing.T) {
	srv, client, csrf, projID, rsvc, _, asvc := setupPromotionServer(t, nil)

	// 禁自审:批准者须为不同于发起人(admin)的第二个账号,否则门会以 self_approval 拒绝。
	if _, err := asvc.CreateUser("approver", "approverpass", auth.RoleAdmin); err != nil {
		t.Fatalf("create approver: %v", err)
	}
	approverClient, approverCSRF := loginAs(t, srv.URL, "approver", "approverpass")

	// 链只有 prod(gated),统一在 pipeline_settings 配置。
	settingsBody := `{"build":{"model":"dockerfile","dockerfilePath":"Dockerfile","artifactType":"image","vars":[],"cache":{"enabled":false,"paths":[]}},` +
		`"environments":[` +
		`{"id":"","name":"prod","inChain":true,"gated":true,"targetServerIds":["s1"],"envVars":[],"imageRegistry":{"type":"","url":"","credentialId":""}}` +
		`],"steps":[]}`
	resp := doJSON(t, client, http.MethodPut, srv.URL+"/api/projects/"+projID+"/pipeline/settings", csrf, settingsBody)
	resp.Body.Close()

	runID := seedSuccessRun(t, rsvc, projID)

	// 并发:发起晋级(阻塞等待审批),另一协程经 /approve 批准。
	done := make(chan int, 1)
	go func() {
		presp := doJSON(t, client, http.MethodPost, srv.URL+"/api/runs/"+runID+"/promote", csrf, `{"targetEnvironment":"prod"}`)
		io.Copy(io.Discard, presp.Body)
		presp.Body.Close()
		done <- presp.StatusCode
	}()

	// 轮询直到该晋级门进入等待,再由**另一用户**批准(stageId = "promote:prod")。
	approveDeadline := pollApprove(t, approverClient, srv.URL, approverCSRF, runID, "promote:prod")
	if !approveDeadline {
		t.Fatalf("approve never succeeded (gate not waiting)")
	}

	if code := <-done; code != http.StatusOK {
		t.Fatalf("gated promote final status = %d; want 200", code)
	}
}

// TestPromoteSelfApprovalForbidden 验证禁自审:发起人(admin)亲自批准 → 409 self_approval。
func TestPromoteSelfApprovalForbidden(t *testing.T) {
	srv, client, csrf, projID, rsvc, _, _ := setupPromotionServer(t, nil)

	settingsBody := `{"build":{"model":"dockerfile","dockerfilePath":"Dockerfile","artifactType":"image","vars":[],"cache":{"enabled":false,"paths":[]}},` +
		`"environments":[` +
		`{"id":"","name":"prod","inChain":true,"gated":true,"targetServerIds":["s1"],"envVars":[],"imageRegistry":{"type":"","url":"","credentialId":""}}` +
		`],"steps":[]}`
	resp := doJSON(t, client, http.MethodPut, srv.URL+"/api/projects/"+projID+"/pipeline/settings", csrf, settingsBody)
	resp.Body.Close()

	runID := seedSuccessRun(t, rsvc, projID)

	done := make(chan int, 1)
	go func() {
		presp := doJSON(t, client, http.MethodPost, srv.URL+"/api/runs/"+runID+"/promote", csrf, `{"targetEnvironment":"prod"}`)
		raw, _ := io.ReadAll(presp.Body)
		presp.Body.Close()
		if presp.StatusCode == http.StatusForbidden {
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			if errObj, ok := body["error"].(map[string]any); ok && errObj["code"] == "self_approval" {
				done <- http.StatusForbidden
				return
			}
		}
		done <- presp.StatusCode
	}()

	// 发起人(admin)亲自批准 → 门以 self_approval 拒绝 → promote 返回 403。
	if !pollApprove(t, client, srv.URL, csrf, runID, "promote:prod") {
		t.Fatalf("approve endpoint did not accept the decision")
	}
	if code := <-done; code != http.StatusForbidden {
		t.Fatalf("self-approval final status = %d; want 403", code)
	}
}

// pollApprove 轮询投递 approve 直到成功(门进入等待)或超时。
func pollApprove(t *testing.T, client *http.Client, srvURL, csrf, runID, stageID string) bool {
	t.Helper()
	for i := 0; i < 200; i++ {
		resp := doJSON(t, client, http.MethodPost, srvURL+"/api/runs/"+runID+"/approve", csrf,
			`{"stageId":"`+stageID+`"}`)
		code := resp.StatusCode
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if code == http.StatusOK {
			return true
		}
		// 409 not_waiting:门尚未进入等待,稍后重试。
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// TestPromoteDeploysSourceArtifactToTargetEnv 验「批准即部署」:晋级到非 gated 环境时,
// 把源 run 的可发布产物部署到**目标环境**的目标机,结果记到独立晋升 run(不覆盖源 run)。
func TestPromoteDeploysSourceArtifactToTargetEnv(t *testing.T) {
	srv, client, csrf, projID, rsvc, dep, _ := setupPromotionServer(t, nil)

	// 非 gated 的 prod(带目标机 srv-1)→ 同步晋级即触发部署。
	// 目标环境作用域变量:一条明文(APP_ENV) + 一条 secret(DB_PASS,经保险库解密)。
	dbCredID := newGitCred(t, client, srv.URL, csrf, "s3cr3t_promote_db")
	settingsBody := `{"build":{"model":"dockerfile","dockerfilePath":"Dockerfile","artifactType":"dist","vars":[],"cache":{"enabled":false,"paths":[]}},` +
		`"environments":[` +
		`{"id":"","name":"prod","inChain":true,"gated":false,"targetServerIds":["srv-1"],` +
		`"envVars":[` +
		`{"key":"APP_ENV","secret":false,"value":"prod"},` +
		`{"key":"DB_PASS","secret":true,"credentialId":"` + dbCredID + `"}` +
		`],"imageRegistry":{"type":"","url":"","credentialId":""}}` +
		`],"steps":[]}`
	resp := doJSON(t, client, http.MethodPut, srv.URL+"/api/projects/"+projID+"/pipeline/settings", csrf, settingsBody)
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT pipeline/settings = %d: %s", resp.StatusCode, raw)
	}
	resp.Body.Close()

	runID := seedSuccessRun(t, rsvc, projID)
	art, err := rsvc.AddArtifact(context.Background(), run.Artifact{
		RunID: runID, Type: run.ArtifactDist, Name: "app", Reference: "dist/app",
	})
	if err != nil {
		t.Fatalf("add artifact: %v", err)
	}

	presp := doJSON(t, client, http.MethodPost, srv.URL+"/api/runs/"+runID+"/promote", csrf, `{"targetEnvironment":"prod"}`)
	defer presp.Body.Close()
	praw, _ := io.ReadAll(presp.Body)
	if presp.StatusCode != http.StatusOK {
		t.Fatalf("POST promote = %d: %s", presp.StatusCode, praw)
	}
	var body struct {
		Status              string   `json:"status"`
		DeployRunID         string   `json:"deployRunId"`
		DeploySkipped       string   `json:"deploySkipped"`
		DeployEnvUnresolved []string `json:"deployEnvUnresolved"`
		DeployTargets       []struct {
			ServerID string `json:"serverId"`
			Status   string `json:"status"`
		} `json:"deployTargets"`
	}
	if err := json.Unmarshal(praw, &body); err != nil {
		t.Fatalf("decode promote response: %v", err)
	}
	if body.Status != "promoted" {
		t.Fatalf("status = %q; want promoted (%s)", body.Status, praw)
	}
	if body.DeploySkipped != "" {
		t.Fatalf("unexpected deploySkipped: %s", body.DeploySkipped)
	}
	if len(body.DeployTargets) != 1 || body.DeployTargets[0].ServerID != "srv-1" {
		t.Fatalf("deployTargets = %+v; want one srv-1", body.DeployTargets)
	}
	if body.DeployRunID == "" || body.DeployRunID == runID {
		t.Fatalf("deployRunId = %q; want a fresh run id != source", body.DeployRunID)
	}
	// 部署被以正确入参触发:源 run 产物 → 目标环境机群,结果记到独立晋升 run。
	if dep.lastInput.RunID != runID {
		t.Fatalf("deploy RunID = %q; want source %q", dep.lastInput.RunID, runID)
	}
	if dep.lastInput.ArtifactID != art.ID {
		t.Fatalf("deploy ArtifactID = %q; want %q", dep.lastInput.ArtifactID, art.ID)
	}
	if len(dep.lastInput.ServerIDs) != 1 || dep.lastInput.ServerIDs[0] != "srv-1" {
		t.Fatalf("deploy ServerIDs = %v; want [srv-1]", dep.lastInput.ServerIDs)
	}
	if dep.lastInput.RecordRunID != body.DeployRunID {
		t.Fatalf("deploy RecordRunID = %q; want %q", dep.lastInput.RecordRunID, body.DeployRunID)
	}
	// 目标环境作用域变量被解析并注入部署入参:明文原样、secret 经保险库解密为明文。
	if len(dep.lastInput.EnvFile) != 2 {
		t.Fatalf("deploy EnvFile = %+v; want 2 entries (plaintext + resolved secret)", dep.lastInput.EnvFile)
	}
	if dep.lastInput.EnvFile["APP_ENV"] != "prod" {
		t.Fatalf("EnvFile[APP_ENV] = %q; want prod", dep.lastInput.EnvFile["APP_ENV"])
	}
	if dep.lastInput.EnvFile["DB_PASS"] != "s3cr3t_promote_db" {
		t.Fatalf("EnvFile[DB_PASS] = %q; want resolved secret s3cr3t_promote_db", dep.lastInput.EnvFile["DB_PASS"])
	}
	// 所有变量均成功注入,无未解析项。
	if len(body.DeployEnvUnresolved) != 0 {
		t.Fatalf("DeployEnvUnresolved = %v; want empty", body.DeployEnvUnresolved)
	}
}

// TestPromoteMirrorsImageToTargetRegistry 验证:image 产物晋级且注入了镜像钩子时,
// 钩子收到源引用 + 目标环境,且目标机改拉钩子返回的目标仓库引用(DeployInput.ImageRef)。
func TestPromoteMirrorsImageToTargetRegistry(t *testing.T) {
	const targetRef = "harbor-prod.acme.com/acme/shop-api:abc1234"
	var gotSrc, gotEnv string
	mirror := func(_ context.Context, _, targetEnv, sourceRef string) (string, error) {
		gotSrc, gotEnv = sourceRef, targetEnv
		return targetRef, nil
	}
	srv, client, csrf, projID, rsvc, dep, _ := setupPromotionServer(t, mirror)

	settingsBody := `{"build":{"model":"dockerfile","dockerfilePath":"Dockerfile","artifactType":"image","vars":[],"cache":{"enabled":false,"paths":[]}},` +
		`"environments":[` +
		`{"id":"","name":"prod","inChain":true,"gated":false,"targetServerIds":["srv-1"],"envVars":[],` +
		`"imageRegistry":{"type":"harbor","url":"https://harbor-prod.acme.com","credentialId":""}}` +
		`],"steps":[]}`
	resp := doJSON(t, client, http.MethodPut, srv.URL+"/api/projects/"+projID+"/pipeline/settings", csrf, settingsBody)
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT pipeline/settings = %d: %s", resp.StatusCode, raw)
	}
	resp.Body.Close()

	runID := seedSuccessRun(t, rsvc, projID)
	srcRef := "harbor-dev.acme.com/acme/shop-api:abc1234"
	if _, err := rsvc.AddArtifact(context.Background(), run.Artifact{
		RunID: runID, Type: run.ArtifactImage, Name: "shop-api", Reference: srcRef,
	}); err != nil {
		t.Fatalf("add artifact: %v", err)
	}

	presp := doJSON(t, client, http.MethodPost, srv.URL+"/api/runs/"+runID+"/promote", csrf, `{"targetEnvironment":"prod"}`)
	defer presp.Body.Close()
	praw, _ := io.ReadAll(presp.Body)
	if presp.StatusCode != http.StatusOK {
		t.Fatalf("POST promote = %d: %s", presp.StatusCode, praw)
	}
	var body struct {
		Status        string `json:"status"`
		DeploySkipped string `json:"deploySkipped"`
	}
	_ = json.Unmarshal(praw, &body)
	if body.Status != "promoted" || body.DeploySkipped != "" {
		t.Fatalf("promote status=%q skipped=%q (%s)", body.Status, body.DeploySkipped, praw)
	}
	if gotEnv != "prod" || gotSrc != srcRef {
		t.Fatalf("mirror called with env=%q src=%q; want prod / %q", gotEnv, gotSrc, srcRef)
	}
	if dep.lastInput.ImageRef != targetRef {
		t.Fatalf("deploy ImageRef = %q; want %q (target registry)", dep.lastInput.ImageRef, targetRef)
	}
}

// TestPromoteMirrorFailureSkipsDeploy 验证:镜像钩子失败 → 晋级记录仍 promoted,但部署被跳过(DeploySkipped)。
func TestPromoteMirrorFailureSkipsDeploy(t *testing.T) {
	mirror := func(_ context.Context, _, _, _ string) (string, error) {
		return "", errors.New("目标仓库登录失败")
	}
	srv, client, csrf, projID, rsvc, _, _ := setupPromotionServer(t, mirror)

	settingsBody := `{"build":{"model":"dockerfile","dockerfilePath":"Dockerfile","artifactType":"image","vars":[],"cache":{"enabled":false,"paths":[]}},` +
		`"environments":[` +
		`{"id":"","name":"prod","inChain":true,"gated":false,"targetServerIds":["srv-1"],"envVars":[],` +
		`"imageRegistry":{"type":"harbor","url":"https://harbor-prod.acme.com","credentialId":""}}` +
		`],"steps":[]}`
	resp := doJSON(t, client, http.MethodPut, srv.URL+"/api/projects/"+projID+"/pipeline/settings", csrf, settingsBody)
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT pipeline/settings = %d: %s", resp.StatusCode, raw)
	}
	resp.Body.Close()

	runID := seedSuccessRun(t, rsvc, projID)
	if _, err := rsvc.AddArtifact(context.Background(), run.Artifact{
		RunID: runID, Type: run.ArtifactImage, Name: "shop-api", Reference: "harbor-dev.acme.com/app:abc1234",
	}); err != nil {
		t.Fatalf("add artifact: %v", err)
	}

	presp := doJSON(t, client, http.MethodPost, srv.URL+"/api/runs/"+runID+"/promote", csrf, `{"targetEnvironment":"prod"}`)
	defer presp.Body.Close()
	praw, _ := io.ReadAll(presp.Body)
	if presp.StatusCode != http.StatusOK {
		t.Fatalf("POST promote = %d: %s", presp.StatusCode, praw)
	}
	var body struct {
		Status        string `json:"status"`
		DeploySkipped string `json:"deploySkipped"`
	}
	_ = json.Unmarshal(praw, &body)
	if body.Status != "promoted" {
		t.Fatalf("status = %q; want promoted (mirror failure must not roll back promotion)", body.Status)
	}
	if body.DeploySkipped == "" {
		t.Fatalf("expected DeploySkipped on mirror failure, got empty (%s)", praw)
	}
}
