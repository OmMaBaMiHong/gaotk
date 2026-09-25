import { apiClient } from './client'

export interface RentalPolicy {
  id?: number
  platform: string
  group_id: number
  owner_share_bps: number
  admin_user_id: number
  enabled: boolean
  version?: number
}
export interface RentalAccount {
  id: number
  owner_user_id: number
  name: string
  platform: string
  type: string
  status: string
  rental_status: string
  schedulable: boolean
  requests: number
  tokens: number
  revenue: number
}
export interface RentalOverview {
  accounts: RentalAccount[]
  total_accounts: number
  total_revenue: number
  today_revenue: number
  admin_revenue: number
}
export interface RentalRevenue {
  id: number
  account_id: number
  owner_user_id: number
  platform: string
  model: string
  billing_type: number
  input_tokens: number
  output_tokens: number
  cache_tokens: number
  bill_amount: number
  owner_share_bps: number
  owner_amount: number
  admin_amount: number
  created_at: string
}
export interface RevenuePage {
  items: RentalRevenue[]
  total: number
}
export interface RentalInput {
  name: string
  platform: string
  api_key?: string
  account_id?: number
}
export interface RentalAuth {
  auth_url: string
  session_id: string
}

export const tokenBankAPI = {
  policies: async (admin: boolean) =>
    (
      await apiClient.get<RentalPolicy[]>(
        `/${admin ? 'admin' : 'user'}/token-bank/policies`
      )
    ).data,
  overview: async (admin: boolean, params: Record<string, unknown>) =>
    (
      await apiClient.get<RentalOverview>(
        `/${admin ? 'admin' : 'user'}/token-bank/accounts`,
        { params }
      )
    ).data,
  revenue: async (admin: boolean, params: Record<string, unknown>) =>
    (
      await apiClient.get<RevenuePage>(
        `/${admin ? 'admin' : 'user'}/token-bank/revenue`,
        { params }
      )
    ).data,
  savePolicy: async (policy: RentalPolicy) =>
    apiClient.put('/admin/token-bank/policies', policy),
  importAccount: async (input: RentalInput) =>
    apiClient.post('/user/token-bank/accounts', input),
  startOAuth: async (input: RentalInput) =>
    (await apiClient.post<RentalAuth>('/user/token-bank/oauth/start', input))
      .data,
  finishOAuth: async (session_id: string, code: string, state: string) =>
    apiClient.post('/user/token-bank/oauth/finish', {
      session_id,
      code,
      state
    }),
  setStatus: async (id: number, status: string) =>
    apiClient.patch(`/user/token-bank/accounts/${id}/status`, { status })
}

export function parseRentalCallback(
  value: string,
  state: string
): { code: string; state: string } {
  const input = value.trim()
  if (input.startsWith('http://') || input.startsWith('https://')) {
    const url = new URL(input)
    return {
      code: url.searchParams.get('code') || '',
      state: url.searchParams.get('state') || ''
    }
  }
  const parts = input.split('#')
  return { code: parts[0], state: parts[1] || state.trim() }
}
