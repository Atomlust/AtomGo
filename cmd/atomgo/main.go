// Command atomgo 是平台入口:加载配置 → 打开存储(应用迁移)→ 装配 HTTP → 启动。
// 整个平台为单静态二进制(前端经 go:embed 内嵌),无前后端分离部署。
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	root "github.com/atomlust/atomgo"
	"github.com/atomlust/atomgo/internal/ai"
	"github.com/atomlust/atomgo/internal/anomaly"
	"github.com/atomlust/atomgo/internal/approval"
	"github.com/atomlust/atomgo/internal/artifactstore"
	"github.com/atomlust/atomgo/internal/audit"
	"github.com/atomlust/atomgo/internal/livecache"
	"github.com/atomlust/atomgo/internal/auth"
	"github.com/atomlust/atomgo/internal/build"
	"github.com/atomlust/atomgo/internal/buildcache"
	"github.com/atomlust/atomgo/internal/chain"
	"github.com/atomlust/atomgo/internal/config"
	"github.com/atomlust/atomgo/internal/cron"
	"github.com/atomlust/atomgo/internal/dagrun"
	"github.com/atomlust/atomgo/internal/deploy"
	"github.com/atomlust/atomgo/internal/environments"
	"github.com/atomlust/atomgo/internal/httpapi"
	"github.com/atomlust/atomgo/internal/library"
	"github.com/atomlust/atomgo/internal/mask"
	"github.com/atomlust/atomgo/internal/metrics"
	"github.com/atomlust/atomgo/internal/notify"
	"github.com/atomlust/atomgo/internal/oauth"
	"github.com/atomlust/atomgo/internal/onboarding"
	"github.com/atomlust/atomgo/internal/opschat"
	"github.com/atomlust/atomgo/internal/pacloader"
	"github.com/atomlust/atomgo/internal/pipeline"
	"github.com/atomlust/atomgo/internal/project"
	"github.com/atomlust/atomgo/internal/promotion"
	"github.com/atomlust/atomgo/internal/proxy"
	"github.com/atomlust/atomgo/internal/prstatus"
	"github.com/atomlust/atomgo/internal/repocache"
	"github.com/atomlust/atomgo/internal/retention"
	"github.com/atomlust/atomgo/internal/run"
	"github.com/atomlust/atomgo/internal/runner"
	"github.com/atomlust/atomgo/internal/settings"
	"github.com/atomlust/atomgo/internal/store"
	"github.com/atomlust/atomgo/internal/target"
	"github.com/atomlust/atomgo/internal/trigger"
	"github.com/atomlust/atomgo/internal/vault"
	"github.com/atomlust/atomgo/internal/version"
)

func main() {
	// --version / -v:在任何配置加载、存储打开等副作用之前处理,纯查询即打印退出。
	for _, a := range os.Args[1:] {
		if a == "--version" || a == "-version" || a == "-v" {
			fmt.Println(version.String()) // 走 stdout(惯例),便于脚本捕获
			return
		}
	}

	// 加载 atomgo.env(若有):首次启动向导固化的数据库等配置在此注入,确保重启后生效。
	// 进程已有的显式环境变量优先于文件(文件仅作兜底,不覆盖已设值)。
	exeDir, _ := executableDir()
	envFilePath := resolveEnvFilePath(exeDir)
	loadEnvFile(envFilePath)

	cfg := config.Load()

	dbDriver, dbDSN, err := cfg.StoreConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	st, err := store.OpenWithConfig(store.OpenConfig{Driver: dbDriver, DSN: dbDSN})
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer func() { _ = st.Close() }()

	// 启动期 fail-fast:内嵌前端必须含 index.html,否则运行期白屏(空/错误 web/dist 也能编过)。
	webFS := root.WebFS()
	if _, err := fs.Stat(webFS, "index.html"); err != nil {
		log.Fatalf("embedded web/dist 缺少 index.html(前端未正确构建?): %v", err)
	}

	if dbDriver == "mysql" {
		log.Printf("data db: mysql (%s)", st.Dialect)
	} else if abs, err := filepath.Abs(dbDSN); err == nil {
		log.Printf("data db: %s", abs)
	}

	// 装配认证服务(nil clock → RealClock)并执行首次启动管理员引导。
	authSvc := auth.NewService(st.DB, nil)
	if err := authSvc.Bootstrap(cfg.AdminUsername, cfg.AdminPassword); err != nil {
		log.Fatalf("auth bootstrap: %v", err)
	}
	// 判定是否处于「待初始化」(admin_user 为空):用于启动横幅给出向导提示。
	setupPending := false
	if nsup, nerr := authSvc.NeedsSetup(context.Background()); nerr != nil {
		log.Printf("[auth] 警告:启动检查管理员状态失败:%v", nerr)
	} else {
		setupPending = nsup
	}
	// 启动时清理 DB 中遗留的过期会话(惰性删除可能遗漏)。
	if n, err := authSvc.PurgeExpiredSessions(); err != nil {
		log.Printf("[auth] 警告:启动清理过期会话失败:%v", err)
	} else if n > 0 {
		log.Printf("[auth] 启动清理过期会话:%d 条", n)
	}

	// 装配凭据保险库(Story 1.3)。master key 缺失 → 未配置态(不 panic,平台仍启动);
	// 解码/长度错误 → 视为配置错误,提示但仍以未配置态继续(/healthz 保持可用)。
	// masterKeyFilePath:首次启动 Web 初始化向导生成 key 的落盘位置。
	// env 显式指定 ATOMGO_MASTER_KEY_FILE 时优先;否则 sqlite 落库文件同目录的
	// master.key(mysql/pg 无自然数据目录,落当前工作目录)。
	masterKeyFilePath := strings.TrimSpace(os.Getenv("ATOMGO_MASTER_KEY_FILE"))
	if masterKeyFilePath == "" {
		if dbDriver == "sqlite" {
			if dir := filepath.Dir(dbDSN); dir != "" && dir != "." {
				masterKeyFilePath = filepath.Join(dir, "master.key")
			} else {
				masterKeyFilePath = "master.key"
			}
		} else {
			// 非 SQLite 无自然数据目录:落可执行文件同目录,与 atomgo.env 一致,便于持久化与重载。
			masterKeyFilePath = filepath.Join(exeDir, "master.key")
		}
	}

	var masterKey *[config.MasterKeyLen]byte
	switch key, err := config.LoadMasterKey(); {
	case err == nil:
		masterKey = key
		log.Printf("[vault] master key 已加载(env),凭据保险库可用")
	case errors.Is(err, config.ErrNoMasterKey):
		// 回退:首次启动 Web 向导生成的 key 落盘在 master.key,重启后经此重新加载。
		if key, ferr := loadMasterKeyFile(masterKeyFilePath); ferr == nil {
			masterKey = key
			log.Printf("[vault] master key 已加载(%s),凭据保险库可用", masterKeyFilePath)
		} else if !errors.Is(ferr, os.ErrNotExist) {
			log.Printf("[vault] 警告:读取 master key 文件 %s 失败(%v),凭据保险库不可用。", masterKeyFilePath, ferr)
		} else {
			log.Printf("[vault] 未配置 master key:可在首次启动的 Web 初始化向导中生成(或设 ATOMGO_MASTER_KEY),凭据保险库暂不可用。")
		}
	default:
		// 不打印 err 之外的任何内容;err 本身不含 key 值。
		log.Printf("[vault] 警告:master key 配置无效(%v),凭据保险库不可用。", err)
	}
	credVault := vault.New(st.DB, masterKey)

	// 系统设置服务(DB 键值 + env 覆盖;当前承载 public_url,供审批链接/PR 状态回写惰性读取)。
	settingsSvc := settings.New(st.DB)
	publicURLFn := func() string { return settingsSvc.PublicURL(context.Background()) }

	// 首次启动 Web 初始化向导服务:无管理员时挂 /api/setup/* 并对业务请求设门。
	// 构造失败(如 crypto/rand 不可用)不致命:记为无向导,平台其余功能照常。
	var setupOpts []httpapi.Option
	if setupSvc, serr := httpapi.NewSetupServiceForWiring(authSvc, credVault, settingsSvc, masterKeyFilePath, envFilePath, selfRestart); serr != nil {
		log.Printf("[setup] 警告:初始化向导服务构造失败(%v),Web 初始化向导不可用", serr)
	} else {
		setupOpts = append(setupOpts, httpapi.WithSetup(setupSvc))
	}

	// 审批链接签名器(「从通知直接审批」):用 master key 做 HMAC 签发/校验审批 token。
	// master key 缺失 → 禁用态(不签链接、公开页不可用),功能优雅关闭。
	var approvalSignerKey []byte
	if masterKey != nil {
		approvalSignerKey = masterKey[:]
	}
	approvalSigner := approval.NewSigner(approvalSignerKey)

	// 装配项目服务(Story 2.1):经 store 触库、经 vault 取凭据做 ls-remote 校验(进程内,用完即弃)。
	// prober 传 nil → 使用默认 go-git ListRemote 实现(纯 Go,无宿主 git 依赖,不驻留)。
	projectSvc := project.New(st.DB, credVault, nil)

	// 装配触发配置服务(Story 2.3):复用 vault secretbox(master key)加密 webhook 签名密钥。
	// master key 未配置时,涉及密钥的读取/重置降级为明确错误(不 panic)。
	triggerSvc := trigger.New(st.DB, credVault)

	// 装配流水线配置服务(Story 2.2):经 store 触库,惰性默认 + spec 校验 + YAML 渲染。
	pipelineSvc := pipeline.New(st.DB)

	// 装配构建/部署配置服务(Story 2.4):独立于 2-2 spec;经 vault 校验 secret 引用存在性 +
	// 回算掩码。master key 未配置时,涉及 secret 引用的保存降级为明确错误(不 panic)。
	pipelineSettingsSvc := pipeline.NewSettingsService(st.DB, credVault)

	// 装配定时(cron)触发配置服务(Story 8-6):每项目一份 cron 表达式 + 分支 + 启用开关。
	// 调度器(下方,pool 后)按启用配置分钟粒度扫描、到点经 run.Service 创建「定时」触发的运行。
	cronSvc := cron.NewService(st.DB)

	// 装配流水线模板 + 变量组复用基座(FR-8-13 · 对标 Jenkins Shared Library / 云效模板与变量组)。
	// 模板:命名、可复用的流水线定义(与项目流水线同一套 stages 模型),跨项目共享;应用模板经
	// pipelineSvc.Save 把 stages 落到项目流水线(复用同一套规范化/校验/渲染)。变量组:命名、可复用的
	// 变量集合,镜像每流水线变量模型;secret 经 credVault 校验存在性、只存引用,明文绝不入库。
	templateSvc := library.NewTemplateService(st.DB, pipelineSvc)
	varGroupSvc := library.NewVarGroupService(st.DB, credVault)
	// 自定义节点(复用库 Tier 2):命名、可复用的「单节点」(nodeType + config 快照),供选择器复用。
	customNodeSvc := library.NewCustomNodeService(st.DB)

	// 装配项目级并发上限配置服务(FR-8-10 并发/队列控制):每项目一份 max_concurrent。
	// 注入 worker pool 做准入(下方);经 /api/projects/{id}/concurrency 读写。
	concurrencySvc := run.NewConcurrencyService(st.DB)
	parameterSvc := run.NewParameterService(st.DB)

	// 装配流水线串联配置服务(FR-8-11):每项目一份「成功后触发的下游目标」列表。
	// 串联终态钩子(下方,与通知钩子同槽组合)在 run 落 success 时据此创建下游运行;
	// 经 /api/projects/{id}/chain 读写。环路安全在 chain.NewHook(深度门 + 路径门)。
	chainSvc := chain.NewService(st.DB)

	// 装配运行服务(Story 3.1):本期桩 runner 跑占位步骤打通状态机;真实构建/部署=3-3/4-x。
	// worker pool 在 aiSvc 装配后创建(以便注入 best-effort 自动诊断钩子,Story 7.2)。
	// 注入分支→环境解析器(#56):手动/定时/串联触发未显式带环境时,据项目分支映射补解析
	// 环境 + 目标服务器,与 webhook 接收路径一致(否则 build_image 不知推哪个 registry、
	// 部署节点 pull 不到镜像)。复用 trigger 包同一套 matchBranch glob。
	runSvc := run.New(st.DB, run.WithEnvResolver(trigger.NewEnvironmentResolver(st.DB)))
	// DORA 指标只读聚合(FR-8-15):同一 *service 实现暴露为 MetricsService,无新连接/新表。
	doraMetricsSvc := run.NewMetricsService(runSvc)

	// 装配人工审批门协调器 + 持久层(Story 8-4):DAG 调度到 Gate 阶段时阻塞等待批准/拒绝。
	// 协调器为进程内(零持久状态);记录落 run_approvals 供 UI/审计/重启清理。
	approvalCoord := approval.New()
	approvalStore := approval.NewStore(st.DB)

	// 装配环境晋级流持久层(Story 8-7 / FR-8-7):晋级记录 + 从流水线配置派生环境链/变量。
	// 环境链与逐环境变量不再独立存储,改由 pipelineSettingsSvc 派生(单一真相源),
	// 故此处注入该服务;晋级审批门复用上面的 approvalCoord/approvalStore(不另起协调器)。
	promotionStore := promotion.NewStore(st.DB, pipelineSettingsSvc)

	// 装配「环境一等公民」只读聚合服务(对标 GitLab environments):按环境聚合部署历史 +
	// 一键回滚定位。纯查询既有表(pipeline_runs + deploy_targets + run_artifacts),零迁移、无副作用。
	environmentsSvc := environments.NewService(st.DB)

	// 重启清理:进程重启会丢失内存等待者,把残留 waiting_approval 运行清为 failed(孤儿不可恢复)。
	if n, err := runSvc.FailOrphanedRuns(context.Background()); err != nil {
		log.Printf("[run] 警告:清理孤儿运行失败:%v", err)
	} else if n > 0 {
		log.Printf("[run] 启动清理孤儿运行(waiting_approval/running):%d 条 → failed", n)
	}

	// 装配诊断反馈服务(Story 7.5;FR-26):诊断 👍/👎(👎 可附正确根因)持久化 + 准确率统计(知识库种子)。
	// 复用同一 *sql.DB(参数化 SQL);correctRootCause 脱敏在 httpapi 层(过 mask)+ 领域层长度上限兜底。
	// upsert by run_id(同 run 改判覆盖)。无 init 副作用、不抬高空载内存。
	feedbackSvc := run.NewFeedbackService(st.DB)

	// 装配 webhook 接收器(Story 3.2):按 token 定位项目触发配置、解密密钥验签、
	// 解析 Gitee 投递、按 events+分支映射匹配、去重后经 RunService 创建运行(入 pool 调度)。
	webhookReceiver := trigger.NewReceiver(st.DB, credVault, httpapi.NewRunCreator(runSvc))

	// 装配审计地基(Story 1.4):敏感操作成功后写 append-only 审计表(SQLite trigger 硬拦
	// UPDATE/DELETE)。Masker 在 detail 落库前脱敏(绝不写明文 secret);可选远端 sink
	// (ATOMGO_AUDIT_SINK)使本地被删后远端仍完整。sink 失败降级不阻断主操作。
	auditMasker := mask.NewMasker()
	var auditSink audit.Sink
	if sink, err := audit.SinkFromEnv(); err != nil {
		log.Printf("[audit] 警告:远端 sink 配置无效(%v),仅启用本地审计。", err)
	} else if sink != nil {
		auditSink = sink
		log.Printf("[audit] 远端审计 sink 已启用(ATOMGO_AUDIT_SINK)")
	}
	auditRec := audit.New(st.DB, auditMasker, auditSink)

	// 装配可配置 AI 提供商服务(Story 7.1):复用 vault secretbox 加密 apiKey(密文入库)。
	// Test 探测用带超时的 HTTP 客户端(~8s);master key 未配置时涉及密钥的操作降级为明确错误。
	// 未配置/未启用 → 下游优雅禁用,核心 CI/CD 不依赖此服务(NFR-10)。
	aiSvc := ai.New(st.DB, credVault, &http.Client{Timeout: 8 * time.Second})

	// 装配仓库分析器(Story 2.5):go-git 浅克隆(内存 storer + memfs,Depth:1)读 manifest
	// 检测技术栈;严格 SSRF 收口;克隆失败优雅降级(不致命)。无 init 副作用/不驻留。
	repoAnalyzer := ai.NewRepoAnalyzer()

	// 装配成功/失败差异计算器(Story 7.3;FR-25):go-git 克隆 + 两 commit tree diff;严格 SSRF 收口;
	// 克隆失败/commit 不可达优雅降级(不致命)。无 init 副作用/不驻留。
	runDiffer := ai.NewRunDiffer()

	// 装配进程内 worker pool(Story 3.1)+ best-effort 自动诊断钩子(Story 7.2)。
	// 钩子在 run→failed 后「取失败日志→脱敏→ai.Diagnose→保存」,经 Option 注入使 run 包不 import ai。
	// AI 未配/失败仅降级(status=unavailable),绝不阻断 run 终态(NFR-10)。pool 调度多 run 并行、结果独立。
	// 日志/诊断/反馈/通知出网脱敏器(Story 3.6/7-2/7-5/5-3;AC-SEC-04)——**红线修**:
	// 不再只登记桩假 secret,而是 per-run 解析该 run 真实用到的全部凭据(项目主凭据 + 流水线
	// settings 的 secret 构建/环境变量 + 镜像仓库凭据),从 vault Reveal 明文登记进 Masker。
	// 这样 3-3 真实构建落地后,真实日志/诊断/反馈/通知里的真凭据也一律 [MASKED],绝不明文外发。
	secretSrc := httpapi.NewRunSecretSource(runSvc, projectSvc, pipelineSettingsSvc, credVault)

	// 装配通知渠道服务(Story 5.1;FR-19):复用 vault secretbox 加密敏感字段(如 SMTP 密码,密文入库)。
	// webhook 投递用带超时的注入 HTTP 客户端(~8s);webhook URL 经 SSRF 收口(拒云元数据/链路本地)。
	// master key 未配置时,涉及敏感字段的保存/读取降级为明确错误(不 panic)。未启用渠道发送时优雅跳过。
	// 提前装配(先于 pool)以便把通知钩子注入 pool(Story 5.2;FR-20)。
	notifySvc := notify.New(st.DB, credVault, &http.Client{Timeout: 8 * time.Second})

	// 选择运行执行器(Story 3-3/3-5):按 ATOMGO_BUILDER 开关装配真实隔离构建器或桩。
	//   - real :强制真实 Builder(探测不到容器 CLI 即启动失败,诚实不假装)。
	//   - stub :强制桩 runner(合成日志,不碰容器;dev/CI 无 Docker 时用)。
	//   - 缺省 auto:探测到 docker/nerdctl/podman 用 real,否则回退桩(优雅降级,NFR-10)。
	// 真实 Builder 经构造函数注入 projectSvc/pipelineSettingsSvc/credVault;真实日志/产物/失败日志
	// 经同一 run.StepSink 喂出,复用既有持久化/SSE/脱敏(下方 WithLogMaskerFunc 对真实日志同样生效)。
	// 运行执行器默认 DAG 调度执行器(Epic 8 · 8-1↔8-3):按阶段 needs 构 DAG 调度
	// (无依赖阶段并行、失败阻断下游),真按 UI 可视化流水线 stages/script job/deploy_ssh/notify
	// 执行,阶段编排经既有 StepSink 持久化。**默认启用**(下方据 ATOMGO_RUNNER 选择):
	// 仅 ATOMGO_RUNNER=legacy 才回退 ATOMGO_BUILDER 选出的旧版固定流程 Builder / 桩 runner。
	// 目标服务器(SSH 执行层)+ 远程构建 runner 配置(FR-8-14 续):项目可指定一台 server 作远程构建机,
	// 配置后该项目构建下沉到远程执行(控制机本地克隆 → 经 SSH 传工作区 → 远程容器跑;token 只在控制机)。
	targetSvc := target.New(st.DB, credVault, nil)
	// 统一实时采集缓存(6-1/6-2 性能):后台按可配置间隔异步采集服务器指标与容器清单,
	// 写入进程内缓存;服务器/容器/概览接口优先读缓存,秒开且降 SSH 压力。间隔存 system_settings
	// (live_poll_interval,默认 30s,可在「设置 → 系统」页配置);ctx 取消即随进程停机退出。
	liveCache := livecache.New()
	for kind, c := range httpapi.NewLiveCacheCollectors(targetSvc) {
		liveCache.Register(kind, c)
	}
	liveCache.SetTarget(targetSvc)
	liveCache.SetIntervalFn(func() time.Duration { return httpapi.LivePollInterval(settingsSvc) })
	// Assistant sessions use a separate, local-only recorder and masker.
	opsMasker := mask.NewMasker()
	if masterKey != nil {
		opsMasker.RegisterSecret(fmt.Sprintf("%x", masterKey[:]))
		opsMasker.RegisterSecret(base64.StdEncoding.EncodeToString(masterKey[:]))
	}
	var opsChatSvc *opschat.Service
	if limited, ok := targetSvc.(target.LimitedExecutor); ok {
		chat, err := opschat.New(opschat.Options{DB: st.DB, Vault: credVault, Executor: limited,
			Model: ai.NewOpsModel(aiSvc, opsMasker), Masker: opsMasker, LocalRecorder: audit.New(st.DB, opsMasker, nil)})
		if err == nil {
			err = chat.Start(context.Background())
		}
		if err == nil {
			opsChatSvc = chat
		} else {
			log.Printf("[opschat] unavailable: local key, storage or executor configuration")
		}
	}
	runnerSvc := runner.New(st.DB, targetExister{targetSvc})

	// 制品库(Story 8-16 / FR-8-16):构建后把 jar/dist 真字节归档,部署时取真字节上传目标机
	// (否则只有占位 reference,jar/dist 无法真正部署)。默认落 DB 同级 artifacts/,可经
	// ATOMGO_ARTIFACT_DIR 改。构造失败 → 降级 nil(保持旧占位行为,不中断启动)。
	var artStore *artifactstore.Store
	{
		artDir := strings.TrimSpace(os.Getenv("ATOMGO_ARTIFACT_DIR"))
		if artDir == "" {
			artDir = filepath.Join(filepath.Dir(cfg.DBPath), "artifacts")
		}
		if as, aerr := artifactstore.New(artDir); aerr == nil {
			artStore = as
			log.Printf("[artifact] 制品库已启用(归档 jar/dist 真字节供部署):%s", artDir)
		} else {
			log.Printf("[artifact] 警告:制品库不可用(%v),jar/dist 部署退回占位引用", aerr)
		}
	}

	// 代码管理区(Story 8-18 / FR-8-18):中控机维护持久 bare 镜像,每次构建增量 fetch 后从本地镜像
	// 秒级出工作区(大仓库/高频构建省时省网),并给「列分支/commit」提供数据(下方 refs 端点)。
	// 默认 DB 同级 repos/(ATOMGO_REPO_CACHE_DIR 可改);ATOMGO_NO_REPO_CACHE=1 关。
	// 任何缓存问题都回退直连网络克隆(永不让构建挂)。
	var repoCache *repocache.Cache
	if os.Getenv("ATOMGO_NO_REPO_CACHE") != "1" {
		repoDir := strings.TrimSpace(os.Getenv("ATOMGO_REPO_CACHE_DIR"))
		if repoDir == "" {
			repoDir = filepath.Join(filepath.Dir(cfg.DBPath), "repos")
		}
		if rc, rerr := repocache.New(repoDir, build.NewCloner()); rerr == nil {
			repoCache = rc
			log.Printf("[repocache] 代码管理区已启用(本地镜像+增量 fetch):%s", repoDir)
		} else {
			log.Printf("[repocache] 警告:代码管理区不可用(%v),构建退回每次直连克隆", rerr)
		}
	}

	// 构建依赖缓存(build cache · P0):script 类节点配了 cachePaths 时,执行前恢复依赖目录
	// (node_modules/.m2/.gradle 等)到工作区、执行后保存回缓存库,按 key(分支 + lockfile hash)
	// 寻址,跨 run 持久化提速。默认 DB 同级 cache/(ATOMGO_CACHE_DIR 可改);
	// ATOMGO_NO_BUILD_CACHE=1 关。缓存问题绝不让构建失败(恢复失败=冷构建,保存失败=记日志)。
	var cacheStore *buildcache.Store
	if os.Getenv("ATOMGO_NO_BUILD_CACHE") != "1" {
		cacheDir := strings.TrimSpace(os.Getenv("ATOMGO_CACHE_DIR"))
		if cacheDir == "" {
			cacheDir = filepath.Join(filepath.Dir(cfg.DBPath), "cache")
		}
		if cs, cerr := buildcache.New(cacheDir); cerr == nil {
			cacheStore = cs
			log.Printf("[buildcache] 构建依赖缓存已启用(配 cachePaths 的脚本节点跨 run 复用依赖):%s", cacheDir)
		} else {
			log.Printf("[buildcache] 警告:构建依赖缓存不可用(%v),脚本节点每次冷构建", cerr)
		}
	}

	// 把代码缓存(若启用)做成 Builder 选项;未启用 → 无操作选项(保持默认直连克隆器)。
	clonerOpt := func(*build.Builder) {}
	var refsLister httpapi.RefsLister
	if repoCache != nil {
		clonerOpt = build.WithCloner(repoCache)
		refsLister = repoCache
	}

	// 构建依赖缓存(若启用)做成 Builder 选项;未启用 → 无操作(避免 typed-nil 接口陷阱)。
	buildCacheOpt := func(*build.Builder) {}
	if cacheStore != nil {
		buildCacheOpt = build.WithBuildCache(cacheStore)
	}

	// 部署服务(提前到 dag 装配前构造,供 deploy_ssh 流水线节点注入)。Story 4.6 诊断钩子复用 7-2。
	deploySvc := deploy.New(targetSvc, runSvc, deploy.WithDiagnoseHook(httpapi.NewDiagnoseHook(runSvc, aiSvc, secretSrc)), deploy.WithArtifactStore(artStore), deploy.WithBatchDB(st.DB))
	if recovery, ok := deploySvc.(interface{ RecoverInterruptedBatches(context.Context) error }); ok {
		if err := recovery.RecoverInterruptedBatches(context.Background()); err != nil {
			log.Fatalf("recover deploy batches: %v", err)
		}
	}

	// 运行执行器选择(Epic 8):**默认走 DAG 调度执行器**——它是唯一真正按 UI 配置的
	// stages/script job/deploy_ssh/notify 编排执行的运行器。只有显式 ATOMGO_RUNNER=legacy
	// 才回退旧版固定流程(clone→对仓库根 docker build→deploy,无视可视化流水线)。
	// ATOMGO_RUNNER=dag 仍受支持(向后兼容);未设或任意非 legacy 值一律 = dag。
	// DAG 模式探测不到容器 CLI(docker)时优雅回退 stub(现有逻辑,NFR-10)。
	var runnerOpts []run.PoolOption
	onboardingCapabilities := onboarding.Capabilities{Runtime: onboarding.RuntimeStub, VaultConfigured: masterKey != nil,
		RepositoryOverride: strings.EqualFold(strings.TrimSpace(os.Getenv("ATOMGO_PAC_RUNTIME")), "1")}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("ATOMGO_RUNNER")), "legacy") {
		onboardingCapabilities.Legacy = true
		runnerOpts, onboardingCapabilities.Runtime = buildRunnerSelection(projectSvc, pipelineSettingsSvc, credVault, artStore, repoCache)
		log.Printf("[run] ATOMGO_RUNNER=legacy:旧版固定流程运行器已启用(clone→对仓库根 docker build→deploy,⚠ 不执行 UI 可视化流水线 stages;如需真按流水线跑请去掉该 env)")
	} else {
		// 阶段执行体(Story 8-2):探测到容器 CLI → 注入真实阶段执行器(script 类型 job 在隔离
		// 容器内真实跑命令,复用 Builder 的 cloner+driver);否则回退 stub(优雅降级,NFR-10)。
		var dagOpts []dagrun.Option
		// 审批门 hook(Story 8-4):Gate 阶段阻塞等待人工批准/拒绝。进入等待态后 best-effort 发
		// 「需要审批」通知 + 签名审批链接(signer/PUBLIC_URL 未配则跳过通知,门行为不变)。
		approvalNotifier := httpapi.NewApprovalNotifier(notifySvc, approvalSigner, publicURLFn, runSvc)
		dagOpts = append(dagOpts, dagrun.WithGate(httpapi.NewApprovalGate(runSvc, approvalCoord, approvalStore, approvalNotifier)))
		if b, berr := build.NewBuilder(projectSvc, pipelineSettingsSvc, credVault, build.WithArtifactStore(artStore), build.WithArtifactLister(runSvc.ListArtifacts), build.WithImageGC(os.Getenv("ATOMGO_NO_IMAGE_GC") != "1"), build.WithCommitRecorder(func(ctx context.Context, runID, commit string) { _ = runSvc.SetCommit(ctx, runID, commit) }), build.WithStageDeployer(deploySvc), build.WithStageNotifier(notifySvc), clonerOpt, buildCacheOpt); berr == nil {
			// runSvc 作测试报告持久层注入(Story 8-6 / FR-8-6):script 步骤产报告 → 解析 →
			// 落库 → 质量门禁裁决(不过则阶段失败,阻断下游部署)。
			dagOpts = append(dagOpts, dagrun.WithStageExecutor(build.NewStageExecutorWithRunner(b, runSvc, runnerSvc, targetSvc)))
			onboardingCapabilities.Runtime = onboarding.RuntimeAvailable
			log.Printf("[run] DAG 调度执行器已启用(默认;阶段按 needs 编排,真按 UI 可视化流水线 stages 执行;script 类型 job 在隔离容器真实执行,CLI=%s;ATOMGO_RUNNER=legacy 可回退旧版固定流程)", b.DriverBinary())
		} else {
			log.Printf("[run] DAG 调度执行器已启用(默认;阶段按 needs 编排,真按 UI 可视化流水线 stages 执行;⚠ 未探测到容器 CLI,阶段执行体回退 stub:%v;ATOMGO_RUNNER=legacy 可回退旧版固定流程)", berr)
		}
		// 「流水线即代码」运行时覆盖(FR-8-12):装饰器包住库内 loader——运行时若项目**开启了
		// 流水线即代码**(每项目开关 projects.pac_enabled)且仓库根含合法 `.atomgo.yml`,即用它
		// 驱动本次运行,否则一律回退库内配置(任何 YAML/拉取问题都不中断运行)。SpecLoader 现已带运行
		// 分支,故按**本次运行分支**拉取(空分支退化默认分支);库内 pipeline.Service(分支无关)经
		// dagrun.SpecLoaderFunc 适配进 branch 感知契约,其 2 参公有签名不变(其它调用方不受影响)。
		//
		// 装饰器始终挂载(每项目开关默认关 → 老项目行为不变,无需服务端 env)。历史全局开关
		// ATOMGO_PAC_RUNTIME=1 保留为**全局强开**:无视每项目开关,对所有项目尝试覆盖。
		pacGlobalOverride := strings.EqualFold(strings.TrimSpace(os.Getenv("ATOMGO_PAC_RUNTIME")), "1")
		var specLoader dagrun.SpecLoader = pacSpecLoader{pacloader.New(
			pipelineSvc,
			pacProjectLookup{projectSvc},
			pacGitAuthResolver{vault: credVault},
			pacBlobFetcher{httpapi.NewSourceReader()},
			pacGlobalOverride,
		)}
		if pacGlobalOverride {
			log.Printf("[pac] 全局强开:.atomgo.yml 覆盖对所有项目生效(ATOMGO_PAC_RUNTIME=1;无视每项目开关)")
		}
		runnerOpts = []run.PoolOption{run.WithRunner(dagrun.New(specLoader, dagOpts...))}
	}

	// 自动 HTTPS + 域名反向代理(R1):路由 CRUD + 在目标主机上经 targetSvc(SSH + docker)编排
	// Caddy(渲染 Caddyfile + reload),Caddy 自动经 Let's Encrypt(HTTP-01)签发/续期证书。
	// 复用已装配的 targetSvc(SSH 执行/上传)+ st.DB(参数化 SQL),无新顶层依赖、无 init 副作用。
	proxySvc := proxy.New(st.DB, targetSvc)

	// 终态钩子:通知(Story 5.2)+「PR 状态回写」(Story 8-9 / FR-8-9):run 终态 → 据项目仓库
	// 识别 GitHub/Gitee → 经项目凭据回写该 commit 的提交状态(PR 检查)。
	//
	// PR 回写钩子**始终挂载**,但仅对**开启了 PR 状态检查**的项目(每项目开关 projects.pr_status_enabled)
	// 生效——老项目默认关 → 行为不变(不会意外往真实仓库回写),且无需服务端 env 即可逐项目启用。
	// 历史全局开关 ATOMGO_PR_STATUS=1 保留为**全局强开**:无视每项目开关,对所有项目回写(向后兼容)。
	// API base 可经 env 覆盖(GitHub/Gitee 企业版自托管;空则用公有云默认),无论是否全局强开均生效。
	// best-effort:失败仅记日志,不阻断 run 终态。
	terminalHook := httpapi.NewNotifyHook(runSvc, notifySvc, secretSrc)
	prStatusGlobalOverride := strings.EqualFold(strings.TrimSpace(os.Getenv("ATOMGO_PR_STATUS")), "1")
	reporter := prstatus.NewReporter(&http.Client{Timeout: 10 * time.Second}).WithBaseURLs(
		strings.TrimSpace(os.Getenv("ATOMGO_PR_STATUS_GITHUB_BASE")),
		strings.TrimSpace(os.Getenv("ATOMGO_PR_STATUS_GITEE_BASE")))
	prHook := httpapi.NewPRStatusHook(runSvc, projectSvc, credVault, reporter,
		publicURLFn, prStatusGlobalOverride)
	notify := terminalHook
	terminalHook = func(ctx context.Context, runID, finalStatus string) {
		notify(ctx, runID, finalStatus)
		prHook(ctx, runID, finalStatus)
	}
	if prStatusGlobalOverride {
		log.Printf("[prstatus] 全局强开:PR 状态回写对所有项目生效(ATOMGO_PR_STATUS=1;无视每项目开关)")
	}

	// 流水线串联终态钩子(FR-8-11):复用同一终态槽,叠加到 terminalHook 之上。
	// run 落 success → 据上游项目串联配置创建下游运行(TriggerChain);环路安全(深度门 + 路径门)
	// 在 chain.NewHook 内。深度上限经 ATOMGO_CHAIN_MAX_DEPTH 覆盖(默认 5);best-effort,
	// 失败仅记日志,绝不阻断 run 终态。
	chainMaxDepth := chain.DefaultMaxDepth
	if v := strings.TrimSpace(os.Getenv("ATOMGO_CHAIN_MAX_DEPTH")); v != "" {
		if n, perr := strconv.Atoi(v); perr == nil && n > 0 {
			chainMaxDepth = n
		} else {
			log.Printf("[chain] 警告:ATOMGO_CHAIN_MAX_DEPTH=%q 非法(须为正整数),用默认 %d", v, chain.DefaultMaxDepth)
		}
	}
	chainHook := chain.NewHook(chainSvc, runSvc, chainMaxDepth)
	priorTerminal := terminalHook
	terminalHook = func(ctx context.Context, runID, finalStatus string) {
		priorTerminal(ctx, runID, finalStatus)
		chainHook(ctx, runID, finalStatus)
	}

	// 并发/队列控制(FR-8-10):全局上限经 ATOMGO_MAX_CONCURRENT 设置(默认 = worker 数);
	// 项目级上限经 concurrencySvc(/api/projects/{id}/concurrency 配置)。突发超限的运行保持 queued
	// 排队,待槽位释放再调度(FIFO)。非法/未设 env → 0,pool 回落到默认行为(仅受全局上限)。
	maxConcurrent := 0
	if v := strings.TrimSpace(os.Getenv("ATOMGO_MAX_CONCURRENT")); v != "" {
		if n, perr := strconv.Atoi(v); perr == nil && n > 0 {
			maxConcurrent = n
		} else {
			log.Printf("[run] 警告:ATOMGO_MAX_CONCURRENT=%q 非法(须为正整数),忽略", v)
		}
	}

	pool := run.NewWorkerPool(runSvc,
		append(runnerOpts,
			run.WithDiagnoseHook(httpapi.NewDiagnoseHook(runSvc, aiSvc, secretSrc)),
			// per-run 日志脱敏:每个 run 构建登记了该 run 真实凭据的 Masker(红线修)。
			run.WithLogMaskerFunc(secretSrc.MaskerForRun),
			// best-effort 终态钩子(通知 + 可选 PR 状态回写)。未配路由的事件不发送;绝不阻断 run 终态。
			run.WithNotifyHook(terminalHook),
			// 并发/队列控制(FR-8-10):全局上限 + 项目级上限准入。
			run.WithMaxConcurrent(maxConcurrent),
			run.WithConcurrencyLimits(concurrencySvc),
		)...,
	)
	pool.Start()
	log.Printf("[run] worker pool started")

	// 装配并启动定时(cron)调度器(Story 8-6):分钟粒度扫描启用的项目 cron,到点经 run.Service
	// 创建「定时」触发的运行(入 pool 调度)。同一项目同一分钟去重;表达式非法的项目跳过。
	// 无启用配置时空转无副作用。优雅停机随 worker pool 停。
	cronScheduler := cron.NewScheduler(cronSvc, httpapi.NewScheduledRunCreator(runSvc))
	cronScheduler.Start(context.Background())
	log.Printf("[cron] 定时调度器已启动")

	// 运行数据保留清理器:按 retention_config 策略定期裁剪过期终态运行(连带其日志/步骤/产物),
	// 防止无限增长撑爆磁盘。默认策略 enabled=0(不删);用户在设置里开启并配条数/天数后生效。
	retentionSvc := retention.NewService(st.DB)
	retentionSweeper := retention.NewSweeper(retentionSvc, time.Hour)
	retentionSweeper.Start(context.Background())
	log.Printf("[retention] 保留清理器已启动(每小时一扫;策略默认关,需在设置开启)")

	// 装配只读源码读取器(Story 3.6;FR-4 预埋):go-git 浅克隆读 tree/blob;严格 SSRF 收口;
	// 克隆失败优雅降级(不致命)。无 init 副作用/不驻留。
	sourceReader := httpapi.NewSourceReader()

	// 装配目标服务器服务(Story 4.1;internal/target,FR-14):通用 SSH exec/session 基座,
	// 供 Epic 4 部署 + Epic 6 运维共享。SSH 私钥/口令经 vault 即用即弃,绝不入库/日志/响应/错误。
	// dialer 传 nil → 使用默认 x/crypto/ssh 实现(无宿主依赖,不驻留)。master key 未配置时
	// Exec/Test 降级为明确错误(不 panic)。

	// 装配部署执行服务(Story 4.2;internal/deploy,FR-10):把成功运行的产物经 SSH 部署到目标机。
	// 部署命令 array 化经 targetSvc.Exec 执行(AC-SEC-02,不拼 shell);每机结果落 deploy_targets,
	// 据结果更新 run 终态(success/partial_failed/failed)。复用已装配的 targetSvc + runSvc,无新顶层依赖。
	// (notifySvc 已在 pool 装配前声明〔Story 5.2 注入 NotifyHook〕,此处不重复。)
	// Story 4.6(FR-22 种子):注入诊断钩子 → 部署失败(failed/partial_failed)合成失败日志 +
	// best-effort 触发 7-2 AI 诊断,让诊断飞轮覆盖部署失败(而非只覆盖构建失败)。复用 7-2 NewDiagnoseHook。
	// (deploySvc 已提前在 dag 装配前构造,供 deploy_ssh 流水线节点注入;此处不重复。)

	// 装配可配置异常检测服务(Story 6.5;FR-23):按阈值规则对服务器指标做检测,命中产告警入库。
	// 检测复用 6-1 指标采集(NewAnomalyCollector 适配 collectServerMetrics);不可达/指标 null 的
	// 服务器跳过(不误报)。复用已装配的 targetSvc(采集),无新顶层依赖;无 init 副作用/不起轮询。
	// 告警去重窗口(同「服务器×规则条件」最小间隔):env ATOMGO_ANOMALY_COOLDOWN(秒)覆盖,默认 600s。
	anomalyCooldown := envDurationSeconds("ATOMGO_ANOMALY_COOLDOWN", 600*time.Second)
	// 定时检测间隔:env ATOMGO_ANOMALY_INTERVAL(秒)覆盖,默认 60s;置 0 关闭定时(仅手动)。
	// 在此提前算出(httpapi 经 WithAnomalyConfig 暴露给前端显示节奏 + 下方定时器复用)。
	anomalyInterval := envDurationSeconds("ATOMGO_ANOMALY_INTERVAL", 60*time.Second)
	anomalySvc := anomaly.New(st.DB, httpapi.NewAnomalyCollector(targetSvc),
		anomaly.WithNotifier(httpapi.NewAnomalyNotifier(notifySvc)),
		anomaly.WithCooldown(anomalyCooldown),
	)

	// 服务器指标时序历史(异常检测「看趋势」折线图数据源):后台采样器复用 6-1 采集周期性写样本,
	// 按保留窗口清理。采样间隔 env ATOMGO_METRICS_SAMPLE_INTERVAL(秒,默认 60;0 关闭采样),
	// 保留天数 env ATOMGO_METRICS_RETENTION_DAYS(默认 7)。
	metricsHist := metrics.New(st.DB)
	metricsSampleInterval := envDurationSeconds("ATOMGO_METRICS_SAMPLE_INTERVAL", 60*time.Second)
	metricsRetentionDays := 7
	if v := strings.TrimSpace(os.Getenv("ATOMGO_METRICS_RETENTION_DAYS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			metricsRetentionDays = n
		}
	}
	metricsSampleCollector := httpapi.NewAnomalyCollector(targetSvc)

	// 装配多 provider OAuth 凭据接入服务(连接 Gitee/GitHub/GitLab/自建):复用 vault secretbox 加密
	// OAuth 应用 client_secret(密文入库);OAuth 拿到的 access_token 经 vault.Create 存成 git_token 凭据,
	// 之后克隆/构建直接复用(无需改克隆逻辑)。Exchange 兑换用带超时的注入 HTTP 客户端(~10s);
	// state CSRF 短期内存绑会话。master key 未配置时涉及密钥的操作降级为明确错误(不 panic)。
	oauthSvc := oauth.New(st.DB, credVault, &http.Client{Timeout: 10 * time.Second})

	// 晋升镜像(FR-7 闭环):控制机容器 CLI 把 image 产物按需镜像到目标环境仓库(同仓库跳过本体、
	// 异仓库完整镜像 + 通道 tag)。探测不到容器 CLI → 传 nil,闭包内降级为不镜像(沿用源引用)。
	mirrorDriver, _ := build.DetectDriver(nil)

	srv := &http.Server{
		Addr: cfg.Addr,
		Handler: httpapi.New(webFS, authSvc, append([]httpapi.Option{httpapi.WithOnboarding(onboarding.New(st.DB, onboardingCapabilities)), httpapi.WithVault(credVault), httpapi.WithProjects(projectSvc), httpapi.WithTriggers(triggerSvc), httpapi.WithPipelines(pipelineSvc), httpapi.WithPipelineSettings(pipelineSettingsSvc), httpapi.WithRuns(runSvc, pool), httpapi.WithWebhooks(webhookReceiver), httpapi.WithAudit(auditRec), httpapi.WithAccount(authSvc), httpapi.WithUsers(authSvc), httpapi.WithAISettings(aiSvc), httpapi.WithAIGenerate(repoAnalyzer), httpapi.WithRunDiff(runDiffer), httpapi.WithSource(sourceReader), httpapi.WithRefs(refsLister), httpapi.WithArtifactStore(artStore), httpapi.WithServers(targetSvc), httpapi.WithRunnerConfig(runnerSvc), httpapi.WithDeploy(deploySvc), httpapi.WithNotifications(notifySvc), httpapi.WithRetention(retentionSvc), httpapi.WithProxy(proxySvc), httpapi.WithLiveCache(liveCache), httpapi.WithSettings(settingsSvc), httpapi.WithDiagnosisFeedback(feedbackSvc), httpapi.WithAnomaly(anomalySvc), httpapi.WithAnomalyConfig(int(anomalyInterval.Seconds()), int(anomalyCooldown.Seconds())), httpapi.WithMetricsHistory(metricsHist), httpapi.WithSecretSource(secretSrc), httpapi.WithOAuth(oauthSvc), httpapi.WithOAuthPublicOrigin(cfg.PublicOrigin), httpapi.WithCron(cronSvc), httpapi.WithChain(chainSvc), httpapi.WithApprovals(approvalCoord, approvalStore), httpapi.WithApprovalLinks(approvalSigner), httpapi.WithConcurrency(concurrencySvc), httpapi.WithParameters(parameterSvc), httpapi.WithPromotion(promotionStore), httpapi.WithImageMirror(func(ctx context.Context, projectID, targetEnv, sourceRef string) (string, error) {
			if mirrorDriver == nil {
				return sourceRef, nil // 无容器 CLI:不镜像,目标机仍从构建仓库拉
			}
			ref, _, ierr := build.MirrorImageForPromotion(ctx, mirrorDriver, credVault, pipelineSettingsSvc, projectID, targetEnv, sourceRef)
			return ref, ierr
		}), httpapi.WithEnvironments(environmentsSvc), httpapi.WithDoraMetrics(doraMetricsSvc), httpapi.WithTemplates(templateSvc), httpapi.WithVariableGroups(varGroupSvc), httpapi.WithCustomNodes(customNodeSvc), httpapi.WithOpsChat(opsChatSvc)}, setupOpts...)...),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// WriteTimeout 置 0:SSE 长连接(/api/runs/{id}/events)不可被写超时切断;
		// 各非流式 handler 自身在合理时间内完成,慢响应风险由 ReadTimeout/IdleTimeout 兜底。
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	// 优雅停机:SIGINT/SIGTERM 后 Shutdown,放行在途请求并让 defer st.Close() 生效。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 异常检测定时器(Story 6.5 增强):周期性 Check → 命中产告警 + best-effort 通知(去重已在服务内)。
	// 间隔 env ATOMGO_ANOMALY_INTERVAL(秒)覆盖,默认 60s;置 "0" 关闭定时(仍可手动「立即检测」)。
	// 无 enabled 规则时 Check 提前返回(不采集),故空载零开销。随 ctx 取消(停机)退出。
	if anomalyInterval > 0 {
		go func() {
			ticker := time.NewTicker(anomalyInterval)
			defer ticker.Stop()
			log.Printf("[anomaly] 定时检测已启用,间隔 %s", anomalyInterval)
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if _, err := anomalySvc.Check(ctx); err != nil && ctx.Err() == nil {
						log.Printf("[anomaly] 定时检测失败(已跳过本轮):%v", err)
					}
				}
			}
		}()
	}

	// 指标时序采样器(异常检测「看趋势」数据源):每 metricsSampleInterval 采集一次写时序历史,
	// 并按保留窗口清理旧样本。独立于异常检测(无规则也采样),随 ctx 停机退出。置 0 关闭采样。
	if metricsSampleInterval > 0 {
		go func() {
			ticker := time.NewTicker(metricsSampleInterval)
			defer ticker.Stop()
			log.Printf("[metrics] 指标采样已启用,间隔 %s,保留 %d 天", metricsSampleInterval, metricsRetentionDays)
			for {
				select {
				case <-ctx.Done():
					return
				case t := <-ticker.C:
					snaps, err := metricsSampleCollector.Collect(ctx, nil)
					if err != nil {
						if ctx.Err() == nil {
							log.Printf("[metrics] 采样失败(已跳过本轮):%v", err)
						}
						continue
					}
					if err := metricsHist.Record(ctx, httpapi.SamplesFromSnapshots(snaps, t.UTC())); err != nil && ctx.Err() == nil {
						log.Printf("[metrics] 写采样失败:%v", err)
					}
					// 顺手清理过期样本(保留窗口外)。
					if n, err := metricsHist.Sweep(ctx, t.UTC().AddDate(0, 0, -metricsRetentionDays)); err != nil && ctx.Err() == nil {
						log.Printf("[metrics] 清理过期样本失败:%v", err)
					} else if n > 0 {
						log.Printf("[metrics] 清理过期样本 %d 行", n)
					}
				}
			}
		}()
	}

	// 启动统一实时采集缓存后台采集器(随 ctx 停机退出;启动即采一轮,首屏不空窗)。
	liveCache.Start(ctx)
	log.Printf("[livecache] 实时采集缓存已启动(间隔 %s,可在「设置 → 系统」调整)", httpapi.LivePollInterval(settingsSvc))

	go func() {
		host, port := splitAddr(cfg.Addr)
		urlHost := host
		if urlHost == "" || urlHost == "0.0.0.0" || urlHost == "::" || urlHost == "[::]" {
			urlHost = "localhost"
		}
		bindAll := host == "" || host == "0.0.0.0" || host == "::" || host == "[::]"
		log.Printf("atomgo 已启动 — 访问地址: http://%s:%s (监听 %s%s)",
			urlHost, port, cfg.Addr, map[bool]string{true: ", 所有网络接口"}[bindAll])
		if setupPending {
			log.Printf("首次启动未完成初始化:请浏览器打开 http://%s:%s/setup 完成初始化向导(将创建管理员账号 + 生成保险库密钥)。在此之前凭据保险库与登录不可用,属正常现象。", urlHost, port)
		} else {
			log.Printf("初始化已完成;控制台: http://%s:%s/dashboard", urlHost, port)
		}
		// 双击启动(桌面交互会话)时,等本地服务可连通后自动打开默认浏览器,方便用户直接登录。
		// 以服务/无图形会话运行时由 shouldAutoOpenBrowser 自动跳过。
		if shouldAutoOpenBrowser() {
			go openBrowserAfterDelay(browserLaunchURL(setupPending, publicURLFn(), urlHost, port), cfg.Addr)
		}
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	stop()
	log.Printf("shutting down…")
	if opsChatSvc != nil {
		opsCtx, opsCancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := opsChatSvc.Close(opsCtx); err != nil {
			log.Printf("[opschat] worker shutdown did not converge before deadline")
		}
		opsCancel()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
	// HTTP 停机后停 worker pool:取消在途运行执行,等 worker 退出(随 shutdownCtx 超时)。
	cronScheduler.Stop()
	retentionSweeper.Stop()
	pool.Stop(shutdownCtx)
	log.Printf("[run] worker pool stopped")
}

// splitAddr 把监听地址拆成 (host, port)。
// 支持 ":7088"、"0.0.0.0:7088"、"127.0.0.1:7088"、"localhost:7088"、"[::]:7088"。
// host 为空表示监听所有接口。
func splitAddr(addr string) (host, port string) {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[:i], addr[i+1:]
	}
	return "", addr
}

// browserLaunchURL 计算双击启动后自动打开的浏览器地址:优先用系统设置里的对外 URL,
// 否则回退到本机监听地址;待初始化时直达 /setup 向导,否则进根路径(由前端路由到登录/控制台)。
func browserLaunchURL(setupPending bool, publicURL, urlHost, port string) string {
	base := strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if base == "" {
		base = "http://" + urlHost + ":" + port
	}
	if setupPending {
		return base + "/setup"
	}
	return base + "/"
}

// shouldAutoOpenBrowser 决定是否在启动后自动打开浏览器。
// 默认开启(便于双击启动后直接登录);以下场景关闭:显式禁用、以 systemd 服务运行、Linux 无图形会话。
func shouldAutoOpenBrowser() bool {
	if v := strings.TrimSpace(os.Getenv("ATOMGO_NO_OPEN_BROWSER")); v == "1" || strings.EqualFold(v, "true") {
		return false
	}
	for _, a := range os.Args[1:] {
		if a == "--no-open-browser" || a == "-no-open-browser" {
			return false
		}
	}
	// systemd 服务(INVOCATION_ID)等非交互场景不弹浏览器。
	if os.Getenv("INVOCATION_ID") != "" {
		return false
	}
	// Linux 仅在有图形会话(DISPLAY / Wayland)时打开。
	if runtime.GOOS == "linux" {
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return false
		}
	}
	return true
}

// openBrowser 用系统默认浏览器打开 rawURL。
func openBrowser(rawURL string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", "", rawURL}
	case "darwin":
		cmd = "open"
		args = []string{rawURL}
	default:
		cmd = "xdg-open"
		args = []string{rawURL}
	}
	return exec.Command(cmd, args...).Start()
}

// openBrowserAfterDelay 等本地服务可连通后再打开浏览器(最多等待 5s),
// 避免端口尚未 bind 就拉起浏览器导致首页打不开。
func openBrowserAfterDelay(rawURL, addr string) {
	host, port := splitAddr(addr)
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 200*time.Millisecond)
		if err == nil {
			_ = c.Close()
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if err := openBrowser(rawURL); err != nil {
		log.Printf("[browser] 自动打开浏览器失败(可手动访问 %s):%v", rawURL, err)
		return
	}
	log.Printf("[browser] 已尝试在默认浏览器打开:%s", rawURL)
}

// loadMasterKeyFile 从文件读取 base64 master key(首次启动 Web 向导落盘格式;
// 文件不存在返回 os.ErrNotExist,内容非法返回明确错误;错误信息绝不含 key 值)。
func loadMasterKeyFile(path string) (*[config.MasterKeyLen]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("master key 文件不是合法 base64")
	}
	if len(decoded) != config.MasterKeyLen {
		return nil, fmt.Errorf("master key 文件应解码为 %d 字节,实为 %d", config.MasterKeyLen, len(decoded))
	}
	var key [config.MasterKeyLen]byte
	copy(key[:], decoded)
	for i := range decoded {
		decoded[i] = 0
	}
	return &key, nil
}

// envDurationSeconds 读取 name 环境变量(整数秒)为 time.Duration;未设/非法 → def。
// 显式 "0"(或负)→ 0(调用方据此关闭对应定时任务)。
func envDurationSeconds(name string, def time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Printf("[config] %s=%q 非法(应为整数秒),用默认 %s", name, v, def)
		return def
	}
	if n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

// ── 「流水线即代码」运行时覆盖适配器(FR-8-12)──────────────────────────────────
// 把既有领域服务桥接到 pacloader 的小接口,使 pacloader 不反向 import project/vault/httpapi。

// pacProjectLookup 把 project.Service 适配为 pacloader.ProjectLookup(取仓库/凭据/默认分支)。
type pacProjectLookup struct{ svc project.Service }

func (p pacProjectLookup) Lookup(ctx context.Context, projectID string) (pacloader.ProjectInfo, error) {
	proj, err := p.svc.Get(ctx, projectID)
	if err != nil {
		return pacloader.ProjectInfo{}, err
	}
	return pacloader.ProjectInfo{
		RepoURL:       proj.RepoURL,
		CredentialID:  proj.CredentialID,
		DefaultBranch: proj.DefaultBranch,
		PacEnabled:    proj.PacEnabled,
	}, nil
}

type pacGitAuthResolver struct{ vault vault.Vault }

func (r pacGitAuthResolver) GetGitAuth(credentialID string) (string, string, error) {
	auth, err := r.vault.GetGitAuth(credentialID)
	if err != nil {
		return "", "", err
	}
	return auth.Username, auth.Token, nil
}

// pacBlobFetcher adapts SourceReader for pacloader without exposing credentials.
type pacBlobFetcher struct{ reader httpapi.SourceReader }

func (b pacBlobFetcher) FetchBlob(ctx context.Context, repoURL, username, token, ref, file string) (string, bool, error) {
	blob, err := b.reader.Blob(ctx, repoURL, username, token, ref, file)
	if err != nil {
		return "", false, err
	}
	return blob.Content, blob.Degraded, nil
}

// pacSpecLoader 把 *pacloader.Loader 适配为 dagrun.SpecLoader:转换其同形的 SpecSource
// (配置来源可见性 · Slice 2)。pacloader 不反向 import dagrun(避免环),故来源结构在 main 处转换。
type pacSpecLoader struct{ inner *pacloader.Loader }

func (l pacSpecLoader) Get(ctx context.Context, projectID, branch string) (*pipeline.Config, dagrun.SpecSource, error) {
	cfg, src, err := l.inner.Get(ctx, projectID, branch)
	return cfg, dagrun.SpecSource{
		FromRepo:       src.FromRepo,
		Ref:            src.Ref,
		File:           src.File,
		FallbackReason: src.FallbackReason,
	}, err
}

// buildRunnerOption 按 ATOMGO_BUILDER 开关返回注入 pool 的运行执行器选项(Story 3-3/3-5)。
//
//   - real :强制真实 Builder;构造失败(无容器 CLI)直接 fail-fast(用户显式要真实却没环境,应明确报错)。
//   - stub :不注入 WithRunner(pool 默认 StubRunner),返回空选项。
//   - 其它/缺省 auto:尝试构造真实 Builder,探测不到容器 CLI 时回退桩(优雅降级,NFR-10)。
//
// 返回 []run.PoolOption(可能为空):空 ⇒ 用 pool 默认 StubRunner。
func buildRunnerOption(projectSvc project.Service, settingsSvc pipeline.SettingsService, v vault.Vault, artStore *artifactstore.Store, repoCache *repocache.Cache) []run.PoolOption {
	opts, _ := buildRunnerSelection(projectSvc, settingsSvc, v, artStore, repoCache)
	return opts
}

func buildRunnerSelection(projectSvc project.Service, settingsSvc pipeline.SettingsService, v vault.Vault, artStore *artifactstore.Store, repoCache *repocache.Cache) ([]run.PoolOption, string) {
	clonerOpt := func(*build.Builder) {}
	if repoCache != nil {
		clonerOpt = build.WithCloner(repoCache)
	}
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("ATOMGO_BUILDER")))
	switch mode {
	case "stub":
		log.Printf("[build] ATOMGO_BUILDER=stub:使用桩 runner(合成日志,不碰容器)")
		return nil, onboarding.RuntimeStub
	case "real":
		b, err := build.NewBuilder(projectSvc, settingsSvc, v, build.WithArtifactStore(artStore), build.WithImageGC(os.Getenv("ATOMGO_NO_IMAGE_GC") != "1"), clonerOpt)
		if err != nil {
			log.Fatalf("[build] ATOMGO_BUILDER=real 但构建器不可用:%v", err)
		}
		log.Printf("[build] 真实隔离构建器已启用(容器 CLI=%s)", b.DriverBinary())
		return []run.PoolOption{run.WithRunner(b)}, onboarding.RuntimeAvailable
	default:
		b, err := build.NewBuilder(projectSvc, settingsSvc, v, build.WithArtifactStore(artStore), build.WithImageGC(os.Getenv("ATOMGO_NO_IMAGE_GC") != "1"), clonerOpt)
		if err != nil {
			log.Printf("[build] 未探测到容器 CLI(docker/nerdctl/podman),回退桩 runner(ATOMGO_BUILDER=real 可强制要求真实):%v", err)
			return nil, onboarding.RuntimeStub
		}
		log.Printf("[build] auto:真实隔离构建器已启用(容器 CLI=%s)", b.DriverBinary())
		return []run.PoolOption{run.WithRunner(b)}, onboarding.RuntimeAvailable
	}
}

// targetExister 把 target.Service 适配为 runner.ServerExister(Get 无错即存在),供 runner 配置校验。
type targetExister struct{ svc target.Service }

func (t targetExister) Exists(ctx context.Context, id string) bool {
	_, err := t.svc.Get(ctx, id)
	return err == nil
}

// ── 首次启动向导的运行时支撑:atomgo.env 加载与进程自重启 ───────────────────────

// executableDir 返回当前可执行文件所在目录(用于定位 atomgo.env / master.key 的稳定位置)。
func executableDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(exe)
	if dir == "" {
		return ".", nil
	}
	return dir, nil
}

// resolveEnvFilePath 解析 atomgo.env 路径:ATOMGO_ENV_FILE 优先;否则取可执行文件同目录。
func resolveEnvFilePath(exeDir string) string {
	if p := strings.TrimSpace(os.Getenv("ATOMGO_ENV_FILE")); p != "" {
		return p
	}
	if exeDir != "" && exeDir != "." {
		return filepath.Join(exeDir, "atomgo.env")
	}
	return "atomgo.env"
}

// loadEnvFile 把 atomgo.env 中的 KEY=VALUE 注入进程环境(仅当该键尚未在环境中设置,
// 显式环境变量优先)。文件不存在或不可读时静默跳过(全新部署尚无此文件)。
func loadEnvFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return // 文件不存在属常态,不报错
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		if key == "" {
			continue
		}
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}

// selfRestart 以相同参数、环境与标准流重新执行当前二进制(脱离父进程),随后退出。
// 用于首次启动向导固化 MySQL/PostgreSQL 配置后,让进程以新数据库配置重新启动——
// 无需用户手动重启,也无需退出后依赖外部 supervisor。文件型部署(双击启动)同样适用。
// 测试环境不注入此回调,故不会产生真实重启。
func selfRestart() {
	exe, err := os.Executable()
	if err != nil {
		log.Printf("[setup] 自重启失败:无法定位可执行文件:%v", err)
		return
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = os.Environ()
	detachCmd(cmd)
	if err := cmd.Start(); err != nil {
		log.Printf("[setup] 自重启失败:无法启动新进程:%v", err)
		return
	}
	log.Printf("[setup] 数据库配置已保存,正在重启以应用新数据库…")
	os.Exit(0)
}
