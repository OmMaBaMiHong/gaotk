import { computed, inject, ref } from 'vue'
import { accountCapabilitiesKey, useAccountWorkspace } from './useAccountWorkspace'
import type { TokenBankCapabilities } from '@/api/tokenBank'

// Workspace scope (not login role) keeps admins in the user page subject to its rules.
export function useAccountCapabilities() {
  const { isAdmin } = useAccountWorkspace()
  const capabilities = inject(accountCapabilitiesKey, ref<TokenBankCapabilities>({ platforms: [] }))
  const availablePlatforms = computed(() => capabilities.value.platforms.filter(p => p.account_types.length))
  const platformAllowed = (platform: string) => isAdmin || availablePlatforms.value.some(p => p.platform === platform)
  const accountAllowed = (platform: string, type: string) => isAdmin || availablePlatforms.value.some(p => p.platform === platform && p.account_types.includes(type))
  return { availablePlatforms, platformAllowed, accountAllowed }
}
