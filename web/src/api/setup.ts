/**
 * Setup API — 首次启动 Web 初始化向导(对齐 internal/httpapi/setup.go 契约)。
 *
 * GET  /api/setup/status → SetupStatus(setup 模式期间附带一次性 token)
 * POST /api/setup/submit → SetupResult(自动生成主密钥时回显一次)
 *
 * 这两个端点在「setup 门」白名单内,即使在初始化未完成(无管理员)期间也可访问,
 * 且豁免 CSRF(与 /api/auth/* 同理,此时尚无会话)。
 */

import { http } from './http'

export interface SetupDefaults {
  username: string
  publicUrl: string
}

export interface SetupStatus {
  setupRequired: boolean
  vaultConfigured: boolean
  defaults: SetupDefaults
  /** 仅 setupRequired 期间非空;提交时需原样回传。 */
  token: string
  /** 数据库是否已由 atomgo.env 固化(选过 MySQL/PostgreSQL 并保存后为 true)。 */
  dbConfigured: boolean
  /** 当前生效的数据库驱动(sqlite/mysql/postgres),仅信息展示。 */
  dbDriver: string
}

export interface SetupSubmitResult {
  ok: boolean
  /** 仅自动生成主密钥时非空(一次性回显,务必提示用户保存)。 */
  masterKey?: string
  /** true 表示数据库配置已固化,进程正在自重启以应用,前端应轮询直至 dbConfigured=true。 */
  restartRequired?: boolean
}

export interface SetupSubmitInput {
  token: string
  username: string
  password: string
  /** "generate"(默认/空)或 base64 编码的 32 字节。 */
  masterKey: string
  publicUrl: string
  /** 数据库驱动:"sqlite"(默认)| "mysql" | "postgres"。 */
  dbDriver: string
  dbHost: string
  dbPort: string
  dbUser: string
  dbPassword: string
  dbName: string
  /** 可选:直接提供完整 DSN,优先于上面各字段。 */
  dbDSN: string
}

export async function fetchSetupStatus(): Promise<SetupStatus> {
  return http.get<SetupStatus>('/api/setup/status')
}

export async function submitSetup(input: SetupSubmitInput): Promise<SetupSubmitResult> {
  return http.post<SetupSubmitResult>('/api/setup/submit', input)
}
