/**
 * Reverse-proxy / auto-HTTPS API client (R1 / FR-1..FR-5).
 *
 * A `ProxyRoute` binds one domain on a target host to an upstream container:port.
 * Caddy (managed per-host) issues & renews a Let's Encrypt certificate over
 * HTTP-01 automatically — the user only points an A record at the host. The cert
 * lifecycle is reflected by `certStatus` (pending / issued / failed) and the
 * human-readable `certDetail`.
 *
 * GET    /api/proxy/routes?serverId=<id>        → { items: ProxyRoute[] }
 * POST   /api/proxy/routes                      → ProxyRoute   (needs CSRF)
 * PUT    /api/proxy/routes/{id}                 → ProxyRoute   (needs CSRF)  (R2 / FR-6..FR-9)
 * POST   /api/proxy/routes/{id}/enabled         → ProxyRoute   (needs CSRF)
 * POST   /api/proxy/routes/{id}/refresh         → ProxyRoute   (needs CSRF)
 * DELETE /api/proxy/routes/{id}                 → { ok: true } (needs CSRF)
 * GET    /api/proxy/overview                    → { items: ProxyRouteOverview[] }  (R2 / FR-10)
 * GET    /api/proxy/server-settings?serverId=   → ServerCaddySettings              (档位 B: 按服务器模式)
 * PUT    /api/proxy/server-settings?serverId=   → ServerCaddySettings              (CSRF; 写覆盖行)
 * POST   /api/proxy/server-settings/inject-import?serverId= → ExternalImportResult (CSRF; 确认后注入 import 行)
 *
 * Writes go through the shared `http` wrapper, which auto-attaches the CSRF token
 * (cookie `atomgo_csrf` → `X-CSRF-Token`) and the active UI locale. Never
 * returns secrets.
 */

import { http } from './http'

/** Certificate lifecycle for a route, as reported by the backend / Caddy. */
export type CertStatus = 'pending' | 'issued' | 'failed'

/** TLS mode — only Let's Encrypt auto-issuance in this MVP. */
export type TlsMode = 'auto'

/** Redirect HTTP statuses we support (R2 / FR-9). */
export type RedirectStatus = 301 | 302 | 307 | 308

/** One HTTP redirect rule on a route: `from` path/host → `to` URL with a status. */
export interface Redirect {
  from: string
  to: string
  status: RedirectStatus
}

/**
 * One path-routing rule (R3 / E3.5): requests matching `path` (e.g. `/api/*`) are
 * reverse-proxied to a different upstream than the route's primary (catch-all)
 * binding. `path` must start with `/`.
 */
export interface PathRule {
  /** Path matcher, e.g. `/api/*`. Must start with `/`. */
  path: string
  /** Upstream container this path routes to. */
  upstreamContainer: string
  /** Upstream port inside the container. */
  upstreamPort: number
}

/**
 * One extra upstream behind a route, for load balancing (R4 / E4.2). The route's
 * primary `upstreamContainer:upstreamPort` is the first upstream; each entry here
 * is an *additional* backend Caddy spreads traffic across per `lbPolicy`. Two or
 * more total upstreams enable load balancing + optional active health checks.
 */
export interface Upstream {
  /** Backend container name (resolved on the shared network). */
  container: string
  /** Backend port inside the container. */
  port: number
}

/**
 * Load-balancing policy across the route's upstreams (R4 / E4.2). Mirrors Caddy's
 * `lb_policy`. Only meaningful when ≥2 upstreams exist.
 */
export type LbPolicy = 'round_robin' | 'least_conn' | 'random' | 'first'

/**
 * TCP (L4) passthrough for a route (R4 / E4.3). When set, Caddy's layer-4 app
 * listens on `listenPort` on the host and forwards raw TCP to
 * `upstreamContainer:upstreamPort` — bypassing HTTP routing entirely (no TLS
 * termination, no path rules). Use for databases, message brokers, custom
 * protocols. Cleared (null/omitted) → HTTP-only route.
 */
export interface TcpPassthrough {
  /** Host port the L4 listener binds. */
  listenPort: number
  /** Backend container raw TCP is forwarded to. */
  upstreamContainer: string
  /** Backend port inside the container. */
  upstreamPort: number
}

/**
 * Per-route advanced config (R2 / FR-6..FR-9). Returned on every route; mutated
 * via `updateProxyRoute`. The Basic-Auth password is **write-only** — it is never
 * returned (only `basicAuthEnabled` reflects whether one is set).
 */
export interface ProxyRouteConfig {
  /**
   * Upstream kind: `'container'` reverse-proxies to a running Docker container by
   * name over the shared network; `'address'` forwards straight to the host:port
   * the user typed (an IP / FQDN / `host.docker.internal`). Defaults to `'container'`.
   */
  upstreamKind?: 'container' | 'address'
  /** Extra FQDNs served by the same route (besides `domain`). */
  aliases: string[]
  /** Redirect plain HTTP → HTTPS (default on for `auto` TLS). */
  forceHttps: boolean
  /** Emit `Strict-Transport-Security`. */
  hsts: boolean
  /** Emit hardening headers (X-Content-Type-Options, X-Frame-Options, Referrer-Policy…). */
  securityHeaders: boolean
  /** gzip/zstd response compression. */
  compression: boolean
  /** Basic-Auth username (`''` if auth disabled). */
  basicAuthUser: string
  /** Server-derived: whether a Basic-Auth password hash is stored. Never sent on write. */
  basicAuthEnabled: boolean
  /** IP allow list (CIDRs). Non-empty → only these may reach the upstream. */
  ipAllow: string[]
  /** IP deny list (CIDRs). Matching clients are rejected. */
  ipDeny: string[]
  /** HTTP redirect rules. */
  redirects: Redirect[]
  /**
   * R3 / E3.5: per-path upstream overrides. The route's primary upstream remains
   * the default / catch-all; each rule diverts a matching path prefix elsewhere.
   */
  pathRules?: PathRule[]
  /**
   * R4 / E4.2: additional upstreams behind this route (besides the primary
   * `upstreamContainer:upstreamPort`). Two or more total upstreams enable load
   * balancing. Empty / omitted → single upstream, no LB.
   */
  upstreams?: Upstream[]
  /** R4 / E4.2: load-balancing policy across upstreams. Defaults to round_robin. */
  lbPolicy?: LbPolicy
  /**
   * R4 / E4.2: optional active health-check URI (e.g. `/healthz`). When set, Caddy
   * polls each upstream and routes only to healthy ones. Empty → passive only.
   */
  healthUri?: string
  /** R4 / E4.2: health-check interval (Go duration, e.g. `10s`). Empty → Caddy default. */
  healthInterval?: string
  /**
   * R4 / E4.3: serve the upstream over HTTP/2 cleartext (h2c) — required for plain
   * gRPC backends. WebSocket needs no flag (Caddy upgrades it automatically).
   */
  grpc?: boolean
  /**
   * R4 / E4.3: optional L4 TCP passthrough listener. Separate from HTTP routing —
   * raw TCP on `listenPort` → upstream container:port. null / omitted → HTTP only.
   */
  tcpPassthrough?: TcpPassthrough | null
}

/** One domain → upstream container:port reverse-proxy route on a host. */
export interface ProxyRoute {
  id: string
  serverId: string
  /** Bound FQDN, e.g. `app.mydomain.com`. */
  domain: string
  /** Upstream container name Caddy reverse-proxies to (resolved on the shared network). */
  upstreamContainer: string
  /** Upstream port inside the container, e.g. `8080`. */
  upstreamPort: number
  tlsMode: TlsMode
  /** Whether the route is active (rendered into Caddy and serving). */
  enabled: boolean
  certStatus: CertStatus
  /** Human-readable cert detail: issuer/expiry when issued, the failure reason when failed. Never a secret. */
  certDetail: string
  /** R2 advanced config (multi-domain, access control, hardening, redirects). */
  config: ProxyRouteConfig
  createdAt: string
  updatedAt: string
}

/** A route enriched with its host's display name — for the cross-host overview dashboard. */
export interface ProxyRouteOverview extends ProxyRoute {
  /** Human-readable server name (from the server registry). */
  serverName: string
}

/**
 * Body for `updateProxyRoute`. Send `config` minus the server-derived
 * `basicAuthEnabled`; send `basicAuthPassword` **only** when the user typed a new
 * one (omit/empty keeps the existing password). Upstream fields are optional —
 * omit to leave the binding unchanged.
 */
export interface UpdateProxyRouteInput {
  upstreamContainer?: string
  upstreamPort?: number
  config: Omit<ProxyRouteConfig, 'basicAuthEnabled'>
  /** New Basic-Auth password (write-only). Omit / `''` to keep the existing one. */
  basicAuthPassword?: string
}

/** Body for creating a route. Upstream port defaults to the container's exposed port when known. */
export interface CreateProxyRouteInput {
  serverId: string
  domain: string
  /**
   * Upstream target: a running container name when `config.upstreamKind` is
   * `'container'`, or the host (IP / FQDN / `host.docker.internal`) the user typed
   * when it is `'address'`.
   */
  upstreamContainer: string
  upstreamPort: number
  /**
   * Partial advanced config sent on create. For the simple bind form this carries
   * only `upstreamKind` (`'container'` | `'address'`); the backend fills the rest
   * with defaults. Omit to default to a container upstream.
   */
  config?: Partial<ProxyRouteConfig>
}

/** List all reverse-proxy routes bound on a single server. */
export async function listProxyRoutes(serverId: string): Promise<ProxyRoute[]> {
  const res = await http.get<{ items: ProxyRoute[] }>(
    `/api/proxy/routes?serverId=${encodeURIComponent(serverId)}`,
  )
  return res.items ?? []
}

/** Bind a new domain → container:port route (FR-1). Returns the created route. */
export async function createProxyRoute(input: CreateProxyRouteInput): Promise<ProxyRoute> {
  return http.post<ProxyRoute>('/api/proxy/routes', input)
}

/** Enable / disable a route (FR-5). Re-renders Caddy and reloads. Returns the updated route. */
export async function setProxyRouteEnabled(id: string, enabled: boolean): Promise<ProxyRoute> {
  return http.post<ProxyRoute>(`/api/proxy/routes/${encodeURIComponent(id)}/enabled`, { enabled })
}

/**
 * Re-check / retry a route's certificate (FR-4): re-runs the ACME order so a
 * fixed DNS / port can be picked up. Returns the route with refreshed certStatus.
 */
export async function refreshProxyRoute(id: string): Promise<ProxyRoute> {
  return http.post<ProxyRoute>(`/api/proxy/routes/${encodeURIComponent(id)}/refresh`, {})
}

/**
 * Update a route's upstream binding and/or advanced config (R2 / FR-6..FR-9).
 * Re-renders Caddy and reloads. Returns the updated route. The Basic-Auth
 * password is write-only — only pass `basicAuthPassword` when the user typed a
 * new one (an empty / omitted value keeps the existing password).
 */
export async function updateProxyRoute(id: string, body: UpdateProxyRouteInput): Promise<ProxyRoute> {
  return http.put<ProxyRoute>(`/api/proxy/routes/${encodeURIComponent(id)}`, body)
}

/** Delete a route (FR-5). Removes it from Caddy and reloads; the cert volume is kept. */
export async function deleteProxyRoute(id: string): Promise<{ ok: boolean }> {
  return http.delete<{ ok: boolean }>(`/api/proxy/routes/${encodeURIComponent(id)}`)
}

/**
 * Status of the `atomgo-caddy` reverse-proxy container on one host (R4).
 * The reverse proxy is a real Docker container AtomGo runs ON the target
 * host — this surfaces that fact for awareness + consent + reversibility:
 * whether it exists, whether it's running, and what it's occupying.
 */
export interface CaddyStatus {
  /** Host the status is for. */
  serverId: string
  /**
   * Reverse-proxy run mode: `managed` (AtomGo owns a `atomgo-caddy` container
   * occupying 80/443) or `external` (the host already runs a user-managed
   * Caddy that AtomGo drives via an imported fragment).
   */
  mode: 'managed' | 'external'
  /** Whether the `atomgo-caddy` container exists on the host. */
  installed: boolean
  /** Whether that container is currently running. */
  running: boolean
  /** Container image, e.g. `caddy:2`. `''` if unknown. */
  image: string
  /** Host ports it occupies, e.g. `80,443`. `''` if unknown. */
  ports: string
  /** Number of reverse-proxy routes bound on this host. */
  routeCount: number
}

/**
 * Read-only snapshot of the reverse-proxy run mode + external-mode paths/commands.
 * Used to show the "external Caddy" hint (fragment dir, main Caddyfile path,
 * reload command) so the user can wire it up on their existing Caddy.
 *
 * GET /api/proxy/mode → ProxyModeInfo
 */
export interface ProxyModeInfo {
  /** `managed` | `external`. */
  mode: 'managed' | 'external'
  /** Main Caddyfile path AtomGo reloads in external mode. */
  configPath: string
  /** Directory where AtomGo writes the reverse-proxy fragment. */
  fragmentDir: string
  /** Reload command AtomGo runs in external mode. */
  reloadCmd: string
  /**
   * External-mode user Caddy container name (containerized Caddy scenario). Empty = host-file mode
   * (the Caddyfile / fragment dir must be bind-mounted to the host). When set, all file ops + the
   * default reload go through `docker exec <container>`, so no bind-mount is needed.
   */
  externalCaddyContainer: string
  /** Managed-mode Caddy Docker network (global default `atomgo-proxy`). */
  network: string
  /** Managed-mode Caddy container name (global default `atomgo-caddy`). */
  containerName: string
  /** Managed-mode Caddy data volume (global default). */
  dataVolume: string
  /** Managed-mode Caddy config volume (global default). */
  configVolume: string
}

/**
 * Managed-mode Caddy resource overrides (docker network / container name /
 * data & config volumes), carried inline on the deploy request so "save +
 * deploy" is one action. Empty fields fall back to the global defaults.
 */
export interface CaddyResources {
  network?: string
  containerName?: string
  dataVolume?: string
  configVolume?: string
}

/**
 * Read the reverse-proxy environment status on a host (R4): whether the
 * `atomgo-caddy` container exists, is running, and what it occupies. Drives
 * the awareness card and gates the first-start consent dialog.
 *
 * GET /api/proxy/caddy?serverId=<id> → CaddyStatus
 */
export async function getCaddyStatus(serverId: string): Promise<CaddyStatus> {
  return http.get<CaddyStatus>(`/api/proxy/caddy?serverId=${encodeURIComponent(serverId)}`)
}

/**
 * Deploy the reverse-proxy environment on a host (R4 · explicit consent path):
 * starts the `atomgo-caddy` container so it's running and ready before any
 * domain is bound. User-triggered from the awareness card's "deploy" button —
 * the same act otherwise done implicitly on first bind, surfaced explicitly here.
 * Returns the refreshed status (should reflect installed + running).
 *
 * `resources` (optional, managed mode): per-host overrides for the docker
 * network / container name / data & config volumes. When any field is set the
 * backend persists them first and deploys in the same request — "set + deploy"
 * in one click; empty/omitted deploys with the already-saved settings.
 *
 * POST /api/proxy/caddy?serverId=<id> → CaddyStatus   (needs CSRF)
 */
export async function prepareCaddyEnv(serverId: string, resources?: CaddyResources): Promise<CaddyStatus> {
  const body = resources?.network || resources?.containerName || resources?.dataVolume || resources?.configVolume
    ? resources
    : {}
  return http.post<CaddyStatus>(`/api/proxy/caddy?serverId=${encodeURIComponent(serverId)}`, body)
}

/**
 * Remove the reverse-proxy environment on a host (R4 · reversibility): stops and
 * deletes the `atomgo-caddy` container. The certificate volume is kept, so
 * re-enabling a domain restores HTTPS without re-issuing from scratch. Bound
 * domains on this host stop serving HTTPS until the env is brought back up.
 *
 * DELETE /api/proxy/caddy?serverId=<id> → { ok: true }   (needs CSRF)
 */
export async function removeCaddyEnv(serverId: string): Promise<{ ok: boolean }> {
  return http.delete<{ ok: boolean }>(`/api/proxy/caddy?serverId=${encodeURIComponent(serverId)}`)
}

/**
 * Read the reverse-proxy run mode + external-mode paths/commands. Drives the
 * "external Caddy" hint. No CSRF; read-only.
 *
 * GET /api/proxy/mode → ProxyModeInfo
 */
export async function getProxyMode(): Promise<ProxyModeInfo> {
  return http.get<ProxyModeInfo>('/api/proxy/mode')
}

/**
 * Per-server reverse-proxy settings (档位 B): the *effective* mode + external
 * paths/commands for one host, after merging the global env default with any
 * per-server override row. Lets the UI show + change a host's mode.
 *
 * GET /api/proxy/server-settings?serverId=<id> → ServerCaddySettings
 */
export interface ServerCaddySettings {
  serverID: string
  /** Effective mode after merging global default + per-server override. */
  mode: 'managed' | 'external'
  /** Main Caddyfile path AtomGo reloads in external mode. */
  configPath: string
  /** Directory where AtomGo writes the reverse-proxy fragment. */
  fragmentDir: string
  /** Reload command AtomGo runs in external mode. */
  reloadCmd: string
  /** External-mode user Caddy container name (effective value; empty = host-file mode). */
  externalCaddyContainer: string
  /** Managed-mode Caddy Docker network (effective value; empty = global default). */
  network: string
  /** Managed-mode Caddy container name (effective value; empty = global default). */
  containerName: string
  /** Managed-mode Caddy data volume (effective value; empty = global default). */
  dataVolume: string
  /** Managed-mode Caddy config volume (effective value; empty = global default). */
  configVolume: string
}

/**
 * Result of the "auto-inject import line" action for external-mode hosts.
 * `present` tells the UI whether the user's main Caddyfile already imports the
 * fragment; `applied` is true only when an actual write happened.
 */
export interface ExternalImportResult {
  /** Absolute path of the user's main Caddyfile. */
  file: string
  /** The exact `import …` line that should (or does) appear in that file. */
  line: string
  /** Whether the main Caddyfile already contains the import line. */
  present: boolean
  /** Whether this call actually appended the line (false on preview, or if already present). */
  applied: boolean
  /** Non-fatal reload warning (empty = clean); only meaningful on apply. */
  reloadWarning: string
}

/** Read a host's effective reverse-proxy settings (no CSRF; read-only). */
export async function getServerCaddySettings(serverId: string): Promise<ServerCaddySettings> {
  return http.get<ServerCaddySettings>(`/api/proxy/server-settings?serverId=${encodeURIComponent(serverId)}`)
}

/**
 * Write a host's per-server reverse-proxy override (full override; an empty
 * field falls back to the global default). Needs CSRF.
 *
 * PUT /api/proxy/server-settings?serverId=<id> → ServerCaddySettings
 */
export async function setServerCaddySettings(
  serverId: string,
  body: {
    mode?: 'managed' | 'external'
    configPath?: string
    fragmentDir?: string
    reloadCmd?: string
    /**
     * External-mode user Caddy container name (empty = fall back to host-file mode). When set,
     * all file ops + the default reload go through `docker exec <container>` (no bind-mount needed).
     */
    externalCaddyContainer?: string
    /** Managed-mode Caddy Docker network (empty = fall back to global default). */
    network?: string
    /** Managed-mode Caddy container name (empty = fall back to global default). */
    containerName?: string
    /** Managed-mode Caddy data volume (empty = fall back to global default). */
    dataVolume?: string
    /** Managed-mode Caddy config volume (empty = fall back to global default). */
    configVolume?: string
  },
): Promise<ServerCaddySettings> {
  return http.put<ServerCaddySettings>(`/api/proxy/server-settings?serverId=${encodeURIComponent(serverId)}`, body)
}

/**
 * Auto-inject the AtomGo fragment `import` line into the host's main Caddyfile
 * (external mode only). `confirm=false` → preview (no write); `confirm=true`
 * → actually append the line. Needs CSRF. The UI must show a confirmation
 * dialog before calling this with `confirm=true`.
 *
 * POST /api/proxy/server-settings/inject-import?serverId=<id> → ExternalImportResult
 */
export async function injectExternalImport(serverId: string, confirm: boolean): Promise<ExternalImportResult> {
  return http.post<ExternalImportResult>(
    `/api/proxy/server-settings/inject-import?serverId=${encodeURIComponent(serverId)}`,
    { confirm },
  )
}

/**
 * Cross-host / cross-domain certificate overview (R2 / FR-10): every route on
 * every reachable host, each enriched with its server's display name. Powers the
 * cert dashboard's table + summary cards. Read-only aggregate.
 */
export async function getProxyOverview(): Promise<ProxyRouteOverview[]> {
  const res = await http.get<{ items: ProxyRouteOverview[] }>('/api/proxy/overview')
  return res.items ?? []
}

