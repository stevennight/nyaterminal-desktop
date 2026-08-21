import { describe, expect, it } from 'vitest'
import { isVaultLockedError, VAULT_LOCKED_MESSAGE } from './connectionRecovery'

describe('connection recovery messages', () => {
  it('recognizes the backend vault lock error', () => {
    expect(isVaultLockedError('vault is locked')).toBe(true)
    expect(isVaultLockedError(new Error('vault is locked'))).toBe(true)
    expect(isVaultLockedError('connection closed')).toBe(false)
  })

  it('keeps the vault lock message user-facing', () => {
    expect(VAULT_LOCKED_MESSAGE).toBe('保险库已锁定，请先解锁。')
  })
})
