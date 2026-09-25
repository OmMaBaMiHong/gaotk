import { beforeEach, describe, expect, it, vi } from 'vitest'
import { tokenBankAPI } from '../tokenBank'
import { createAccountsAPI, accountsAPI } from '../admin/accounts'
import { createGeminiAPI } from '../admin/gemini'
import { createGrokAPI } from '../admin/grok'
import { createKimiOAuthAPI } from '../admin/kimiOAuth'
import { apiClient } from '../client'

vi.mock('../client', () => ({ apiClient: { get: vi.fn(), post: vi.fn(), put: vi.fn(), patch: vi.fn(), delete: vi.fn() } }))
beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(apiClient.get).mockResolvedValue({ data: {} })
  vi.mocked(apiClient.post).mockResolvedValue({ data: {} })
})
describe('account workspace scopes', () => {
  it('keeps admin defaults while user workspaces use owned account routes', async () => {
    await accountsAPI.getById(7)
    await createAccountsAPI('user').getById(7)
    expect(apiClient.get).toHaveBeenNthCalledWith(1, '/admin/accounts/7')
    expect(apiClient.get).toHaveBeenNthCalledWith(2, '/user/accounts/7', undefined)
  })
  it('scopes dynamic OpenAI and Claude authorization endpoints', async () => {
    const owned = createAccountsAPI('user')
    await owned.generateAuthUrl('/admin/openai/generate-auth-url', {})
    await owned.exchangeCode('/admin/accounts/exchange-code', { session_id: 'session', code: 'code' })
    expect(apiClient.post).toHaveBeenNthCalledWith(1, '/user/openai/generate-auth-url', {}, undefined)
    expect(apiClient.post).toHaveBeenNthCalledWith(2, '/user/accounts/exchange-code', { session_id: 'session', code: 'code' }, undefined)
  })
  it('reuses Gemini, Grok and Kimi authorization APIs in user scope', async () => {
    await createGeminiAPI('user').generateAuthUrl({})
    await createGrokAPI('user').generateAuthUrl({})
    await createKimiOAuthAPI('user').startKimiDeviceFlow()
    expect(vi.mocked(apiClient.post).mock.calls.map(call => call[0])).toEqual(['/user/gemini/oauth/auth-url', '/user/grok/oauth/auth-url', '/user/kimi/oauth/start'])
  })
  it('keeps the savings ledger separate from account management', async () => {
    await tokenBankAPI.revenue(false, { account_id: 7 })
    expect(apiClient.get).toHaveBeenCalledWith('/user/token-bank/revenue', { params: { account_id: 7 } })
  })
})
