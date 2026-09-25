import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

const { api, create } = vi.hoisted(() => {
  const create = vi.fn().mockResolvedValue({ id: 7, platform: 'openai', type: 'apikey' })
  return { create, api: { accounts: { create }, settings: { getWebSearchEmulationConfig: vi.fn(), getSettings: vi.fn() }, tlsFingerprintProfiles: { list: vi.fn() } } }
})
vi.mock('@/composables/useAccountWorkspace', () => ({ useAccountWorkspace: () => ({ api, scope: 'user', isAdmin: false }) }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn(), showWarning: vi.fn() }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isSimpleMode: false }) }))
vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
import CreateAccountModal from '../CreateAccountModal.vue'

const BaseDialog = defineComponent({ props: ['show'], template: '<div v-if="show"><slot/><slot name="footer"/></div>' })

describe('shared account creation in user workspace', () => {
  it('keeps platform authorization choices while hiding admin routing and scheduling controls', async () => {
    const wrapper = mount(CreateAccountModal, { props: { show: true, proxies: [], groups: [] }, global: { stubs: { BaseDialog, Select: true, Icon: true, PlatformIcon: true, ConfirmDialog: true, OAuthAuthorizationFlow: true } } })
    await flushPromises()
    const text = wrapper.text()
    for (const platform of ['OpenAI', 'Gemini', 'Antigravity', 'Grok', 'Kimi', 'DeepSeek']) expect(text).toContain(platform)
    for (const label of ['admin.accounts.proxy', 'admin.accounts.priority', 'admin.accounts.concurrency', 'admin.accounts.billingRateMultiplier', 'admin.accounts.quotaControl', 'admin.accounts.modelRestriction']) expect(text).not.toContain(label)
    expect(api.settings.getSettings).not.toHaveBeenCalled()
    expect(api.settings.getWebSearchEmulationConfig).not.toHaveBeenCalled()
    expect(api.tlsFingerprintProfiles.list).not.toHaveBeenCalled()
    await wrapper.findAll('button').find(b => b.text().trim() === 'OpenAI')!.trigger('click')
    await wrapper.findAll('button').find(b => b.text().includes('API Key'))!.trigger('click')
    expect(wrapper.text()).not.toContain('admin.accounts.baseUrl')
    await wrapper.get('form input[type="text"]').setValue('Owned account')
    await wrapper.get('form input[type="password"]').setValue('owned-key')
    await wrapper.get('form').trigger('submit.prevent')
    await flushPromises()
    expect(create).toHaveBeenCalledWith(expect.objectContaining({ name: 'Owned account', platform: 'openai', type: 'apikey' }))
    wrapper.unmount()
  })
})
