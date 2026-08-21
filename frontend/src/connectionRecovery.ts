export const VAULT_LOCKED_MESSAGE = '保险库已锁定，请先解锁。'
export const VAULT_LOCKED_RECONNECT_MESSAGE = '保险库已锁定，解锁后将自动重连'
export const MANUAL_RECONNECT_MESSAGE = '连接已断开，请重试'
export const RESUMING_RECONNECT_MESSAGE = '解锁成功，正在恢复连接…'

export function isVaultLockedError(value: unknown) {
  const message = value instanceof Error ? value.message : String(value)
  return message.toLowerCase().includes('vault is locked')
}
