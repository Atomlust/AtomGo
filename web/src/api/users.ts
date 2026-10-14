/**
 * Users API — multi-user management (RBAC).
 *
 * GET    /api/users                     → { users, roles }
 * POST   /api/users                     → user (201)
 * PUT    /api/users/{id}/role           → 204
 * POST   /api/users/{id}/reset-password → 204
 * DELETE /api/users/{id}                → 204
 *
 * All endpoints require the `users:manage` permission (admin only); non-admin
 * callers receive 403.
 */

import { http } from './http'

export type UserRole = 'admin' | 'operator' | 'viewer'

export interface User {
  id: string
  username: string
  role: UserRole
}

export interface UserListResponse {
  users: User[]
  roles: UserRole[]
}

export async function listUsers(): Promise<UserListResponse> {
  return http.get<UserListResponse>('/api/users')
}

export async function createUser(input: {
  username: string
  password: string
  role?: UserRole
}): Promise<User> {
  return http.post<User>('/api/users', input)
}

export async function setUserRole(id: string, role: UserRole): Promise<void> {
  await http.put<void>(`/api/users/${encodeURIComponent(id)}/role`, { role })
}

export async function resetUserPassword(id: string, password: string): Promise<void> {
  await http.post<void>(`/api/users/${encodeURIComponent(id)}/reset-password`, { password })
}

export async function deleteUser(id: string): Promise<void> {
  await http.delete<void>(`/api/users/${encodeURIComponent(id)}`)
}
