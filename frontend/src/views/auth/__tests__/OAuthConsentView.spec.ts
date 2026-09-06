/**
 * 授权确认页。
 *
 * 这页守的是几件"错了不会报错、只会让人卡住"的事：
 *   1. 未登录时去登录要**带着本页的 query 回来**——丢了 client_id/state，
 *      回来就是一张不知道在授权什么的页面；
 *   2. store 说已登录不算数，要实打实问一次 /auth/me——本地留着的过期 token
 *      会让人点了「授权」才在下一步失败；
 *   3. 「拒绝」必须回到发起方并带 error=access_denied——静默停在本页的话，
 *      第三方那边一直等着，用户以为自己卡住了；
 *   4. 参数缺失时不给任何按钮——点了也必然失败。
 */
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import OAuthConsentView from '@/views/auth/OAuthConsentView.vue'

const { routeState, locationState, routerPushMock, isAuthenticatedRef, getCurrentUserMock, oauthAuthorizeMock } =
  vi.hoisted(() => ({
    routeState: {
      path: '/oauth/authorize',
      fullPath: '/oauth/authorize?client_id=skoob&redirect_uri=http%3A%2F%2Fapp.test%2Fcb&state=st1',
      query: {} as Record<string, unknown>,
    },
    locationState: { current: { href: 'http://site.test/oauth/authorize', hostname: 'site.test' } },
    routerPushMock: vi.fn(),
    isAuthenticatedRef: { value: true },
    getCurrentUserMock: vi.fn(),
    oauthAuthorizeMock: vi.fn(),
  }))

vi.mock('vue-router', () => ({
  useRoute: () => routeState,
  useRouter: () => ({ push: (...args: any[]) => routerPushMock(...args) }),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ get isAuthenticated() { return isAuthenticatedRef.value } }),
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ siteName: 'GaoTK' }),
}))

vi.mock('@/api/auth', () => ({
  getCurrentUser: (...args: any[]) => getCurrentUserMock(...args),
  oauthAuthorize: (...args: any[]) => oauthAuthorizeMock(...args),
}))

vi.mock('@/components/icons/Icon.vue', () => ({
  default: { name: 'Icon', template: '<span />' },
}))

function mountView() {
  return mount(OAuthConsentView)
}

describe('OAuthConsentView', () => {
  beforeEach(() => {
    routeState.query = {
      client_id: 'skoob',
      redirect_uri: 'http://app.test/cb',
      state: 'st1',
    }
    routeState.fullPath = '/oauth/authorize?client_id=skoob&redirect_uri=http%3A%2F%2Fapp.test%2Fcb&state=st1'
    isAuthenticatedRef.value = true
    locationState.current = { href: 'http://site.test/oauth/authorize', hostname: 'site.test' }
    Object.defineProperty(window, 'location', { configurable: true, value: locationState.current })
    routerPushMock.mockReset()
    getCurrentUserMock.mockReset()
    oauthAuthorizeMock.mockReset()
  })

  it('已登录：显示账号与「授权并登录」', async () => {
    getCurrentUserMock.mockResolvedValue({ data: { email: 'a@example.com' } })
    const wrapper = mountView()
    await flushPromises()

    expect(getCurrentUserMock).toHaveBeenCalled()
    expect(wrapper.text()).toContain('a@example.com')
    expect(wrapper.text()).toContain('auth.consent.allow')
  })

  it('本地 token 过期（/auth/me 失败）→ 当作未登录，而不是显示假的已登录态', async () => {
    getCurrentUserMock.mockRejectedValue(new Error('401'))
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('auth.consent.goLogin')
    expect(wrapper.text()).not.toContain('auth.consent.allow')
  })

  it('未登录去登录：redirect 带着本页 query，回来才知道在授权什么', async () => {
    isAuthenticatedRef.value = false
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('button').trigger('click')
    expect(routerPushMock).toHaveBeenCalledWith({
      path: '/login',
      query: { redirect: routeState.fullPath },
    })
    expect(getCurrentUserMock).not.toHaveBeenCalled()
  })

  it('同意：把后端拼好的 redirectUrl 整页跳过去', async () => {
    getCurrentUserMock.mockResolvedValue({ data: { email: 'a@example.com' } })
    oauthAuthorizeMock.mockResolvedValue({ data: { redirectUrl: 'http://app.test/cb?code=c1&state=st1' } })
    const wrapper = mountView()
    await flushPromises()

    const buttons = wrapper.findAll('button')
    await buttons[buttons.length - 1]!.trigger('click')
    await flushPromises()

    expect(oauthAuthorizeMock).toHaveBeenCalledWith({
      client_id: 'skoob',
      redirect_uri: 'http://app.test/cb',
      state: 'st1',
    })
    expect(locationState.current.href).toBe('http://app.test/cb?code=c1&state=st1')
  })

  it('拒绝：回到发起方并带 error=access_denied，不静默停在本页', async () => {
    getCurrentUserMock.mockResolvedValue({ data: { email: 'a@example.com' } })
    const wrapper = mountView()
    await flushPromises()

    const buttons = wrapper.findAll('button')
    await buttons[buttons.length - 2]!.trigger('click')

    expect(locationState.current.href).toBe('http://app.test/cb?error=access_denied&state=st1')
  })

  it('授权失败：留在本页显示错误，不跳到一个空地址', async () => {
    getCurrentUserMock.mockResolvedValue({ data: { email: 'a@example.com' } })
    oauthAuthorizeMock.mockResolvedValue({ data: {} })
    const wrapper = mountView()
    await flushPromises()

    const buttons = wrapper.findAll('button')
    await buttons[buttons.length - 1]!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('auth.consent.failed')
    expect(locationState.current.href).toBe('http://site.test/oauth/authorize')
  })

  it('缺 redirect_uri：只说缺什么，一个按钮都不给', async () => {
    routeState.query = { client_id: 'skoob' }
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.text()).toContain('auth.consent.missingParam')
    expect(wrapper.findAll('button')).toHaveLength(0)
    expect(getCurrentUserMock).not.toHaveBeenCalled()
  })
})
