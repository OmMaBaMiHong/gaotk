import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import TokenSavingsRulesEditor from '../TokenSavingsRulesEditor.vue'
import { buildReceivingConfig, validReceivingRules, type ReceivingRule } from '../tokenSavingsRules'
import zh from '@/i18n/locales/zh/tokenBank'

vi.mock('vue-i18n', () => ({ useI18n: () => ({
  t: (key: string) => zh[key.replace('tokenBank.', '') as keyof typeof zh] || key
}) }))

const groups = [
  { id: 3, name: 'GPT Pro×20', platform: 'openai' },
  { id: 4, name: 'OpenAI 通用', platform: 'openai' },
  { id: 5, name: 'Claude', platform: 'anthropic' }
]
function render(groupIds: number[] = [], rules: ReceivingRule[] = []) {
  return mount({
    components: { TokenSavingsRulesEditor },
    setup: () => ({ groups, groupIds: ref(groupIds), rules: ref(rules) }),
    template: '<TokenSavingsRulesEditor :groups="groups" v-model:group-ids="groupIds" v-model:rules="rules" />'
  })
}

describe('Token Savings receiving rules editor', () => {
  it('never infers a plan from a group name and requires explicit OpenAI eligibility', async () => {
    const view = render()
    await view.get('[data-testid="receiving-group-3"]').setValue(true)
    const editor = view.getComponent(TokenSavingsRulesEditor)
    expect(editor.props('rules')).toEqual([{ group_id: 3, priority: 0, account_types: ['oauth'], allowed_plans: [] }])
    expect(validReceivingRules(buildReceivingConfig(editor.props('groupIds'), editor.props('rules'), groups).receiving_rules, groups)).toBe(false)
    const selector = view.get<HTMLSelectElement>('[data-testid="receiving-plans-3"]')
    expect(selector.element.multiple).toBe(true)
    expect(selector.findAll('option').map(option => [option.attributes('value'), option.text()])).toEqual([
      ['pro', 'Pro20x'], ['prolite', 'Pro5x'], ['plus', 'Plus'], ['team', 'Business Standard'], ['selfservebusinessprolite', 'Business Premium']
    ])
    await selector.setValue(['pro'])
    expect(editor.props('rules')).toEqual([{ group_id: 3, priority: 0, account_types: ['oauth'], allowed_plans: ['pro'] }])
    view.unmount()
  })
  it('roundtrips every accepted plan and changes only the edited group', async () => {
    const view = render([3, 4], [
      { group_id: 3, priority: 800, account_types: ['oauth'], allowed_plans: ['pro', 'team'] },
      { group_id: 4, priority: 100, account_types: ['oauth'], allowed_plans: ['plus', 'prolite'] }
    ])
    const selector = view.get<HTMLSelectElement>('[data-testid="receiving-plans-3"]')
    expect(Array.from(selector.element.selectedOptions).map(option => option.value)).toEqual(['pro', 'team'])
    expect(view.get<HTMLInputElement>('[data-testid="receiving-priority-3"]').element.value).toBe('800')
    await view.get('[data-testid="receiving-priority-3"]').setValue('900')
    expect(view.getComponent(TokenSavingsRulesEditor).props('rules')).toEqual([
      { group_id: 3, priority: 900, account_types: ['oauth'], allowed_plans: ['pro', 'team'] },
      { group_id: 4, priority: 100, account_types: ['oauth'], allowed_plans: ['plus', 'prolite'] }
    ])
    view.unmount()
  })
  it('removes unchecked rules and requires Claude plan selection', async () => {
    const view = render([3], [{ group_id: 3, priority: 200, account_types: ['oauth'], allowed_plans: ['pro'] }])
    await view.get('[data-testid="receiving-group-5"]').setValue(true)
    expect(view.find('[data-testid="receiving-plans-5"]').exists()).toBe(true)
    await view.get('[data-testid="receiving-group-3"]').setValue(false)
    expect(view.getComponent(TokenSavingsRulesEditor).props('groupIds')).toEqual([5])
    expect(view.getComponent(TokenSavingsRulesEditor).props('rules')).toEqual([{ group_id: 5, priority: 0, account_types: ['oauth'], allowed_plans: [] }])
    view.unmount()
  })
})

describe('Token Savings save payload', () => {
  it('filters rules for removed/unlinked groups and clones allowed lists without losing multi-selection', () => {
    const rules = [
      { group_id: 3, priority: 800, account_types: ['oauth'], allowed_plans: ['pro', 'team'] },
      { group_id: 4, priority: 100, account_types: ['oauth'], allowed_plans: ['plus'] },
      { group_id: 5, priority: 40, account_types: ['oauth'], allowed_plans: ['pro'] }
    ]
    const payload = buildReceivingConfig([3, 4, 5], rules, [groups[0], groups[2]])
    expect(payload).toEqual({ receiving_group_ids: [3, 5], receiving_rules: [rules[0], { group_id: 5, priority: 40, account_types: ['oauth'], allowed_plans: ['pro'] }] })
    payload.receiving_rules[0].allowed_plans.push('plus')
    expect(rules[0].allowed_plans).toEqual(['pro', 'team'])
    expect(buildReceivingConfig([5], rules, groups).receiving_rules).toEqual([{ group_id: 5, priority: 40, account_types: ['oauth'], allowed_plans: ['pro'] }])
  })
  it('leaves legacy OpenAI groups unconfigured until the admin selects accepted plans', () => {
    const payload = buildReceivingConfig([3, 5], [], groups)
    expect(payload.receiving_rules).toEqual([
      { group_id: 3, priority: 0, account_types: ['oauth'], allowed_plans: [] }, { group_id: 5, priority: 0, account_types: ['oauth'], allowed_plans: [] }
    ])
    expect(validReceivingRules(payload.receiving_rules, groups)).toBe(false)
  })
  it.each([-1, 1001, 1.5, NaN])('rejects invalid priority %s', (priority) => {
    expect(validReceivingRules([{ group_id: 3, priority, allowed_plans: ['pro'] }], groups)).toBe(false)
  })
  it('accepts boundary priorities and exact canonical plans only', () => {
    expect(validReceivingRules([{ group_id: 3, priority: 1000, allowed_plans: ['pro'] }, { group_id: 5, priority: 0, account_types: ['oauth'], allowed_plans: ['max'] }], groups)).toBe(true)
    expect(validReceivingRules([{ group_id: 3, priority: 1, allowed_plans: ['pro20x'] }], groups)).toBe(false)
    expect(validReceivingRules([{ group_id: 3, priority: 1, account_types: ['oauth'], allowed_plans: ['free'] }], groups)).toBe(false)
    expect(validReceivingRules([], groups)).toBe(false)
  })
})


describe('receiving account type strategy', () => {
  it('persists an explicitly disabled rule and clears its old plan restrictions', async () => {
    const view = render([3], [{ group_id: 3, priority: 1, allowed_plans: ['pro'], account_types: ['oauth'] }])
    await view.get('[data-testid="receiving-type-3-oauth"]').setValue(false)
    expect(view.find('[data-testid="receiving-plans-3"]').exists()).toBe(false)
    const rules = view.getComponent(TokenSavingsRulesEditor).props('rules')
    expect(rules).toEqual([{ group_id: 3, priority: 1, account_types: [], allowed_plans: [] }])
    expect(validReceivingRules(rules, groups)).toBe(true)
    expect(buildReceivingConfig([3], [{ ...rules[0], allowed_plans: ['pro'] }], groups).receiving_rules).toEqual(rules)
    view.unmount()
  })
  it('makes API keys opt-in and drops plan selection when only API keys are accepted', async () => {
    const view = render([3], [{ group_id: 3, priority: 1, allowed_plans: ['pro'], account_types: ['oauth'] }])
    expect(view.get<HTMLInputElement>('[data-testid="receiving-type-3-apikey"]').element.checked).toBe(false)
    await view.get('[data-testid="receiving-type-3-apikey"]').setValue(true)
    await view.get('[data-testid="receiving-type-3-oauth"]').setValue(false)
    expect(view.find('[data-testid="receiving-plans-3"]').exists()).toBe(false)
    const rules = view.getComponent(TokenSavingsRulesEditor).props('rules')
    expect(rules[0]).toEqual({ group_id: 3, priority: 1, allowed_plans: [], account_types: ['apikey'] })
    expect(validReceivingRules(rules, groups)).toBe(true)
    view.unmount()
  })
  it('fails closed for other platforms until API keys are explicitly enabled', () => {
    const other = [{ id: 6, name: 'Gemini Pro', platform: 'gemini' }]
    const defaults = buildReceivingConfig([6], [], other)
    expect(defaults.receiving_rules[0].account_types).toEqual([])
    expect(validReceivingRules(defaults.receiving_rules, other)).toBe(true)
    const config = buildReceivingConfig([6], [{ group_id: 6, priority: 1, account_types: ['apikey'], allowed_plans: [] }], other)
    expect(validReceivingRules(config.receiving_rules, other)).toBe(true)
    expect(validReceivingRules([{ ...config.receiving_rules[0], account_types: ['setup-token'] }], other)).toBe(false)
  })
})
