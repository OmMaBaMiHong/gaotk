import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { ref } from 'vue'
import TokenBankShowcase from '../TokenBankShowcase.vue'
import zh from '@/i18n/locales/zh/tokenBank'

vi.mock('vue-i18n', async (importOriginal) => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({
  locale: ref('zh'),
  t: (key: string, values: Record<string, unknown> = {}) => (zh[key.replace('tokenBank.', '') as keyof typeof zh] || key).replace(/\{(\w+)\}/g, (_, name) => String(values[name] ?? ''))
}) }))

const { showcase } = vi.hoisted(() => ({ showcase: vi.fn() }))
vi.mock('@/api/tokenBank', () => ({ tokenBankAPI: { showcase } }))
const empty = { enabled: true, leaderboard: [], recent: [], updated_at: '2026-09-25T00:00:00Z' }
const populated = {
  ...empty,
  leaderboard: [{ rank: 1, name: '张***', amount: 1.23456789 }],
  recent: [
    { name: '张***', platform: 'openai', amount: 0.12345678, created_at: '2026-09-25T00:00:00Z' },
    { name: '李***', platform: 'gemini', amount: 0.5, created_at: '2026-09-24T23:00:00Z' }
  ]
}
let wrapper: VueWrapper | undefined
let hidden = false
let reduced = false
const render = () => {
  wrapper = mount(TokenBankShowcase, {
    global: { stubs: { teleport: true } }
  })
  return wrapper
}
beforeEach(() => {
  vi.useFakeTimers()
  hidden = false
  reduced = false
  vi.spyOn(document, 'hidden', 'get').mockImplementation(() => hidden)
  vi.spyOn(window, 'matchMedia').mockImplementation(query => ({ matches: reduced, media: query, addEventListener: vi.fn(), removeEventListener: vi.fn() }) as unknown as MediaQueryList)
  showcase.mockReset().mockResolvedValue({ ...empty, enabled: false })
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; vi.useRealTimers(); vi.restoreAllMocks() })

describe('Token Bank showcase', () => {
  it('hides when off and discovers later enablement', async () => {
    const view = render()
    await flushPromises()
    expect(view.find('[data-testid="showcase"]').exists()).toBe(false)
    showcase.mockResolvedValue(empty)
    await vi.advanceTimersByTimeAsync(60_000)
    expect(view.text()).toContain('暂无已入账收益')
  })
  it('has honest empty broadcast and leaderboard', async () => {
    showcase.mockResolvedValue(empty)
    const view = render(); await flushPromises()
    expect(view.text()).toContain('暂无已入账收益')
    await view.get('[data-testid="leaderboard-open"]').trigger('click')
    expect(view.find('[role="dialog"]').exists()).toBe(true)
    expect(view.findAll('tbody tr')).toHaveLength(0)
    expect(view.text()).not.toContain('$')
  })
  it('shows masked supplied earnings, original currency precision and a bounded leaderboard', async () => {
    showcase.mockResolvedValue({ ...populated, email: 'private@example.com', leaderboard: Array.from({ length: 12 }, (_, i) => ({ rank: i + 1, name: '张***', amount: 1.23456789 })) })
    const view = render(); await flushPromises()
    expect(view.text()).toContain('张***')
    expect(view.text()).toContain('OpenAI')
    expect(view.text()).toContain('0.12345678')
    expect(view.html()).not.toContain('private@example.com')
    expect(view.text()).not.toContain('提现')
    await view.get('[data-testid="leaderboard-open"]').trigger('click')
    expect(view.findAll('tbody tr')).toHaveLength(10)
    expect(view.text()).toContain('1.23456789')
  })
  it('cycles with pause and respects reduced motion', async () => {
    showcase.mockResolvedValue(populated)
    const view = render(); await flushPromises()
    expect(view.find('[data-testid="broadcast-pause"]').exists()).toBe(true)
    await vi.advanceTimersByTimeAsync(8_500)
    expect(view.text()).toContain('李***')
    await view.get('[data-testid="broadcast-pause"]').trigger('click')
    await vi.advanceTimersByTimeAsync(8_500)
    expect(view.text()).toContain('李***')
    view.unmount(); wrapper = undefined
    reduced = true
    const still = render(); await flushPromises()
    await vi.advanceTimersByTimeAsync(8_500)
    expect(still.text()).toContain('张***')
    expect(still.text()).not.toContain('李***')
  })
  it('refreshes only while visible and cancels timers, requests and listeners on unmount', async () => {
    const remove = vi.spyOn(document, 'removeEventListener')
    const view = render(); await flushPromises()
    hidden = true; document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(120_000)
    expect(showcase).toHaveBeenCalledTimes(1)
    hidden = false; document.dispatchEvent(new Event('visibilitychange')); await flushPromises()
    expect(showcase).toHaveBeenCalledTimes(2)
    const signal = showcase.mock.calls.at(-1)![0] as AbortSignal
    view.unmount(); wrapper = undefined
    expect(signal.aborted).toBe(true)
    expect(remove).toHaveBeenCalledWith('visibilitychange', expect.any(Function))
    expect(vi.getTimerCount()).toBe(0)
    await vi.advanceTimersByTimeAsync(120_000)
    expect(showcase).toHaveBeenCalledTimes(2)
  })
  it('hides stale earnings after refresh failure or switch off', async () => {
    showcase.mockResolvedValue(populated)
    const view = render(); await flushPromises()
    showcase.mockRejectedValue(new Error('unavailable'))
    await vi.advanceTimersByTimeAsync(60_000)
    expect(view.find('[data-testid="showcase"]').exists()).toBe(false)
    showcase.mockResolvedValue(populated)
    await vi.advanceTimersByTimeAsync(60_000)
    await view.get('[data-testid="leaderboard-open"]').trigger('click')
    showcase.mockResolvedValue({ ...empty, enabled: false })
    await vi.advanceTimersByTimeAsync(60_000)
    expect(view.find('[role="dialog"]').exists()).toBe(false)
  })
})
