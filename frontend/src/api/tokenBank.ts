import { apiClient } from './client'

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
export interface TokenBankShowcase {
  enabled: boolean
  leaderboard: { rank: number; name: string; amount: number }[]
  recent: { name: string; platform: string; amount: number; created_at: string }[]
  updated_at: string
}
export interface TokenBankCapabilities {
  platforms: { platform: string; account_types: string[] }[]
}
export const tokenBankAPI = {
  capabilities: async () => (await apiClient.get<TokenBankCapabilities>('/token-bank/capabilities')).data,
  config: async () => (await apiClient.get<{ enabled: boolean }>('/admin/token-bank/config')).data,
  updateConfig: async (enabled: boolean) => (await apiClient.put<{ enabled: boolean }>('/admin/token-bank/config', { enabled })).data,
  showcase: async (signal?: AbortSignal) =>
    (await apiClient.get<TokenBankShowcase>('/token-bank/showcase', { signal })).data,
  showcaseSettings: async () =>
    (await apiClient.get<{ enabled: boolean }>('/admin/token-bank/showcase')).data,
  updateShowcaseSettings: async (enabled: boolean) =>
    (await apiClient.put<{ enabled: boolean }>('/admin/token-bank/showcase', { enabled })).data,
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
    ).data
}
