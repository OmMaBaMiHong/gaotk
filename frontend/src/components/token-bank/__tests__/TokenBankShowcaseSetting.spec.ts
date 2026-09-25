import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { ref } from 'vue'
import TokenBankShowcaseSetting from '../TokenBankShowcaseSetting.vue'
import zh from '@/i18n/locales/zh/tokenBank'

vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({
  locale: ref('zh'),
  t: (key: string, values: Record<string, unknown> = {}) => (zh[key.replace('tokenBank.', '') as keyof typeof zh] || key).replace(/\{(\w+)\}/g, (_, name) => String(values[name] ?? ''))
}) }))

const api = vi.hoisted(() => ({ showcaseSettings: vi.fn(), updateShowcaseSettings: vi.fn() }))
vi.mock('@/api/tokenBank', () => ({ tokenBankAPI: api }))
let wrapper: VueWrapper | undefined
afterEach(() => { wrapper?.unmount(); vi.resetAllMocks() })
const render = () => (wrapper = mount(TokenBankShowcaseSetting, {}))

describe('showcase global setting', () => {
  it('defaults off and persists independently with the server response', async () => {
    api.showcaseSettings.mockResolvedValue({ enabled: false })
    api.updateShowcaseSettings.mockResolvedValue({ enabled: true })
    const view = render(); await flushPromises()
    expect(view.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    await view.get('[role="switch"]').trigger('click'); await flushPromises()
    expect(api.updateShowcaseSettings).toHaveBeenCalledWith(true)
    expect(view.get('[role="switch"]').attributes('aria-checked')).toBe('true')
  })
  it('keeps the confirmed value after failed save', async () => {
    api.showcaseSettings.mockResolvedValue({ enabled: false })
    api.updateShowcaseSettings.mockRejectedValue(new Error('failed'))
    const view = render(); await flushPromises()
    await view.get('[role="switch"]').trigger('click'); await flushPromises()
    expect(view.get('[role="switch"]').attributes('aria-checked')).toBe('false')
    expect(view.find('[role="alert"]').exists()).toBe(true)
  })
  it('disables mutation when initial read fails and allows retry', async () => {
    api.showcaseSettings.mockRejectedValueOnce(new Error('failed')).mockResolvedValue({ enabled: true })
    const view = render(); await flushPromises()
    expect(view.get('[role="switch"]').attributes('disabled')).toBeDefined()
    await view.get('[data-testid="setting-retry"]').trigger('click'); await flushPromises()
    expect(view.get('[role="switch"]').attributes('aria-checked')).toBe('true')
  })
})
