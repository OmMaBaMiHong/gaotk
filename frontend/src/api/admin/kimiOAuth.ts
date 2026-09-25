/**
 * Admin Kimi OAuth API endpoints
 * Handles Kimi Code device-authorization-flow (OAuth 2.0 device flow) for enterprise seats.
 */

import { apiClient as defaultClient } from '../client'
import { createAccountScopeClient, type AccountScope } from '../accountScopeClient'
import type { Account } from '@/types'

export interface KimiOAuthCapabilities {
  service: string
  client_id: string
  scope: string
  verification_base: string
}

export interface KimiStartDeviceFlowResult {
  session_id: string
  user_code: string
  verification_uri: string
  verification_uri_complete: string
  expires_in: number
  interval: number
}

export interface KimiPollDeviceFlowResult {
  pending: boolean
  token_info?: {
    access_token: string
    refresh_token: string
    expires_in: number
    expires_at: number
    token_type: string
    scope: string
  }
}

/**
 * Fetch Kimi OAuth capability info.
 */
export function createKimiOAuthAPI(scope: AccountScope = 'admin') {
const apiClient = scope === 'admin' ? defaultClient : createAccountScopeClient(scope)
async function getKimiCapabilities(): Promise<KimiOAuthCapabilities> {
  const { data } = await apiClient.get<KimiOAuthCapabilities>('/admin/kimi/oauth/capabilities')
  return data
}

/**
 * Kick off a Kimi device authorization. Returns the user_code + verification URL the
 * admin must open to approve, plus a session_id used to poll.
 */
async function startKimiDeviceFlow(): Promise<KimiStartDeviceFlowResult> {
  const { data } = await apiClient.post<KimiStartDeviceFlowResult>('/admin/kimi/oauth/start')
  return data
}

/**
 * Poll the Kimi device authorization result for a session.
 * `pending=true` means the user has not approved yet.
 */
async function pollKimiDeviceFlow(
  sessionId: string
): Promise<KimiPollDeviceFlowResult> {
  const { data } = await apiClient.post<KimiPollDeviceFlowResult>('/admin/kimi/oauth/poll', {
    session_id: sessionId,
  })
  return data
}

/**
 * Finalize: fetch the authorized token for the session and create a Kimi OAuth account.
 */
async function createKimiAccountFromOAuth(payload: {
  session_id: string
  name?: string
  concurrency?: number
  priority?: number
  group_ids?: number[]
}): Promise<Account> {
  const { data } = await apiClient.post<Account>('/admin/kimi/create-from-oauth', payload)
  return data
}
return { getKimiCapabilities, startKimiDeviceFlow, pollKimiDeviceFlow, createKimiAccountFromOAuth }

}
export const kimiOAuthAPI = createKimiOAuthAPI()
export const { getKimiCapabilities, startKimiDeviceFlow, pollKimiDeviceFlow, createKimiAccountFromOAuth } = kimiOAuthAPI
