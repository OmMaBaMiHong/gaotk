/**
 * Daily check-in API endpoints
 * 用户侧每日签到（发美元额度）：状态查询 + 执行签到
 */

import { apiClient } from './client'

export interface CheckinDay {
  date: string
  amount: number
  streak_days: number
}

export interface CheckinStatus {
  enabled: boolean
  today_checked_in: boolean
  today_amount: number
  streak_days: number
  min_amount: number
  max_amount: number
  streak_bonus_amount: number
  streak_bonus_days: number
  month_records: CheckinDay[]
  total_checkin_count: number
  total_awarded_amount: number
}

export interface CheckinResult {
  amount_awarded: number
  base_amount: number
  streak_bonus_applied: boolean
  streak_bonus_amount: number
  streak_days: number
  new_balance: number
}

/**
 * Get the current user's check-in status
 */
export async function getStatus(): Promise<CheckinStatus> {
  const { data } = await apiClient.get<CheckinStatus>('/user/checkin')
  return data
}

/**
 * Perform today's check-in
 */
export async function checkin(): Promise<CheckinResult> {
  const { data } = await apiClient.post<CheckinResult>('/user/checkin')
  return data
}

export const checkinAPI = {
  getStatus,
  checkin
}

export default checkinAPI
