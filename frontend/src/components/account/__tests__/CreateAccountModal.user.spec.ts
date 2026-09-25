import { defineComponent, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

const { api, create, workspace } = vi.hoisted(() => {
  const create = vi.fn().mockResolvedValue({ id: 7, platform: 'openai', type: 'apikey' })
  return { workspace: { isAdmin: false }, create, api: { accounts: { create }, settings: { getWebSearchEmulationConfig: vi.fn(), getSettings: vi.fn() }, tlsFingerprintProfiles: { list: vi.fn() } } }
})
vi.mock('@/composables/useAccountWorkspace', () => ({ accountCapabilitiesKey: Symbol('test-capabilities'), useAccountWorkspace: () => ({ api, scope: workspace.isAdmin ? 'admin' : 'user', isAdmin: workspace.isAdmin }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn(), showWarning: vi.fn() }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isSimpleMode: false, isAdmin: true }) }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
import { accountCapabilitiesKey } from '@/composables/useAccountWorkspace'
import CreateAccountModal from '../CreateAccountModal.vue'
import ReAuthAccountModal from '@/components/admin/account/ReAuthAccountModal.vue'

const BaseDialog = defineComponent({ props: ['show'], template: '<div v-if="show"><slot/><slot name="footer"/></div>' })

describe('shared account creation in user workspace', () => {
  it('shows only configured subscriptions and hides admin controls even for an admin logged into the user workspace', async () => {
    const wrapper = mount(CreateAccountModal, { props: { show: true, proxies: [], groups: [] }, global: { provide: { [accountCapabilitiesKey as symbol]: ref({ platforms: [{ platform: 'openai', account_types: ['oauth'] }, { platform: 'anthropic', account_types: ['oauth'] }] }) }, stubs: { BaseDialog, Select: true, Icon: true, PlatformIcon: true, ConfirmDialog: true, OAuthAuthorizationFlow: true } } })
    await flushPromises()
    const text = wrapper.text()
    for (const platform of ['OpenAI', 'Anthropic']) expect(text).toContain(platform)
    for (const platform of ['Gemini', 'Antigravity', 'Grok', 'Kimi', 'DeepSeek']) expect(text).not.toContain(platform)
    expect(wrapper.find('input[value="setup-token"]').exists()).toBe(false)
    expect(text).not.toContain('admin.accounts.apiKey')
    for (const label of ['admin.accounts.proxy', 'admin.accounts.priority', 'admin.accounts.concurrency', 'admin.accounts.billingRateMultiplier', 'admin.accounts.quotaControl', 'admin.accounts.modelRestriction']) expect(text).not.toContain(label)
    expect(api.settings.getSettings).not.toHaveBeenCalled()
    expect(api.settings.getWebSearchEmulationConfig).not.toHaveBeenCalled()
    expect(api.tlsFingerprintProfiles.list).not.toHaveBeenCalled()
    await wrapper.findAll('button').find(b => b.text().trim() === 'OpenAI')!.trigger('click')
    expect(wrapper.findAll('button').some(b => b.text().includes('API Key'))).toBe(false)
    wrapper.unmount()
  })
  it('does not allow setup-token switching during Claude reauthorization', async () => {
    const wrapper = mount(ReAuthAccountModal, { props: { show: true, account: { id: 7, name: 'Claude', platform: 'anthropic', type: 'oauth', credentials: {} } as any }, global: { provide: { [accountCapabilitiesKey as symbol]: ref({ platforms: [{ platform: 'anthropic', account_types: ['oauth'] }] }) }, stubs: { BaseDialog, Icon: true, OAuthAuthorizationFlow: true } } })
    await flushPromises()
    expect(wrapper.find('input[value="setup-token"]').exists()).toBe(false)
    expect(wrapper.getComponent({ name: 'OAuthAuthorizationFlow' }).props('addMethod')).toBe('oauth')
    wrapper.unmount()
  })
  it('supports an explicitly enabled API key strategy using the original account form', async () => {
    const wrapper = mount(CreateAccountModal, { props: { show: true, proxies: [], groups: [] }, global: { provide: { [accountCapabilitiesKey as symbol]: ref({ platforms: [{ platform: 'openai', account_types: ['apikey'] }] }) }, stubs: { BaseDialog, Select: true, Icon: true, PlatformIcon: true, ConfirmDialog: true, OAuthAuthorizationFlow: true } } })
    await flushPromises()
    expect(wrapper.text()).not.toContain('admin.accounts.types.chatgptOauth')
    await wrapper.get('form input[type="text"]').setValue('Owned account')
    await wrapper.get('form input[type="password"]').setValue('owned-key')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(create).toHaveBeenCalledWith(expect.objectContaining({ name: 'Owned account', platform: 'openai', type: 'apikey' }))
    wrapper.unmount()
  })
})
