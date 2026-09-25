/**
 * Admin Gemini API endpoints
 * Handles Gemini OAuth flows for administrators
 */

import { apiClient as defaultClient } from '../client'
import { createAccountScopeClient, type AccountScope } from '../accountScopeClient'

export interface GeminiAuthUrlResponse {
  auth_url: string
  session_id: string
  state: string
}

export interface GeminiOAuthCapabilities {
  ai_studio_oauth_enabled: boolean
  required_redirect_uris: string[]
}

export interface GeminiAuthUrlRequest {
  proxy_id?: number
  project_id?: string
  oauth_type?: 'code_assist' | 'google_one' | 'ai_studio'
  tier_id?: string
}

export interface GeminiExchangeCodeRequest {
  session_id: string
  state: string
  code: string
  proxy_id?: number
  oauth_type?: 'code_assist' | 'google_one' | 'ai_studio'
  tier_id?: string
}

export type GeminiTokenInfo = {
  access_token?: string
  refresh_token?: string
  token_type?: string
  scope?: string
  expires_in?: number
  expires_at?: number
  project_id?: string
  oauth_type?: string
  tier_id?: string
  extra?: Record<string, unknown>
  [key: string]: unknown
}

export function createGeminiAPI(scope: AccountScope = 'admin') {
const apiClient = scope === 'admin' ? defaultClient : createAccountScopeClient(scope)











async function generateAuthUrl(
  payload: GeminiAuthUrlRequest
): Promise<GeminiAuthUrlResponse> {
  const { data } = await apiClient.post<GeminiAuthUrlResponse>(
    '/admin/gemini/oauth/auth-url',
    payload
  )
  return data
}

async function exchangeCode(payload: GeminiExchangeCodeRequest): Promise<GeminiTokenInfo> {
  const { data } = await apiClient.post<GeminiTokenInfo>(
    '/admin/gemini/oauth/exchange-code',
    payload
  )
  return data
}

async function getCapabilities(): Promise<GeminiOAuthCapabilities> {
  const { data } = await apiClient.get<GeminiOAuthCapabilities>('/admin/gemini/oauth/capabilities')
  return data
}

return { generateAuthUrl, exchangeCode, getCapabilities }
}

export const geminiAPI = createGeminiAPI()
export const {
  generateAuthUrl,
  exchangeCode,
  getCapabilities
} = geminiAPI
export default geminiAPI
