import { describe, it, expect, vi } from 'vitest'
import { parseRentalCallback } from '../tokenBank'

vi.mock('../client', () => ({ apiClient: {} }))

describe('Rental OAuth callback parsing', () => {
  it('uses the returned state from a full callback URL', () => {
    expect(
      parseRentalCallback(
        'http://localhost:1455/auth/callback?code=a%2Bb&state=returned',
        'generated'
      )
    ).toEqual({ code: 'a+b', state: 'returned' })
  })
  it('does not hide a missing state in a full callback URL', () => {
    expect(
      parseRentalCallback(
        'http://localhost:1455/auth/callback?code=abc',
        'generated'
      )
    ).toEqual({ code: 'abc', state: '' })
  })
  it('supports Claude code#state and a code with explicit state', () => {
    expect(parseRentalCallback('code#claude-state', 'generated')).toEqual({
      code: 'code',
      state: 'claude-state'
    })
    expect(parseRentalCallback(' code ', ' explicit ')).toEqual({
      code: 'code',
      state: 'explicit'
    })
  })
})
