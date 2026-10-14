/**
 * Promotion API — Epic 8 · Story 8-7 (FR-8-7).
 *
 * GET  /api/projects/{id}/environments       → EnvironmentsResponse (derived chain)
 * POST /api/runs/{id}/promote                → PromotionDTO                  (CSRF)
 * GET  /api/runs/{id}/promotions             → { items: PromotionDTO[] }
 * GET  /api/projects/{id}/promotions         → { items: PromotionDTO[] }
 *
 * The environment chain is no longer configured via a separate endpoint. It is
 * *derived* from the project's pipeline settings (Environment.inChain / gated,
 * ordered by array position) — see api/pipelineSettings. This client therefore
 * only reads the derived chain here; per-environment variables live in
 * pipeline_settings and are fetched via GET /api/projects/{id}/pipeline/settings.
 *
 * Gated environments enter a waiting-approval state on promote; the caller
 * reuses the existing /runs/{id}/approve|reject endpoints (see api/runs.ts).
 * A 409 is returned for already-promoted / gated-rejected promotions — the
 * response body is still a PromotionDTO (not an error envelope) so callers
 * should read the record's `status` field.
 */

import { http } from './http'

// ─── Domain types ─────────────────────────────────────────────────────────────

/** One stage in the ordered environment chain (derived from pipeline settings). */
export interface EnvStage {
  name: string
  /** Whether this stage requires manual approval before promotion proceeds. */
  gated: boolean
}

/** Full response from GET /api/projects/{id}/environments (the derived chain). */
export interface EnvironmentsResponse {
  environments: EnvStage[]
}

/** Promotion record DTO (matches Go's promotionDTO camelCase json tags). */
export interface PromotionDTO {
  id: string
  projectId: string
  sourceRunId: string
  fromEnvironment: string
  targetEnvironment: string
  /** "pending" | "promoted" | "rejected" */
  status: PromotionStatus
  promotedBy: string
  createdAt: string
  decidedAt: string
}

export type PromotionStatus = 'pending' | 'promoted' | 'rejected'

/** One target server's deploy result (mirrors the run-detail targets DTO shape). */
export interface DeployTargetDTO {
  serverId: string
  serverName: string
  /** pending | deploying | success | failed | rolled_back */
  status: string
  message: string
  startedAt: string
  finishedAt?: string
}

/**
 * Response from POST /api/runs/{id}/promote — the promotion record plus (when the
 * promotion was approved) the target-environment deploy outcome.
 *
 * On approval the backend deploys the source run's artifact to the *target
 * environment's* servers and records it under an independent "promotion deploy"
 * run (`deployRunId`), so the source run's own deploy record is not overwritten.
 * If the environment has no target servers / the run has no deployable artifact,
 * `deploySkipped` explains why (the promotion record still stands).
 */
export interface PromoteResultDTO extends PromotionDTO {
  deployRunId?: string
  deployTargets?: DeployTargetDTO[]
  deploySkipped?: string
  /**
   * Environment-scoped variable keys that could NOT be injected into the target
   * machines (e.g. a secret whose credential is missing / vault unconfigured).
   * Never contains plaintext values — only the key names, for warning/audit.
   */
  deployEnvUnresolved?: string[]
}

// ─── Input types ──────────────────────────────────────────────────────────────

export interface PromoteRunInput {
  /** Leave empty to auto-advance to the next environment in the chain. */
  targetEnvironment?: string
}

// ─── API functions ────────────────────────────────────────────────────────────

export async function getEnvironments(projectId: string): Promise<EnvironmentsResponse> {
  return http.get<EnvironmentsResponse>(`/api/projects/${projectId}/environments`)
}

export async function promoteRun(
  runId: string,
  input: PromoteRunInput = {},
): Promise<PromoteResultDTO> {
  return http.post<PromoteResultDTO>(`/api/runs/${runId}/promote`, input)
}

export async function listRunPromotions(runId: string): Promise<{ items: PromotionDTO[] }> {
  return http.get<{ items: PromotionDTO[] }>(`/api/runs/${runId}/promotions`)
}

export async function listProjectPromotions(
  projectId: string,
): Promise<{ items: PromotionDTO[] }> {
  return http.get<{ items: PromotionDTO[] }>(`/api/projects/${projectId}/promotions`)
}
