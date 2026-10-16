/**
 * Pipeline build/deploy settings API — aligns to frozen 2.4 contract.
 *
 * GET /api/projects/{id}/pipeline/settings  → SettingsDTO  (lazily creates default on first access)
 * PUT /api/projects/{id}/pipeline/settings  → SettingsDTO  (needs CSRF)
 *
 * Independent of the 2.2 pipeline spec. Secret variables / registry credentials are
 * stored as a credentialId reference only — the server never returns plaintext, only
 * a server-computed maskedValue. On save, secret items send {key,secret:true,credentialId}
 * (no plaintext / mask).
 */

import { http } from './http'

// ─── Enums (frozen) ──────────────────────────────────────────────────────────

export type BuildModel = 'dockerfile' | 'toolchain'
export type ArtifactType = 'image' | 'jar' | 'dist'
export type RegistryType = 'harbor' | 'acr' | 'dockerhub' | 'custom'

// ─── Domain types (frozen DTO shape) ─────────────────────────────────────────

export interface BuildVar {
  id: string
  key: string
  secret: boolean
  /** Present only for non-secret vars. */
  value?: string
  /** Present only for secret vars — vault reference. */
  credentialId?: string
  /** Server-computed mask for secret vars, e.g. "••••_zzz" — never plaintext. */
  maskedValue?: string
}

export interface Toolchain {
  language: string
  version: string
}

export interface Cache {
  enabled: boolean
  paths: string[]
}

export interface BuildConfig {
  model: BuildModel
  dockerfilePath: string
  toolchain: Toolchain
  artifactType: ArtifactType
  vars: BuildVar[]
  cache: Cache
  /** Project-level image repository name (e.g. "org/app"); empty = derived from project name slug. */
  imageName?: string
}

export interface ImageRegistry {
  /** Empty string means "not bound yet". */
  type: RegistryType | ''
  url: string
  credentialId?: string
  maskedCredential?: string
}

export interface Environment {
  id: string
  name: string
  /** Whether this environment participates in the promotion chain (derived chain = in-chain envs, in array order). */
  inChain: boolean
  /** Whether promoting to this environment requires manual approval (only meaningful when inChain). */
  gated: boolean
  /** Approvers allowed to approve promotion into this env (empty = any identity). Aligns to GitHub required reviewers / GitLab allowed-to-deploy. */
  approvers: string[]
  targetServerIds: string[]
  envVars: BuildVar[]
  imageRegistry: ImageRegistry
  /** Moving channel tag pushed alongside the immutable commit tag on successful build (empty = disabled). */
  channelTag?: string
  /** Per-environment container name for promotion deploy (docker run --name). Empty → image-name fallback. */
  containerName?: string
  /** Per-environment port mappings (comma/space separated, each "host:container") → docker run -p. */
  ports?: string
  /** Per-environment extra docker run args (quote-aware split), e.g. --restart / -v / --network. */
  runArgs?: string
}

export interface SettingsDTO {
  build: BuildConfig
  environments: Environment[]
  updatedAt: string
}

// ─── Save request types (secret items send only credentialId; no plaintext/mask) ──

export interface SaveBuildVar {
  id?: string
  key: string
  secret: boolean
  value?: string
  credentialId?: string
}

export interface SaveImageRegistry {
  type: RegistryType | ''
  url: string
  credentialId?: string
}

export interface SaveEnvironment {
  id?: string
  name: string
  inChain: boolean
  gated: boolean
  approvers: string[]
  targetServerIds: string[]
  envVars: SaveBuildVar[]
  imageRegistry: SaveImageRegistry
  channelTag?: string
  containerName?: string
  ports?: string
  runArgs?: string
}

export interface SaveSettingsInput {
  build: {
    model: BuildModel
    dockerfilePath?: string
    toolchain?: Toolchain
    artifactType: ArtifactType
    vars: SaveBuildVar[]
    cache: Cache
    imageName?: string
  }
  environments: SaveEnvironment[]
}

// ─── API functions ────────────────────────────────────────────────────────────

export async function getSettings(projectId: string): Promise<SettingsDTO> {
  return http.get<SettingsDTO>(`/api/projects/${projectId}/pipeline/settings`)
}

export async function saveSettings(
  projectId: string,
  input: SaveSettingsInput,
): Promise<SettingsDTO> {
  return http.put<SettingsDTO>(`/api/projects/${projectId}/pipeline/settings`, input)
}
