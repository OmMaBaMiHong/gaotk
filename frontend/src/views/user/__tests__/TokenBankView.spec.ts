import { flushPromises, shallowMount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

const api = vi.hoisted(() => ({ capabilities: vi.fn(), overview: vi.fn(), revenue: vi.fn() }))
const accounts = vi.hoisted(() => ({ list: vi.fn(), getBatchTodayStats: vi.fn() }))
vi.mock('@/api/tokenBank', () => ({ tokenBankAPI: api }))
vi.mock('@/api/admin/accounts', async (importOriginal) => ({ ...await importOriginal<typeof import('@/api/admin/accounts')>(), createAccountsAPI: () => accounts }))
vi.mock('@/composables/useAccountWorkspace', () => ({ provideAccountWorkspace: vi.fn() }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key, locale: ref('en') }) }))
import TokenBankView from '../TokenBankView.vue'

const render = () => shallowMount(TokenBankView, { global: { stubs: { AppLayout: { template: '<div><slot /></div>' } } } })
beforeEach(() => {
  vi.resetAllMocks()
  api.overview.mockResolvedValue({ today_revenue: 0, total_revenue: 0 })
  api.revenue.mockResolvedValue({ items: [], total: 0 })
  accounts.list.mockResolvedValue({ items: [], total: 0 })
})

describe('Token Bank user availability', () => {
  it('hides enrollment and reports disabled when a stale open page receives 503', async () => {
    api.capabilities.mockRejectedValue({ status: 503, message: 'disabled' })
    const view = render(); await flushPromises()
    expect(view.text()).toContain('tokenBank.disabled')
    expect(view.text()).not.toContain('tokenBank.add')
    expect(accounts.list).not.toHaveBeenCalled()
    expect(view.find('token-bank-showcase-stub').exists()).toBe(false)
    view.unmount()
  })
  it('shows only configured platforms and retains the original create modal', async () => {
    api.capabilities.mockResolvedValue({ platforms: [{ platform: 'openai', account_types: ['oauth'] }] })
    const view = render(); await flushPromises()
    expect(view.findAll('select option').map(item => item.attributes('value'))).toEqual(['', 'openai'])
    expect(view.text()).not.toContain('admin.accounts.dataImportTitle')
    await view.findAll('button').find(item => item.text() === 'tokenBank.add')!.trigger('click')
    expect(view.get('create-account-modal-stub').attributes('show')).toBe('true')
    view.unmount()
  })
})
