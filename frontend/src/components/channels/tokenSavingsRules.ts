export interface ReceivingRule {
  group_id: number
  priority: number
  allowed_plans: string[]
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
  { value: 'free', label: 'Free' },
  { value: 'team', label: 'Business Standard' },
  { value: 'selfservebusinessprolite', label: 'Business Premium' }
]

export function buildReceivingConfig(groupIds: number[], rules: ReceivingRule[], groups: ReceivingGroup[]) {
  const selected = [...new Set(groupIds)].filter(id => groups.some(group => group.id === id))
  return {
    receiving_group_ids: selected,
    receiving_rules: selected.map(group_id => {
      const rule = rules.find(item => item.group_id === group_id)
      const isOpenAI = groups.find(group => group.id === group_id)?.platform === 'openai'
      return {
        group_id,
        priority: rule?.priority ?? 0,
        allowed_plans: isOpenAI ? [...(rule?.allowed_plans || [])] : []
      }
    })
  }
}

export function validReceivingRules(rules: ReceivingRule[], groups: ReceivingGroup[]): boolean {
  return rules.length > 0 && rules.every(rule => {
    if (!Number.isInteger(rule.priority) || rule.priority < 0 || rule.priority > 1000) return false
    const group = groups.find(item => item.id === rule.group_id)
    if (!group) return false
    if (group.platform !== 'openai') return rule.allowed_plans.length === 0
    return rule.allowed_plans.length > 0 && rule.allowed_plans.every(plan => OPENAI_SAVINGS_PLANS.some(option => option.value === plan))
  })
}
