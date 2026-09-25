/**
 * Admin Antigravity API endpoints
 * Handles Antigravity (Google Cloud AI Companion) OAuth flows for administrators
 */

import { apiClient as defaultClient } from '../client'
import { createAccountScopeClient, type AccountScope } from '../accountScopeClient'

export interface AntigravityAuthUrlResponse {
  auth_url: string
  session_id: string
  state: string
}

export interface AntigravityAuthUrlRequest {
  proxy_id?: number
}

export interface AntigravityExchangeCodeRequest {
  session_id: string
  state: string
  code: string
  proxy_id?: number
}

export interface AntigravityTokenInfo {
  access_token?: string
  refresh_token?: string
  token_type?: string
  expires_at?: number | string
  expires_in?: number
  project_id?: string
  plan_type?: string
  email?: string
  [key: string]: unknown
}

export function createAntigravityAPI(scope: AccountScope = 'admin') {
const apiClient = scope === 'admin' ? defaultClient : createAccountScopeClient(scope)









async function generateAuthUrl(
  payload: AntigravityAuthUrlRequest
): Promise<AntigravityAuthUrlResponse> {
  const { data } = await apiClient.post<AntigravityAuthUrlResponse>(
    '/admin/antigravity/oauth/auth-url',
    payload
  )
  return data
}

async function exchangeCode(
  payload: AntigravityExchangeCodeRequest
): Promise<AntigravityTokenInfo> {
  const { data } = await apiClient.post<AntigravityTokenInfo>(
    '/admin/antigravity/oauth/exchange-code',
    payload
  )
  return data
}

async function refreshAntigravityToken(
  refreshToken: string,
  proxyId?: number | null
): Promise<AntigravityTokenInfo> {
  const payload: Record<string, any> = { refresh_token: refreshToken }
  if (proxyId) payload.proxy_id = proxyId
  
  const { data } = await apiClient.post<AntigravityTokenInfo>(
    '/admin/antigravity/oauth/refresh-token',
    payload
  )
  return data
}

return { generateAuthUrl, exchangeCode, refreshAntigravityToken }
}

export const antigravityAPI = createAntigravityAPI()
export const {
  generateAuthUrl,
  exchangeCode,
  refreshAntigravityToken
} = antigravityAPI
export default antigravityAPI
