/**
 * Admin OAuth Clients API endpoints（OAuth 授权应用管理）
 */

import { apiClient } from '../client'

export interface OAuthClientView {
  id: number
  name: string
  client_id: string
  secret_last4: string
  redirect_uris: string[]
  allow_localhost: boolean
  enabled: boolean
  remark: string
  created_at: string
  updated_at: string
}

export interface CreateOAuthClientRequest {
  name: string
  client_id: string
  client_secret?: string
  redirect_uris: string[]
  allow_localhost?: boolean
  remark?: string
}

export interface UpdateOAuthClientRequest {
  name?: string
  client_secret?: string
  redirect_uris?: string[]
  allow_localhost?: boolean
  enabled?: boolean
  remark?: string
  regenerate_secret?: boolean
}

export async function list(): Promise<{ items: OAuthClientView[] }> {
  const { data } = await apiClient.get<{ items: OAuthClientView[] }>('/admin/oauth-clients')
  return data
}

export async function create(
  request: CreateOAuthClientRequest
): Promise<{ client: OAuthClientView; client_secret: string }> {
  const { data } = await apiClient.post<{ client: OAuthClientView; client_secret: string }>(
    '/admin/oauth-clients',
    request
  )
  return data
}

export async function update(
  id: number,
  request: UpdateOAuthClientRequest
): Promise<{ client: OAuthClientView; client_secret?: string }> {
  const { data } = await apiClient.put<{ client: OAuthClientView; client_secret?: string }>(
    `/admin/oauth-clients/${id}`,
    request
  )
  return data
}

export async function remove(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.delete<{ message: string }>(`/admin/oauth-clients/${id}`)
  return data
}

export default {
  list,
  create,
  update,
  remove
}
