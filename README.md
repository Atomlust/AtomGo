<div align="center">

# AtomGo

**A lightweight, self-hosted CI/CD + deployment + ops platform.**
A single static Go binary (frontend embedded, zero runtime dependencies) —
one tool replacing the "CI + Ansible/Kamal + Portainer" trio.

[![Release](https://img.shields.io/github/v/release/atomlust/atomgo)](https://github.com/atomlust/atomgo/releases)
[![CI](https://github.com/atomlust/atomgo/actions/workflows/ci.yml/badge.svg)](https://github.com/atomlust/atomgo/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

English | [简体中文](README.zh-CN.md)

</div>

---

## Why AtomGo

Mainstream options are either heavy (Jenkins with a pile of plugins + JVM) or a three-tool assembly (Woodpecker/Drone + Ansible/Kamal + Portainer). AtomGo packs "continuous integration, multi-server deployment, and server/container ops" into **one static binary**: download, start, open the browser — no JVM, no Node, no database, **from zero to usable in 60 seconds**.

It has since grown past that trio: publishing a deployed service on an HTTPS domain (the nginx + certbot step) is built in too — and first launch walks you through a Web setup wizard in the browser: pick the database, create the admin account, generate the credential-vault master key. A few clicks, zero config files touched.

**Who is it for?** Solo developers and small teams with no dedicated ops person and no elaborate collaboration pipeline — people who just want "push to build, ship when done, roll back when something breaks." It's light enough to run on a Raspberry Pi, deploys and builds locally, and keeps your data in your own hands — no SaaS subscription, no Jenkins ops squad; install it and it just works.

| | AtomGo | Jenkins | Drone + Ansible + Portainer |
|---|:---:|:---:|:---:|
| Single-binary deploy | ✅ | ❌ JVM + plugins | ❌ three-tool assembly |
| Visual pipeline orchestration (DAG) | ✅ built-in canvas | plugin | hand-written YAML |
| Pipeline as code (per-branch YAML) | ✅ | plugin | ✅ |
| Isolated builds | ✅ | ✅ | ✅ |
| Multi-server deploy (SSH, agentless) | ✅ built-in | plugin | Ansible |
| Server / container ops | ✅ built-in | ❌ | Portainer |
| Zero-downtime + failure rollback | ✅ | plugin | DIY |
| Auto HTTPS + domain reverse proxy | ✅ built-in | ❌ | ❌ |
| UI languages (8 out of the box) | ✅ | plugin | ❌ |
| DORA metrics | ✅ built-in | plugin | ❌ |
| AI failure diagnosis | ✅ optional | ❌ | ❌ |
| One-click self-update | ✅ | ❌ | ❌ |
| Lightweight (fits a small box) | ✅ one tiny binary | ❌ JVM eats RAM | ⚠️ three tools, three footprints |

## Screenshots

<div align="center">
  <img src="docs/screenshot/dashboard.png" alt="AtomGo dashboard" width="820">
  <p><sub>Dashboard: DORA metrics, run activity, server health, environment status, and AI diagnosis on one screen</sub></p>
  <img src="docs/screenshot/setup.png" alt="AtomGo first-run setup wizard" width="820">
  <p><sub>First-run web wizard: pick a database, create the admin, generate the vault master key — a few clicks and you're deployed</sub></p>
</div>

## Feature Overview

**Complexity is our problem, simplicity is yours** — from pipeline orchestration to HTTPS certificates, everything is clicked into place in the browser; push to build, build to deploy, one click to roll back — visual end to end, auditable all the way.

- **⚡ One-click start, up and running in 60 seconds** — a single static binary (or one `docker run`) is the whole install: no JVM, no Node, no database, SQLite zero-config by default. First launch opens a web setup wizard — pick the database, create the admin, generate the credential-vault master key; a few clicks, zero config files touched. `SETUP_SERVICE=1` adds a self-healing systemd service with auto-start on boot.
- **🪶 Lightweight enough for a Raspberry Pi** —— a single Go binary (no JVM, no Node, no runtime deps) with a tiny resident memory footprint that's near-idle when idle; SQLite by default means zero ops, so even a Raspberry Pi or a 512 MB box can be your main CI. Builds can run **locally**: an incremental bare-mirror repo cache plus a branch + lockfile-keyed build-dependency cache hands you a workspace in seconds, without maxing out bandwidth on every run; or point a project at a remote build machine to offload the heavy lifting (tokens stay on the control node). **Self-hosted, period** —— one `docker run` or a bare binary and everything lives on your hardware, in your network, never on someone else's cloud. **Built for solo developers and small teams who want to stay lean** —— no Jenkins ops squad to staff, no SaaS subscription to pay; install it and it just works, saving you more than money: the mental overhead of babysitting yet another cluster.
- **🧩 Visual pipelines: drag, don't write** — compose stage-level DAGs and job-level DAGs on a browser canvas; matrix builds, manual approval gates (approve straight from a notification via signed link), sidecar services, `when` conditions + post steps, typed run parameters — all configured visually with server-authoritative validation, so mistakes are caught at save time, not mid-run. Triggers to taste: webhook, branch→environment mapping, 5-field cron, upstream→downstream chaining (loop-safe); per-project concurrency caps with FIFO queueing; pipeline templates / variable groups / custom nodes in a reuse library. Prefer GitOps? Commit `.atomgo.yml` and let it evolve per branch — the canvas always remains the fallback ([details below](#pipeline-as-code-gitops)).
- **🚀 Visual one-click deploy + one-click rollback** — trigger a deploy with a click: agentless SSH fan-out across hosts, health gating, zero-downtime cutover, partial failures visible; command-style deploys (no artifact, just restart a service) too. Environments are first-class — per-env deploy timeline and current active version at a glance; when things go wrong, **one click rolls back to the last fully successful deploy**. Promotion chains (dev→staging→prod) with per-env variables/secrets and approval gates.
- **🔒 Free HTTPS, issued & renewed automatically** — enter a domain and Let's Encrypt does the rest: certificates issued and renewed automatically over HTTP-01 (wildcards / DNS-01 not supported), zero human effort, zero cost. Managed mode runs one orchestrated Caddy container per target host over the same SSH + docker path as container ops (render Caddyfile → `docker cp` → graceful reload). Already have your own Caddy? **External mode**: AtomGo only writes a config fragment and triggers your Caddy's reload — no container takeover, no ports 80/443. Multi-domain aliases, path routing (`/api`→A, `/`→B), redirects, access control (basic auth, IP allow/deny CIDR), HSTS / security headers / compression, multi-upstream load balancing with active health-check failover, WebSocket / gRPC (h2c), and a certificate dashboard probing the real 443 handshake — all included; TCP passthrough (caddy-l4) is available when `ATOMGO_CADDY_IMAGE` points at a Caddy build with that plugin. The old nginx + certbot step is now a form.
- **🔐 Security foundation** — multi-user RBAC role-based access control (argon2id + CSRF, see [Roles & Permissions](#roles--permissions)) · encrypted credential vault (NaCl secretbox, masked display, never plaintext) · OAuth app onboarding for Gitee / GitHub / GitLab / self-hosted instances (the access token lands straight in the vault as a reusable credential) · append-only audit (SQLite triggers hard-block UPDATE/DELETE) with an optional remote sink · per-run secret redaction across logs, diagnostics, and notifications.
- **🏗 Isolated builds & artifacts** — version-pinned isolated builds inside containers (docker/nerdctl/podman) · a local bare-mirror repo cache (incremental fetch, then a workspace in seconds) · build dependency caching keyed by branch + lockfile hash · a content-addressed artifact store that keeps the **real bytes** of jar/dist for deployment (not just a placeholder reference) · image build + push to private registries with image GC · an optional remote build machine per project (build is offloaded over SSH; tokens stay on the control node) · JUnit + Cobertura test reports feeding quality gates that fail the stage and block downstream deploys · live terminal logs (SSE) + history replay · read-only code browsing (Monaco).
- **📣 Notifications** — WeCom / DingTalk / Lark (Feishu) / Slack / email / custom webhook · fine-grained event→channel routing · templates + custom variables · rich Lark cards with approve/detail action buttons and a release summary · in-pipeline notification nodes.
- **🖥 Server & container ops** — multi-host status overview (CPU/memory/disk) plus time-series trend charts · container/image/Stacks/volume/network management · container create/inspect/prune · live + historical service logs · live stats · interactive container terminal · web ops terminal (host shell, full copy-paste/signal support) · configurable anomaly detection that runs on a timer, dedupes by cooldown, and routes alerts to your notification channels.
- **🤖 AI assist (optional, fully degradable)** — bring your own Claude / OpenAI / Ollama endpoint (API key encrypted in the vault). Automatic root-cause diagnosis when a build or deploy fails, with a 👍/👎 feedback loop and accuracy stats · repo analysis → generated pipeline draft · success-vs-failure commit diff · script risk annotation · natural-language→shell assistant and container diagnosis in the ops terminal. The core CI/CD path never depends on any of it (NFR-10).
- **📈 Metrics** — the four DORA metrics (deployment frequency / lead time for changes / change failure rate / mean time to restore) out of the box, with Elite/High/Medium/Low performance bands.
- **🌐 Multilingual by design** — **8 UI languages** (zh-CN / zh-TW / en / ja / ko / de / fr / es) switchable in one click, with server-side localization of API error messages too — one binary, equally at home for Chinese teams and global ones.
- **🧹 Housekeeping & platform** — configurable run-data retention sweeper (off by default; never touches in-flight runs) · SQLite (pure Go), MySQL, or PostgreSQL.
- **🔄 Update check + one-click self-update** — Settings → System checks GitHub for the latest release with semantic comparison; binary deployments can **auto-update with one click** from the UI (download + checksum verification + atomic replace + self-restart), while Docker deployments get the exact upgrade command. **Triggering an update requires admin privileges** (`system:update`); one-click in-place replacement is available only on Linux/macOS binary deployments — Windows and Docker fall back to manual upgrade or a command.

> Security is non-negotiable: credentials stored as ciphertext only, commands arrayified against injection, outbound SSRF locked down, logs redacted.

## Roles & Permissions

Three built-in roles, enforced at the endpoint level with a `resource:action` permission model — insufficient permission is a straight 403. Split responsibilities safely across the team and keep a full audit trail:

| Capability | 👑 Admin | 🛠️ Operator | 👁️ Viewer |
|---|:---:|:---:|:---:|
| Browse projects / pipelines / runs & logs | ✅ | ✅ | ✅ |
| View server status & metrics | ✅ | ✅ | ✅ |
| View credentials (masked only) | ✅ | ✅ | ✅ |
| View audit log | ✅ | ✅ | ✅ |
| Create projects / configure pipelines & visual settings | ✅ | ✅ | — |
| Trigger runs / deploy / rollback | ✅ | ✅ | — |
| Approve manual approval gates | ✅ | ✅ | — |
| Domain reverse proxy & HTTPS certificate config | ✅ | ✅ | — |
| Container ops (start/stop, terminal, cleanup) | ✅ | ✅ | — |
| Templates / variable groups / custom nodes / batches | ✅ | ✅ | — |
| Create/modify/delete servers | ✅ | — | — |
| Create/modify/delete credentials | ✅ | — | — |
| User & role management | ✅ | — | — |
| System settings / one-click self-update | ✅ | — | — |

> Bottom-line protections: the system always keeps at least one admin; passwords are salted argon2id hashes with session + CSRF defenses; every sensitive operation lands in an append-only audit log — SQLite triggers hard-block UPDATE/DELETE, because a trail that can't be edited is the only one worth calling an audit.

## Install / Deploy

Pick any of three form factors. The platform itself is a single static binary with **zero runtime dependencies** (no Go/Node required).

> **Docker prerequisite**: the platform itself doesn't depend on Docker, but **"isolated builds / container deployment" do require Docker** (without it, it degrades to a stub runner and performs no real builds). The console / SSH deployment / notifications don't need Docker. The one-click script **detects Docker** and prompts if it's missing; on Linux you can set `INSTALL_DOCKER=1` to auto-install it (via the official get.docker.com); on macOS, install Docker Desktop.

### ① One-click script (Linux / macOS)

Downloads the static binary for your platform from GitHub Releases and installs it to `/usr/local/bin` (with checksum verification + Docker detection):

```bash
curl -fsSL https://raw.githubusercontent.com/atomlust/atomgo/master/install.sh | sh

# Pin a version / custom dir / auto-install Docker on Linux too:
VERSION=v1.0.0 INSTALL_DIR=$HOME/.local/bin INSTALL_DOCKER=1 \
  sh -c "$(curl -fsSL https://raw.githubusercontent.com/atomlust/atomgo/master/install.sh)"

# Run — on first launch AtomGo serves a Web setup wizard at http://localhost:7088
# where you create the admin account and (optionally) generate the vault master key.
# No env vars are required for a fresh start; the two below are the non-interactive
# equivalent (skip the wizard by pre-seeding them):
ATOMGO_MASTER_KEY=$(openssl rand -base64 32) \
ATOMGO_ADMIN_PASSWORD=change-me \
  atomgo          # or just: atomgo  → open http://localhost:7088 and follow the wizard
```

**Recommended: install as a systemd service** (auto-start on boot + restart on crash + one-click self-update available; Linux, requires root). The script persists the master key to `/etc/atomgo/master.key`, stores data in `/var/lib/atomgo`, and writes config to `/etc/atomgo/atomgo.env`:

```bash
SETUP_SERVICE=1 sh -c "$(curl -fsSL https://raw.githubusercontent.com/atomlust/atomgo/master/install.sh)"
# Status / logs: systemctl status atomgo  ·  journalctl -u atomgo -f
# Change port etc.: edit /etc/atomgo/atomgo.env then systemctl restart atomgo

# Use MySQL instead of the default SQLite (DSN is go-sql-driver format; parseTime=true is required):
SETUP_SERVICE=1 ATOMGO_DB_DRIVER=mysql \
  ATOMGO_DB_DSN='user:pw@tcp(host:3306)/atomgo?parseTime=true&charset=utf8mb4' \
  sh -c "$(curl -fsSL https://raw.githubusercontent.com/atomlust/atomgo/master/install.sh)"

# Or use PostgreSQL (lib/pq DSN; sslmode=disable is fine for a local/trusted network):
SETUP_SERVICE=1 ATOMGO_DB_DRIVER=postgres \
  ATOMGO_DB_DSN='postgres://atomgo:pgpass@host:5432/atomgo?sslmode=disable' \
  sh -c "$(curl -fsSL https://raw.githubusercontent.com/atomlust/atomgo/master/install.sh)"
```

> Windows users: download the `.zip` from [Releases](https://github.com/atomlust/atomgo/releases).

### ② docker compose (recommended for self-hosting)

```bash
curl -fsSLO https://raw.githubusercontent.com/atomlust/atomgo/master/docker-compose.yml
curl -fsSLO https://raw.githubusercontent.com/atomlust/atomgo/master/.env.example
cp .env.example .env       # ATOMGO_DB_DRIVER defaults to sqlite; see .env comments to switch to MySQL/PostgreSQL
docker compose up -d       # data persists in the named volume atomgo-data; on first launch open the URL and follow the Web setup wizard
```

### ③ docker run (fastest trial)

```bash
docker run -d -p 7088:7088 -v atomgo-data:/data \
  -e ATOMGO_ADMIN_PASSWORD=change-me \
  -e ATOMGO_MASTER_KEY=$(openssl rand -base64 32) \
  ghcr.io/atomlust/atomgo:latest
# Tip: drop the two -e lines and AtomGo still starts — first launch serves the Web setup wizard.
```

### First-run setup (Web wizard)

A brand-new AtomGo instance has **no admin account**. Until you complete setup, every page other than `/setup` redirects to the wizard, and all business APIs return `404 setup_required` — so nothing is exposed before you've configured it.

1. Start AtomGo (no env vars needed for a fresh start).
2. Open `http://<host>:7088` in a browser — you'll land on `/setup`.
3. The wizard asks for, step by step:
   - **Database (first step)** — defaults to **SQLite** (local file, zero-config, fine for most deployments). For **MySQL / PostgreSQL**: create an empty database first, then pick the driver in the wizard and fill in the connection details (host / port / user / password / database name, or a full DSN). The wizard validates the connection, writes it to `atomgo.env`, and **auto-restarts** to apply the new database; after the restart it returns to this step with the database locked, and you continue with the admin setup below. **You never have to edit any config file by hand.**
   - **Admin account** — username (default `admin`) + password (8–72 characters).
   - **Public URL** (optional) — the externally reachable base URL; set it now if you run behind a reverse proxy (needed for webhook callbacks / OAuth redirects). You can change it later in Settings.
   - **Credential-vault master key** — choose **Generate** (AtomGo creates a 32-byte key, encrypts it at rest, and shows it **once** — save it somewhere safe; it's required to recover the vault if the data directory moves) or **Provide** your own base64-encoded 32-byte key.
4. Submit. On SQLite the wizard creates the admin, injects the master key, and drops you at the login page; on MySQL/PostgreSQL it auto-restarts after the connection check, and you submit the admin setup once it's back.

> **Non-interactive alternative:** pre-seed `ATOMGO_ADMIN_PASSWORD` and `ATOMGO_MASTER_KEY` (or `ATOMGO_MASTER_KEY_FILE`) before the first start — AtomGo then skips the wizard and boots straight into normal operation. The wizard only appears when no admin exists yet.

### Build from source

```bash
make build          # frontend build → go:embed → single static binary ./atomgo (pure Go, no CGO)
./atomgo --version
```

### Updating

Open **Settings → System** and click "Check for updates" to query the latest release; when a new version is available:

- **Binary deployment**: click "Update now" to auto-download the new version + verify checksum + replace + restart (requires write permission to the binary file; installing to `$HOME/.local/bin` avoids sudo, and a root systemd service installed via `SETUP_SERVICE=1` also satisfies this).
- **Docker deployment**: the container doesn't replace its own image; follow the prompt to run `docker compose pull && docker compose up -d` on the host (the data volume is preserved).

### Configuration (environment variables)

A normal install only needs the first two (plus `ATOMGO_PUBLIC_URL` if you run behind a reverse proxy); everything else has a sane default.

**Core**

| Variable | Description | Default |
|---|---|---|
| `ATOMGO_ADMIN_PASSWORD` | Admin password on first launch | none (must be set) |
| `ATOMGO_MASTER_KEY` | Credential vault master key (base64-encoded 32 bytes); or use `ATOMGO_MASTER_KEY_FILE` to point to a file | vault disabled if unset |
| `ATOMGO_ADDR` | HTTP listen address | `:7088` |
| `ATOMGO_PUBLIC_URL` | Externally reachable base URL (e.g. `https://ci.example.com`). Required for webhook callbacks, OAuth redirects, and signed approval links in notifications | none |
| `ATOMGO_ADMIN_USERNAME` | Admin username on first launch | `admin` |
| `ATOMGO_TRUST_PROXY` | Trust the first `X-Forwarded-For` hop as the audit client IP (`1`/`true`/`yes`/`on`). Leave off unless a trusted reverse proxy sits in front — otherwise anyone can forge audit source IPs | off |
| `ATOMGO_RELEASE_REPO` | GitHub repo queried for update checks (point it at your own release repo; empty falls back to the default — there is no "disable checks" switch) | `atomlust/atomgo` |
| `ATOMGO_RUNTIME` | Set `docker` to declare a container deployment (affects self-update mode); otherwise auto-detected via `/.dockerenv` | auto-detect |

### Git SSH repositories

For Git SSH repositories, use `git@host:group/repo.git` on port 22 or
`ssh://git@host:2424/group/repo.git` for a custom port. Select an SSH key or
SSH password credential in the project form. Before connecting, verify the
server's SSH host key out of band and add it to the AtomGo process
account's `~/.ssh/known_hosts`; alternatively set `SSH_KNOWN_HOSTS` to a
known-hosts file. Unknown or changed host keys are rejected. SSH private keys
are stored only in the encrypted credential vault.

**Database**

| Variable | Description | Default |
|---|---|---|
| `ATOMGO_DB_DRIVER` | Database driver: `sqlite`, `mysql`, or `postgres` | `sqlite` |
| `ATOMGO_DB` | SQLite database path (when driver=sqlite) | `atomgo.db` |
| `ATOMGO_DB_DSN` | MySQL / PostgreSQL DSN (required when driver=mysql or postgres). MySQL uses go-sql-driver format (`user:pw@tcp(host:3306)/db?parseTime=true&charset=utf8mb4`); PostgreSQL uses lib/pq format (`postgres://user:pw@host:5432/db?sslmode=disable`) | none |

> For most cases the default **SQLite** is all you need. To use MySQL / PostgreSQL, **the easiest path is to pick it and fill in the connection details in the first-run Web wizard** — it writes the config to `atomgo.env` and restarts to apply, no manual file editing required. The env vars above only matter if you prefer to set them explicitly via env / docker-compose.

**Runs, reverse proxy & ops**

| Variable | Description | Default |
|---|---|---|
| `ATOMGO_RUNNER` | Run executor: `dag` (canvas-orchestrated) or `legacy` (old fixed flow) | `dag` |
| `ATOMGO_BUILDER` | `auto` / `real` / `stub` (whether to use a container for isolated builds) | `auto` |
| `ATOMGO_MAX_CONCURRENT` | Global cap on simultaneously running runs (excess queues) | worker count (4) |
| `ATOMGO_PROXY_MODE` | `managed` (own Caddy container) or `external` (write a fragment and trigger your Caddy reload) | `managed` |
| `ATOMGO_CADDY_IMAGE` | Reverse-proxy image (wildcard / DNS-01 unsupported, DNS-provider integration removed) | `caddy:2` |
| `ATOMGO_PR_STATUS` | `1` forces PR status reporting on for all projects | off |
| `ATOMGO_ANOMALY_INTERVAL` | Anomaly-detection interval in seconds; `0` disables the timer | `60` |
| `ATOMGO_METRICS_SAMPLE_INTERVAL` | Server-metrics sampling interval in seconds; `0` disables sampling | `60` |

> The remaining advanced variables (cache directories, image GC, PR-status API bases, external Caddy paths, metric retention, audit sink, etc.) and their defaults are defined in the source under `internal/config`.

## Pipeline as code (GitOps)

Commit your pipeline structure to `.atomgo.yml` in the repo — **same source of truth as your code, reviewable in a PR, evolving per branch** — instead of relying on implicit drift in the canvas.

- **Enable**: flip the "Pipeline as code" toggle on the project's pipeline page (per project).
- **How it works**: once enabled, every run reads `.atomgo.yml` from the **repo root** on the **branch being built** (falling back to the project default branch when the branch is empty), and the pipeline spec in that file drives the run. Different branches can carry different `.atomgo.yml`. The file is fetched with the project's bound repo credential (ephemeral; no new exposure).
- **Never breaks a run**: if the file is **missing** → falls back to the pipeline configured in the canvas (UI); if it exists but is **invalid YAML** → also falls back to the stored canvas config.
- **Scope**: the YAML controls **pipeline structure only** (stages / jobs / `needs` / DAG layout). **Variables & cache, environments & credentials, and trigger rules** still come from the canvas (UI) settings — they are **not** in the YAML.
- **Schema** is the same one used by the platform's "Import from YAML" (`version` + `stages` → `jobs`; a job uses a nested `script:` block for `image`/`commands`/`env`/`workdir`).
- **Job types** available in both the canvas and the YAML: `git_source`, `script`, `build_backend`, `build_frontend`, `build_image`, `push_image`, `deploy_ssh`, `deploy_frontend`, `health_check`, `notify`, `templated`, `custom`.

```yaml
version: 1
stages:
  - id: stg_src             # needs references stages by id, so cross-stage deps need an explicit id
    name: Source
    kind: source
    jobs:
      - name: Gitee source
        type: git_source
  - id: stg_build
    name: Build
    kind: build
    needs: [stg_src]
    jobs:
      - name: Run tests
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
    name: Deploy
    kind: deploy
    needs: [stg_build]
    gate: true              # manual approval gate
    when:
      branches: [main, release/*]
    jobs:
      - name: SSH deploy
        type: deploy_ssh
        config:
          targetEnv: prod
```

> You can also force pipeline-as-code on for **all projects** (ignoring the per-project toggle) via the global env var `ATOMGO_PAC_RUNTIME=1`, for back-compat / power users.

## Tech Stack

- **Backend**: Go · Chi (routing) · modernc/sqlite (pure Go, no CGO) + go-sql-driver/mysql + lib/pq (PostgreSQL) · go-git (no host git dependency) · NaCl secretbox (vault) · argon2id + bcrypt · golang.org/x/crypto/ssh (agentless deployment) · coder/websocket (terminals) · Caddy (orchestrated on target hosts for auto HTTPS)
- **Frontend**: Vue 3 `<script setup>` · Vite · naive-ui · OKLCH dual theme · Monaco (read-only code browsing) · Vitest + Playwright · embedded into the binary via `go:embed`

Deliberately dependency-lean: the cron parser, DAG engine, artifact/build caches, and Caddyfile renderer are all in-tree rather than pulled in as libraries.

## Architecture

One process, ~46 domain packages. `internal/httpapi` is the only package that touches HTTP; every domain package is injected into it, and `cmd/atomgo/main.go` is the single wiring layer (including the adapters that keep otherwise-circular packages pointing one way).

```
single static binary (cmd/atomgo)
│
├─ foundation
│  ├── internal/config        env config + master key loading
│  ├── internal/store         DB open + migrations (sqlite / mysql / postgres dialects)
│  ├── internal/auth          auth + sessions + CSRF
│  ├── internal/vault         encrypted credential vault (secretbox)
│  ├── internal/oauth         OAuth app onboarding → token stored as a vault credential
│  ├── internal/gitauth       Git HTTP(S)/SSH credential resolution for repos
│  ├── internal/onboarding   local first-success snapshot for onboarding
│  ├── internal/settings      system settings persistence + env overrides
│  ├── internal/audit         append-only audit + optional remote sink
│  ├── internal/mask          secret redaction (logs / diagnostics / notifications)
│  ├── internal/i18n          server-side localization of user-facing messages
│  └── internal/version       release check + one-click self-update
│
├─ pipelines
│  ├── internal/project       project onboarding + repo detection
│  ├── internal/pipeline      pipeline spec + build/deploy config + validation
│  ├── internal/pipelineyaml  YAML ↔ spec round-trip (import / export)
│  ├── internal/pacloader     pipeline-as-code loader (.atomgo.yml, per branch)
│  ├── internal/library       reusable templates + variable groups + custom nodes
│  ├── internal/trigger       webhook + branch→environment mapping
│  ├── internal/cron          5-field cron parser + minute-granularity scheduler
│  └── internal/chain         upstream→downstream chaining (loop-safe)
│
├─ execution
│  ├── internal/run           run model + worker pool + logs + artifacts + parameters
│  ├── internal/dag           pure DAG scheduling kernel (no I/O)
│  ├── internal/dagrun        DAG run orchestration (stage + job level, matrix expansion)
│  ├── internal/build         isolated container builds + images + stage executor
│  ├── internal/runner        per-project remote build machine config
│  ├── internal/repocache     local bare-mirror repo cache (incremental fetch)
│  ├── internal/buildcache    build dependency cache (branch + lockfile keyed)
│  ├── internal/artifactstore content-addressed artifact bytes
│  ├── internal/testreport    JUnit + Cobertura parsing
│  ├── internal/qualitygate   pure quality-gate evaluation
│  ├── internal/approval      manual approval gates (coordinator + persistence)
│  └── internal/retention     run data retention sweeper
│
├─ delivery
│  ├── internal/target        generic SSH exec/session layer (shared by deploy + ops)
│  ├── internal/deploy        SSH deploy execution + health gating + rollback
│  ├── internal/environments  environments as first-class objects + rollback targets
│  ├── internal/promotion     environment promotion chains + per-env vars
│  ├── internal/prstatus      PR / commit status reporting (GitHub / Gitee)
│  ├── internal/proxy         auto HTTPS + domain reverse proxy (Caddy orchestration)
│
├─ observability
│  ├── internal/notify        multi-channel notifications + event routing + templates
│  ├── internal/anomaly       configurable threshold detection + alerts
│  ├── internal/metrics       server metric time series (trend charts)
│  ├── internal/dora          DORA metrics + performance bands
│  ├── internal/opschat       operation chat sessions (AI ops assistant domain core)
│  └── internal/ai            optional AI: diagnosis, repo analysis, run diff, risk
│
├── internal/httpapi          the sole outward HTTP surface (~184 routes)
└── web/                      Vue 3 frontend (embedded via go:embed)
```

## Project Status

✅ **Officially released** and under active iteration — see the latest version in [Releases](https://github.com/atomlust/atomgo/releases) (tag-driven releases: 6-platform binaries + ghcr multi-arch images). Already running in real production, carrying builds, deployments, and daily ops for multiple projects.

## Contributing

PRs and issues welcome! Before you start, please read [CONTRIBUTING.md](CONTRIBUTING.md) (environment setup / testing / commit conventions) and follow the [Code of Conduct](CODE_OF_CONDUCT.md). For security vulnerabilities, please use the private channel described in [SECURITY.md](SECURITY.md).

## License

MIT — see [LICENSE](LICENSE).

---

<div align="center">
<sub>AtomGo — CI, deployment, and ops in a single binary.</sub>
</div>
