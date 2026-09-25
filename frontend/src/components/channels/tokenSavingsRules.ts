export interface ReceivingRule {
  group_id: number
  priority: number
  allowed_plans: string[]
  account_types?: string[]
}

export interface ReceivingGroup {
  id: number
  name: string
  platform: string
}

export const OPENAI_SAVINGS_PLANS = [
  { value: 'pro', label: 'Pro20x' },
  { value: 'prolite', label: 'Pro5x' },
  { value: 'plus', label: 'Plus' },
  { value: 'team', label: 'Business Standard' },
  { value: 'selfservebusinessprolite', label: 'Business Premium' }
]

export const CLAUDE_SAVINGS_PLANS = [{ value: 'pro', label: 'Claude Pro' }, { value: 'max', label: 'Claude Max' }]
export const plansForPlatform = (platform: string) => platform === 'openai' ? OPENAI_SAVINGS_PLANS : platform === 'anthropic' ? CLAUDE_SAVINGS_PLANS : []
export const defaultAccountTypes = (platform: string) => ['openai', 'anthropic'].includes(platform) ? ['oauth'] : []
export const availableAccountTypes = (platform: string) => ['openai', 'anthropic'].includes(platform) ? ['oauth', 'apikey'] : ['apikey']

export function buildReceivingConfig(groupIds: number[], rules: ReceivingRule[], groups: ReceivingGroup[]) {
  const selected = [...new Set(groupIds)].filter(id => groups.some(group => group.id === id))
  return {
    receiving_group_ids: selected,
    receiving_rules: selected.map(group_id => {
      const rule = rules.find(item => item.group_id === group_id)
      const platform = groups.find(group => group.id === group_id)!.platform
      const account_types = [...(rule?.account_types ?? defaultAccountTypes(platform))]
      return {
        group_id,
        priority: rule?.priority ?? 0,
        account_types,
        allowed_plans: account_types.includes('oauth') && plansForPlatform(platform).length ? [...(rule?.allowed_plans || [])] : []
      }
    })
  }
}

export function validReceivingRules(rules: ReceivingRule[], groups: ReceivingGroup[]): boolean {
  return rules.length > 0 && rules.every(rule => {
    if (!Number.isInteger(rule.priority) || rule.priority < 0 || rule.priority > 1000) return false
    const group = groups.find(item => item.id === rule.group_id)
    if (!group) return false
    const types = rule.account_types ?? defaultAccountTypes(group.platform)
    if (types.some(type => !availableAccountTypes(group.platform).includes(type))) return false
    if (!types.includes('oauth')) return rule.allowed_plans.length === 0
    return rule.allowed_plans.length > 0 && rule.allowed_plans.every(plan => plansForPlatform(group.platform).some(option => option.value === plan))
  })
}
