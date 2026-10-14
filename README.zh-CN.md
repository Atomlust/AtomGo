<div align="center">

# AtomGo

**一个轻量、自托管的 CI/CD + 部署 + 运维一体化平台。**
单个 Go 静态二进制(内嵌前端,运行时零依赖),
一个工具替掉「CI + Ansible/Kamal + Portainer」三件套。

[![Release](https://img.shields.io/github/v/release/atomlust/atomgo)](https://github.com/atomlust/atomgo/releases)
[![CI](https://github.com/atomlust/atomgo/actions/workflows/ci.yml/badge.svg)](https://github.com/atomlust/atomgo/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

[English](README.md) | 简体中文

</div>

---

## 为什么是 AtomGo

主流方案要么重(Jenkins 一堆插件 + JVM),要么是三个工具拼装(Woodpecker/Drone + Ansible/Kamal + Portainer)。AtomGo 把「持续集成、多服务器部署、服务器/容器运维」装进**一个静态二进制**:下载、启动、打开浏览器 —— 不装 JVM、不装 Node、不装数据库,**从零到可用只要 60 秒**。

如今它已长过那三件套:把部署好的服务挂上 HTTPS 域名(原本的 nginx + certbot 那一步)也都内置了 —— 而且首次启动会在浏览器里走一遍 Web 初始化向导:选数据库、建管理员、生成凭据保险库密钥,点几下鼠标完成初始化,不碰一行配置文件。

**为谁而造?** 没有专职运维、没有复杂协作流程、只想要「推代码就构建、改完就上线、出事能回滚」的个人开发者与中小团队。它轻量到能跑在树莓派上,本地化部署、本地化构建,数据都在你手里、不上任何云 —— 不养 SaaS 订阅,也不养 Jenkins 运维班底,装好就能用。

| | AtomGo | Jenkins | Drone + Ansible + Portainer |
|---|:---:|:---:|:---:|
| 单二进制部署 | ✅ | ❌ JVM + 插件 | ❌ 三套拼装 |
| 可视化流水线编排(DAG) | ✅ 内置画布 | 插件 | YAML 手写 |
| 流水线即代码(按分支演进) | ✅ | 插件 | ✅ |
| 隔离构建 | ✅ | ✅ | ✅ |
| 多服务器部署(SSH,免 Agent) | ✅ 内置 | 插件 | Ansible |
| 服务器 / 容器运维 | ✅ 内置 | ❌ | Portainer |
| 零停机 + 失败回滚 | ✅ | 插件 | 自己写 |
| 自动 HTTPS + 域名反向代理 | ✅ 内置 | ❌ | ❌ |
| 界面多语言(8 种开箱即用) | ✅ | 插件 | ❌ |
| DORA 指标 | ✅ 内置 | 插件 | ❌ |
| AI 失败诊断 | ✅ 可选 | ❌ | ❌ |
| 一键自更新 | ✅ | ❌ | ❌ |
| 轻量 · 低资源占用(适配小机器) | ✅ 单二进制极省 | ❌ JVM 吃内存 | ⚠️ 三件套各自占资源 |

## 界面速览

<div align="center">
  <img src="docs/screenshot/dashboard.png" alt="AtomGo 概览仪表盘" width="820">
  <p><sub>概览:DORA 四指标、运行态势、服务器健康、环境状态与 AI 诊断,一屏尽览</sub></p>
  <img src="docs/screenshot/setup.png" alt="AtomGo 首次启动初始化向导" width="820">
  <p><sub>首次启动 Web 初始化向导:选数据库、建管理员、生成凭据保险库密钥 —— 点几下鼠标,部署完成</sub></p>
</div>

## 能力总览

**复杂留给我们,简单留给你** —— 从流水线编排到 HTTPS 证书,全部在浏览器里点出来;推代码即构建,构建完即部署,出事一键回滚,全程可视化、全链路可审计。

- **⚡ 一键启动,60 秒上手** —— 一个静态二进制(或一条 `docker run`)就是全部安装:不装 JVM、不装 Node、不装数据库,SQLite 默认零配置。首次启动自动进入 Web 初始化向导 —— 选数据库、建管理员、生成凭据保险库密钥,全程点几下鼠标,不碰一行配置文件。`SETUP_SERVICE=1` 一行,再送开机自启 + 崩溃自愈的 systemd 服务。
- **🪶 轻量省资源,小机器也跑得稳** —— 单个 Go 静态二进制(无 JVM、无 Node、无运行时依赖),常驻内存极低、闲置近乎零开销;默认 SQLite 零运维,树莓派 / 512M 小机也能当主力 CI。构建支持**本地化构建**:本地 bare 镜像增量 fetch + 按分支 + lockfile hash 寻址的构建依赖缓存,秒级出工作区、不每次拉满带宽;也可指定远程构建机把重活下沉到强算力(token 只留控制机)。**本地化部署**一条 `docker run` 或一个二进制即落地,数据全在你手里、不上任何云。**专为没有复杂协作流程、追求轻量化的个人开发者与小团队而生** —— 不用养一整套 Jenkins 运维班底,不用付 SaaS 订阅,装好就能用,省下的不只是钱,还有盯监控的那份精力。
- **🧩 可视化流水线:拖出来,不是写出来** —— 浏览器画布拖拽编排阶段 DAG + 阶段内任务级 DAG;矩阵构建、人工审批门(通知里点签名链接即可审批)、旁挂服务、阶段条件 + 后置步骤、类型化运行参数,全部可视配置,服务端权威校验 —— 配错当场拦截,而不是跑到一半才炸。触发方式随你挑:webhook、分支→环境映射、5 字段 cron 定时、上游→下游流水线串联(防环);项目级并发上限 + 超限 FIFO 排队;流水线模板 / 变量组 / 自定义节点进复用库。想走 GitOps?`.atomgo.yml` 流水线即代码按分支演进,画布永远兜底([详见下文](#流水线即代码gitops))。
- **🚀 可视化一键部署 + 一键回滚** —— 页面上点一下即触发部署:经 SSH 免 Agent 并行扇出到多台主机,健康门控 + 零停机切换,多机部分失败看得见;也有无产物的命令型部署(直接重启服务)。环境是一等公民 —— 逐环境部署时间线、当前活跃版本一目了然,失败**点一下回滚到上一次全成功部署**;环境晋级流(dev→staging→prod)+ 逐环境变量/密钥 + 审批门。
- **🔒 免费 HTTPS,自动签发自动续期** —— 填个域名,Let's Encrypt 证书按 HTTP-01 自动签发、到期自动续期(通配符 / DNS-01 不支持),零人工零费用。托管模式:每台目标主机一个托管 Caddy 容器,复用与容器运维同一套 SSH + docker 手法编排(渲染 Caddyfile → `docker cp` → 优雅 reload);已有自己的 Caddy?**外接模式**:AtomGo 只写配置片段并触发你方 Caddy reload,不接管容器、不占 80/443。多域名别名、路径路由(`/api`→A、`/`→B)、重定向、访问控制(basic auth、IP 允许/拒绝 CIDR)、HSTS / 安全头 / 压缩、多上游负载均衡 + 主动健康检查故障转移、WebSocket / gRPC(h2c)、按真实 443 握手探测的证书大盘,一应俱全;TCP 透传(caddy-l4)需把 `ATOMGO_CADDY_IMAGE` 指向带该插件的 Caddy 构建。原本 nginx + certbot 那一步,现在是一张表单。
- **🔐 安全地基** —— 多用户 RBAC 角色访问控制(argon2id + CSRF,详见[系统角色与权限](#系统角色与权限))· 凭据加密保险库(NaCl secretbox,掩码呈现,绝无明文)· OAuth 应用接入 Gitee / GitHub / GitLab / 自建实例(拿到的 access token 直接存成可复用的保险库凭据)· append-only 审计(SQLite trigger 硬拦 UPDATE/DELETE)+ 可选远端 sink · 按 run 解析真实凭据后对日志 / 诊断 / 通知全链路脱敏。
- **🏗 隔离构建与产物** —— 版本钉死的容器内隔离构建(docker/nerdctl/podman)· 代码管理区:本地 bare 镜像 + 增量 fetch,秒级出工作区 · 构建依赖缓存(按分支 + lockfile hash 寻址)· 内容寻址制品库,jar/dist 存**真字节**供部署(而非占位 reference)· 镜像构建 + 推送私有仓库 + 镜像 GC · 每项目可指定远程构建机(构建经 SSH 下沉到远程,token 只留控制机)· JUnit + Cobertura 测试报告喂质量门禁,不过则阶段失败、阻断下游部署 · 实时终端日志(SSE)+ 历史回放 · 只读代码浏览(Monaco)。
- **📣 通知** —— 企业微信 / 钉钉 / 飞书 / Slack / 邮件 / 自定义 webhook · 事件→渠道细粒度路由 · 模板 + 变量自定义 · 飞书富卡片(审批/详情行动按钮 + 发版汇总)· 流水线内通知节点。
- **🖥 服务器与容器运维** —— 多机状态总览(CPU/内存/磁盘)+ 指标时序趋势图 · 容器/镜像/Stacks/卷/网络管理 · 容器创建/inspect/prune · 实时 + 历史服务日志 · 实时 stats · 容器交互终端 · Web 运维终端(主机 shell,完整复制粘贴/信号支持)· 可配置异常检测:定时自动跑、冷却去重、命中走通知渠道。
- **🤖 AI 辅助(可选,完全可降级)** —— 自带 Claude / OpenAI / Ollama 端点(apiKey 密文入保险库)。构建/部署失败自动根因诊断 + 👍/👎 反馈飞轮与准确率统计 · 仓库分析 → 生成流水线草案 · 成功/失败提交差异 · 脚本风险标注 · 运维终端的自然语言→shell 助手与容器诊断。核心 CI/CD 路径完全不依赖它(NFR-10)。
- **📈 度量** —— DORA 四指标(部署频率/变更前置时长/变更失败率/平均恢复时长)开箱即用,并给 Elite/High/Medium/Low 绩效分档。
- **🌐 生而多语言** —— **8 种界面语言**(简中 / 繁中 / 英 / 日 / 韩 / 德 / 法 / 西)一键切换,连 API 错误信息都做服务端本地化 —— 中文团队的顺手,海外团队的开箱,同一份二进制。
- **🧹 数据与平台** —— 运行数据保留清理器(默认关;绝不动在跑的运行)· SQLite(纯 Go)、MySQL 或 PostgreSQL。
- **🔄 检查 + 一键自更新** —— 设置→系统 一键查 GitHub 最新发布并语义比对;二进制部署可页面**一键自动更新**(下载 + 校验和核验 + 原子替换 + 自重启),Docker 部署给出精确升级命令。**触发更新需管理员权限**(`system:update`);一键自动替换仅 Linux/macOS 二进制部署可用,Windows 与 Docker 退回手动升级或执行命令。

> 安全不可妥协:凭据仅以密文存储、命令 array 化防注入、出网 SSRF 收口、日志脱敏。

## 系统角色与权限

内置三级 RBAC,端点级 `资源:操作` 权限拦截,权限不足一律 403 —— 给团队分工降风险,给审计留全量轨迹:

| 能力 | 👑 管理员 admin | 🛠️ 运维 operator | 👁️ 只读 viewer |
|---|:---:|:---:|:---:|
| 浏览项目 / 流水线 / 运行与日志 | ✅ | ✅ | ✅ |
| 查看服务器状态与指标 | ✅ | ✅ | ✅ |
| 查看凭据(仅掩码) | ✅ | ✅ | ✅ |
| 查看审计日志 | ✅ | ✅ | ✅ |
| 建项目 / 配置流水线 / 修改可视配置 | ✅ | ✅ | — |
| 触发运行 / 部署 / 回滚 | ✅ | ✅ | — |
| 审批人工审批门 | ✅ | ✅ | — |
| 域名反代与 HTTPS 证书配置 | ✅ | ✅ | — |
| 容器运维(启停 / 终端 / 清理) | ✅ | ✅ | — |
| 模板 / 变量组 / 自定义节点 / 批量操作 | ✅ | ✅ | — |
| 服务器增删改 | ✅ | — | — |
| 凭据增删改 | ✅ | — | — |
| 用户与角色管理 | ✅ | — | — |
| 系统设置 / 一键自更新 | ✅ | — | — |

> 底线保护:系统强制保留至少一名管理员;口令 argon2id 加盐哈希,会话 + CSRF 双防护;敏感操作全部落 append-only 审计 —— 数据库 trigger 层面硬禁 UPDATE/DELETE,改不掉的轨迹才配叫审计。

## 安装 / 部署

三种形态任选,平台本体是单静态二进制、**运行时零依赖**(无需 Go/Node)。

> **Docker 前置**:平台本体不依赖 Docker,但**「隔离构建 / 容器部署」需要 Docker**(没有则降级到桩 runner、不做真实构建)。控制台 / SSH 部署 / 通知不需要。一键脚本会**检测 Docker** 并在缺失时提示;Linux 下可 `INSTALL_DOCKER=1` 自动安装(经官方 get.docker.com),macOS 请装 Docker Desktop。

### ① 一键脚本(Linux / macOS)

从 GitHub Release 下载对应平台的静态二进制装到 `/usr/local/bin`(含校验和核验 + Docker 检测):

```bash
curl -fsSL https://raw.githubusercontent.com/atomlust/atomgo/master/install.sh | sh

# 钉版本 / 自定义目录 / Linux 顺带自动装 Docker:
VERSION=v1.0.0 INSTALL_DIR=$HOME/.local/bin INSTALL_DOCKER=1 \
  sh -c "$(curl -fsSL https://raw.githubusercontent.com/atomlust/atomgo/master/install.sh)"

# 运行 —— 首次启动 AtomGo 会在 http://localhost:7088 提供 Web 初始化向导,
# 用于创建管理员账号、并(可选)生成保险库 master key。全新启动无需任何 env;
# 下面两行是非交互等价写法(用它们预置即可跳过向导):
ATOMGO_MASTER_KEY=$(openssl rand -base64 32) \
ATOMGO_ADMIN_PASSWORD=change-me \
  atomgo          # 或直接:atomgo  → 打开 http://localhost:7088 按向导操作
```

**推荐:装为 systemd 服务**(开机自启 + 崩溃重启 + 一键自更新可用;Linux,需 root)。脚本会自动持久化 master key 到 `/etc/atomgo/master.key`、数据落 `/var/lib/atomgo`、配置写 `/etc/atomgo/atomgo.env`:

```bash
SETUP_SERVICE=1 sh -c "$(curl -fsSL https://raw.githubusercontent.com/atomlust/atomgo/master/install.sh)"
# 状态 / 日志:systemctl status atomgo  ·  journalctl -u atomgo -f
# 改端口等:编辑 /etc/atomgo/atomgo.env 后 systemctl restart atomgo

# 用 MySQL 而非默认 SQLite(DSN 为 go-sql-driver 格式,parseTime=true 必带):
SETUP_SERVICE=1 ATOMGO_DB_DRIVER=mysql \
  ATOMGO_DB_DSN='user:pw@tcp(host:3306)/atomgo?parseTime=true&charset=utf8mb4' \
  sh -c "$(curl -fsSL https://raw.githubusercontent.com/atomlust/atomgo/master/install.sh)"

# 或用 PostgreSQL(DSN 为 lib/pq 格式;内网/可信网络可用 sslmode=disable):
SETUP_SERVICE=1 ATOMGO_DB_DRIVER=postgres \
  ATOMGO_DB_DSN='postgres://atomgo:pgpass@host:5432/atomgo?sslmode=disable' \
  sh -c "$(curl -fsSL https://raw.githubusercontent.com/atomlust/atomgo/master/install.sh)"
```

> Windows 用户:到 [Releases](https://github.com/atomlust/atomgo/releases) 下载 `.zip`。

### ② docker compose(推荐自托管)

```bash
curl -fsSLO https://raw.githubusercontent.com/atomlust/atomgo/master/docker-compose.yml
curl -fsSLO https://raw.githubusercontent.com/atomlust/atomgo/master/.env.example
cp .env.example .env       # ATOMGO_DB_DRIVER 默认 sqlite;切 MySQL/PostgreSQL 见 .env 注释
docker compose up -d       # 数据持久化在具名卷 atomgo-data;首次启动打开地址按 Web 向导走完初始化
```

### ③ docker run(最快试用)

```bash
docker run -d -p 7088:7088 -v atomgo-data:/data \
  -e ATOMGO_ADMIN_PASSWORD=change-me \
  -e ATOMGO_MASTER_KEY=$(openssl rand -base64 32) \
  ghcr.io/atomlust/atomgo:latest
# 提示:去掉上面两行 -e,AtomGo 仍能启动 —— 首次启动会自动进入 Web 初始化向导。
```

### 首次启动(Web 向导)

全新 AtomGo 实例**还没有管理员账号**。在你完成初始化之前,除 `/setup` 外的所有页面都会重定向到向导,且所有业务 API 返回 `404 setup_required` —— 配置完成前绝不暴露任何内容。

1. 启动 AtomGo(全新启动无需任何 env)。
2. 浏览器打开 `http://<host>:7088`,会自动落到 `/setup`。
3. 向导会分步要你填:
   - **数据库(第一步)** —— 默认 **SQLite**(本地文件,零配置,适合绝大多数场景)。如需 **MySQL / PostgreSQL**:先在数据库里建一个空库,然后在向导里选对应驱动并填写连接信息(主机 / 端口 / 用户名 / 密码 / 库名,或直接填完整 DSN)。向导会校验连接、把配置写入 `atomgo.env`,并**自动重启**以应用新数据库;重启后向导回到本步并锁定数据库,继续下面的管理员设置。**整个过程无需你手动改任何配置文件。**
   - **管理员账号** —— 用户名(默认 `admin`)+ 口令(8–72 位)。
   - **对外 URL**(可选) —— 外部可达的基址;若架在反向代理后请现在就填(webhook 回调 / OAuth 重定向要用)。之后也能在设置里改。
   - **凭据保险库 master key** —— 选 **自动生成**(AtomGo 生成 32 字节密钥、密文落盘,并**仅展示一次** —— 务必妥善保存;数据目录迁移后恢复保险库需要它)或 **自备** base64 的 32 字节密钥。
4. 提交。SQLite 场景下向导创建管理员、注入 master key,随后带你到登录页;MySQL/PostgreSQL 场景则在数据库校验通过后自动重启,重启后再提交管理员设置。

> **非交互等价写法:** 首次启动前预置 `ATOMGO_ADMIN_PASSWORD` 与 `ATOMGO_MASTER_KEY`(或 `ATOMGO_MASTER_KEY_FILE`)——AtomGo 会跳过向导、直接正常启动。向导仅在「尚无管理员」时出现。

### 从源码构建

```bash
make build          # 前端构建 → go:embed → 单个静态二进制 ./atomgo(纯 Go,无 CGO)
./atomgo --version
```

### 更新

打开 **设置 → 系统**,点「检查更新」查最新发布;有新版时:

- **二进制部署**:点「立即更新」即自动下载新版 + 校验和核验 + 替换 + 重启(需对二进制文件有写权限;装在 `$HOME/.local/bin` 免 sudo,或用 `SETUP_SERVICE=1` 装的 root systemd 服务亦满足)。
- **Docker 部署**:容器不替换自身镜像,按提示在宿主执行 `docker compose pull && docker compose up -d`(数据卷保留)。

### 配置(环境变量)

正常安装只需前两个(若跑在反代后面再加 `ATOMGO_PUBLIC_URL`),其余都有合理默认值。

**核心**

| 变量 | 说明 | 默认 |
|---|---|---|
| `ATOMGO_ADMIN_PASSWORD` | 首次启动管理员口令 | 无(须设置) |
| `ATOMGO_MASTER_KEY` | 凭据保险库主密钥(base64 的 32 字节);或用 `ATOMGO_MASTER_KEY_FILE` 指文件 | 未配则保险库禁用 |
| `ATOMGO_ADDR` | HTTP 监听地址 | `:7088` |
| `ATOMGO_PUBLIC_URL` | 外部可访问的基址(如 `https://ci.example.com`)。webhook 回调、OAuth 回跳、通知里的签名审批链接都需要它 | 无 |
| `ATOMGO_ADMIN_USERNAME` | 首次启动管理员用户名 | `admin` |
| `ATOMGO_TRUST_PROXY` | 采信 `X-Forwarded-For` 首段作为审计来源 IP(`1`/`true`/`yes`/`on`)。除非前面确有可信反代,否则别开 —— 否则任意客户端都能伪造审计来源 IP | 关 |
| `ATOMGO_RELEASE_REPO` | 检查更新所查的 GitHub 仓库(可指向你自己的发布仓库;留空回退默认,**无「置空禁用检查」开关**) | `atomlust/atomgo` |
| `ATOMGO_RUNTIME` | 设 `docker` 显式声明容器部署形态(影响自更新方式);否则经 `/.dockerenv` 自动探测 | 自动探测 |

**数据库**

| 变量 | 说明 | 默认 |
|---|---|---|
| `ATOMGO_DB_DRIVER` | 数据库驱动:`sqlite`、`mysql` 或 `postgres` | `sqlite` |
| `ATOMGO_DB` | SQLite 数据库路径(driver=sqlite 时) | `atomgo.db` |
| `ATOMGO_DB_DSN` | MySQL / PostgreSQL DSN(driver=mysql 或 postgres 时必填)。MySQL 用 go-sql-driver 格式(`user:pw@tcp(host:3306)/db?parseTime=true&charset=utf8mb4`);PostgreSQL 用 lib/pq 格式(`postgres://user:pw@host:5432/db?sslmode=disable`) | 无 |

> 多数场景直接用默认 **SQLite** 即可;若要用 MySQL / PostgreSQL,**最简单的方式是在首次启动的 Web 引导界面里选择并填写连接信息**——向导会自动把配置写入 `atomgo.env` 并重启生效,无需手动改文件。以上环境变量仅在你想用 env / docker-compose 显式指定时才需要。

**运行、反代与运维**

| 变量 | 说明 | 默认 |
|---|---|---|
| `ATOMGO_RUNNER` | 运行执行器:`dag`(画布编排)或 `legacy`(旧版固定流程) | `dag` |
| `ATOMGO_BUILDER` | `auto` / `real` / `stub`(是否使用容器做隔离构建) | `auto` |
| `ATOMGO_MAX_CONCURRENT` | 全局同时运行上限(超出排队) | worker 数(4) |
| `ATOMGO_PROXY_MODE` | `managed`(自建 Caddy 容器)或 `external`(只写片段触发你方 Caddy reload) | `managed` |
| `ATOMGO_CADDY_IMAGE` | 反代镜像(通配符 / DNS-01 不支持,DNS 提供商集成已移除) | `caddy:2` |
| `ATOMGO_PR_STATUS` | `1` 对全部项目强开 PR 状态回写 | 关 |
| `ATOMGO_ANOMALY_INTERVAL` | 异常检测间隔(秒);`0` 关闭定时 | `60` |
| `ATOMGO_METRICS_SAMPLE_INTERVAL` | 服务器指标采样间隔(秒);`0` 关闭 | `60` |

> 其余高级变量(缓存目录、镜像 GC、PR 状态 API 基址、外接 Caddy 路径、指标保留天数、审计 sink 等)及其默认值,以源码 `internal/config` 中的定义为准。

## 流水线即代码(GitOps)

把流水线结构写进仓库的 `.atomgo.yml`,**跟代码同源、走 PR 评审、按分支演进**——不再依赖画布配置的隐式漂移。

- **开启**:在项目的流水线页打开「流水线即代码 / Pipeline as code」开关(按项目维度)。
- **生效方式**:开启后,每次运行都从**本次构建分支**的**仓库根**读取 `.atomgo.yml`(分支为空时回退项目默认分支),用其中的流水线 spec 驱动本次运行;不同分支可携带各自的 `.atomgo.yml`。文件用项目绑定的仓库凭据临时拉取,无新增暴露面。
- **永不卡住运行的回退**:文件**缺失** → 回退到画布(UI)里已配置的流水线;文件存在但 **YAML 非法** → 同样回退到已存的画布配置。
- **作用范围**:YAML 只管**流水线结构**(阶段 / 任务 / `needs` / DAG 编排);**变量与缓存、环境与凭据、触发规则**仍来自画布(UI)设置,**不写在 YAML 里**。
- **schema** 与平台「从 YAML 导入」用的是同一套(`version` + `stages` → `jobs`,job 用嵌套 `script:` 块写 `image`/`commands`/`env`/`workdir`)。
- **节点类型**(画布与 YAML 通用):`git_source`、`script`、`build_backend`、`build_frontend`、`build_image`、`push_image`、`deploy_ssh`、`deploy_frontend`、`health_check`、`notify`、`templated`、`custom`。

```yaml
version: 1
stages:
  - id: stg_src              # needs 按阶段 id 引用,故跨阶段依赖须显式写 id
    name: 流水线源
    kind: source
    jobs:
      - name: Gitee 源
        type: git_source
  - id: stg_build
    name: 构建
    kind: build
    needs: [stg_src]
    jobs:
      - name: 运行测试
        type: script
        script:
          image: golang:1.23
          commands:
            - go vet ./...
            - go test ./...
          env:
            CGO_ENABLED: "0"
          workdir: src/app
  - id: stg_deploy
    name: 部署
    kind: deploy
    needs: [stg_build]
    gate: true               # 人工审批门
    when:
      branches: [main, release/*]
    jobs:
      - name: SSH 部署
        type: deploy_ssh
        config:
          targetEnv: prod
```

> 也可用全局环境变量 `ATOMGO_PAC_RUNTIME=1` 对**所有项目**强制开启流水线即代码(无视各项目开关),供向后兼容 / 高级用户使用。

## 技术栈

- **后端**:Go · Chi(路由)· modernc/sqlite(纯 Go,无 CGO)+ go-sql-driver/mysql + lib/pq(PostgreSQL)· go-git(不依赖宿主 git)· NaCl secretbox(保险库)· argon2id + bcrypt · golang.org/x/crypto/ssh(免 Agent 部署)· coder/websocket(终端)· Caddy(在目标主机上编排,提供自动 HTTPS)
- **前端**:Vue 3 `<script setup>` · Vite · naive-ui · OKLCH 双主题 · Monaco(只读代码浏览)· Vitest + Playwright · 经 `go:embed` 内嵌进二进制

依赖刻意保持精简:cron 解析器、DAG 引擎、制品库/构建缓存、Caddyfile 渲染器全部自己实现,不引三方库。

## 架构

一个进程,约 46 个领域包。`internal/httpapi` 是唯一碰 HTTP 的包,所有领域包注入其中;`cmd/atomgo/main.go` 是唯一的装配层(包内互引经这里的适配器晚绑,保持包间单向依赖)。

```
单静态二进制 (cmd/atomgo)
│
├─ 地基
│  ├── internal/config        env 配置 + master key 加载
│  ├── internal/store         开库 + 迁移(sqlite / mysql / postgres 三方言)
│  ├── internal/auth          认证 + 会话 + CSRF
│  ├── internal/vault         凭据加密保险库(secretbox)
│  ├── internal/oauth         OAuth 应用接入 → token 存成保险库凭据
│  ├── internal/gitauth       Git HTTP(S)/SSH 凭据解析
│  ├── internal/onboarding   本地首次成功快照(引导用)
│  ├── internal/settings      系统设置持久化 + 环境变量覆盖读取
│  ├── internal/audit         append-only 审计 + 可选远端 sink
│  ├── internal/mask          secret 脱敏(日志 / 诊断 / 通知)
│  ├── internal/i18n          用户可见信息的服务端本地化
│  └── internal/version       版本检查 + 一键自更新
│
├─ 流水线
│  ├── internal/project       项目接入 + 仓库探测
│  ├── internal/pipeline      流水线 spec + 构建/部署配置 + 校验
│  ├── internal/pipelineyaml  YAML ↔ spec 双向往返(导入 / 导出)
│  ├── internal/pacloader     流水线即代码加载(.atomgo.yml,按分支)
│  ├── internal/library       可复用模板 + 变量组 + 自定义节点
│  ├── internal/trigger       webhook + 分支→环境映射
│  ├── internal/cron          5 字段 cron 解析器 + 分钟粒度调度器
│  └── internal/chain         上游→下游串联(防环)
│
├─ 执行
│  ├── internal/run           运行模型 + worker pool + 日志 + 产物 + 参数
│  ├── internal/dag           纯 DAG 调度内核(零 I/O)
│  ├── internal/dagrun        DAG 运行编排(阶段级 + 任务级,矩阵展开)
│  ├── internal/build         容器内隔离构建 + 镜像 + 阶段执行器
│  ├── internal/runner        每项目远程构建机配置
│  ├── internal/repocache     代码管理区(本地 bare 镜像 + 增量 fetch)
│  ├── internal/buildcache    构建依赖缓存(分支 + lockfile 寻址)
│  ├── internal/artifactstore 内容寻址的产物字节存储
│  ├── internal/testreport    JUnit + Cobertura 解析
│  ├── internal/qualitygate   纯质量门禁评估
│  ├── internal/approval      人工审批门(协调器 + 持久化)
│  └── internal/retention     运行数据保留清理
│
├─ 交付
│  ├── internal/target        通用 SSH exec/session 层(部署 + 运维共享)
│  ├── internal/deploy        SSH 部署执行 + 健康门控 + 回滚
│  ├── internal/environments  环境一等公民 + 回滚目标定位
│  ├── internal/promotion     环境晋级流 + 逐环境变量
│  ├── internal/prstatus      PR / 提交状态回写(GitHub / Gitee)
│  ├── internal/proxy         自动 HTTPS + 域名反向代理(Caddy 编排)
│
├─ 可观测
│  ├── internal/notify        多渠道通知 + 事件路由 + 模板
│  ├── internal/anomaly       可配置阈值检测 + 告警
│  ├── internal/metrics       服务器指标时序(趋势图)
│  ├── internal/dora          DORA 指标 + 绩效分档
│  ├── internal/opschat       运维会话领域核心(AI 运维助手)
│  └── internal/ai            可选 AI:诊断 / 仓库分析 / 差异 / 风险标注
│
├── internal/httpapi          唯一对外 HTTP 面(约 184 条路由)
└── web/                      Vue3 前端(go:embed 内嵌)
```

## 开发状态

✅ **已正式发布**,持续迭代中 —— 最新版本见 [Releases](https://github.com/atomlust/atomgo/releases)(tag 驱动发版:6 平台二进制 + ghcr 多架构镜像)。已在真实生产环境承载多项目的构建、部署与日常运维。

## 贡献 / Contributing

欢迎 PR 与 Issue!动手前请读 [CONTRIBUTING.md](CONTRIBUTING.md)(搭环境 / 测试 / 提交规范),并遵守[行为准则](CODE_OF_CONDUCT.md)。安全漏洞请走私密渠道,见 [SECURITY.md](SECURITY.md)。

## License

MIT — 见 [LICENSE](LICENSE)。

---

<div align="center">
<sub>AtomGo —— 把 CI、部署与运维装进一个二进制。</sub>
</div>
