import { inject, provide, type Ref, type InjectionKey } from 'vue'
import { adminAPI } from '@/api/admin'
import { createAccountsAPI } from '@/api/admin/accounts'
import { createGeminiAPI } from '@/api/admin/gemini'
import { createAntigravityAPI } from '@/api/admin/antigravity'
import { createGrokAPI } from '@/api/admin/grok'
import { createCnProvidersAPI } from '@/api/admin/cnProviders'
import { createKimiOAuthAPI } from '@/api/admin/kimiOAuth'
import type { AccountScope } from '@/api/accountScopeClient'

import type { TokenBankCapabilities } from '@/api/tokenBank'

export const accountCapabilitiesKey: InjectionKey<Ref<TokenBankCapabilities>> = Symbol('account-capabilities')

const accountScopeKey: InjectionKey<AccountScope> = Symbol('account-workspace')

export function provideAccountWorkspace(scope: AccountScope, capabilities?: Ref<TokenBankCapabilities>) {
  provide(accountScopeKey, scope)
  if (capabilities) provide(accountCapabilitiesKey, capabilities)
}

export function useAccountWorkspace() {
  const scope = inject(accountScopeKey, 'admin')
  const api = scope === 'admin' ? adminAPI : {
    ...adminAPI,
    accounts: createAccountsAPI(scope),
    gemini: createGeminiAPI(scope),
    antigravity: createAntigravityAPI(scope),
    grok: createGrokAPI(scope),
    cnProviders: createCnProvidersAPI(scope)
  }
  return { scope, isAdmin: scope === 'admin', api, kimi: createKimiOAuthAPI(scope) }
}
