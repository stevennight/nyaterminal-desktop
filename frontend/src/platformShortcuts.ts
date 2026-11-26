export type ModifierKeyEvent = Pick<KeyboardEvent | MouseEvent, 'ctrlKey' | 'metaKey' | 'altKey'>

export function isMacPlatform(userAgent?: string): boolean {
  const ua = userAgent ?? (typeof navigator !== 'undefined' ? navigator.userAgent : '')
  return /Mac|iPhone|iPad/.test(ua)
}

export function isPrimaryModifier(event: ModifierKeyEvent, isMac = isMacPlatform()): boolean {
  if (event.altKey) return false
  return isMac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey
}

export function primaryModifierKeyLabel(isMac = isMacPlatform()): string {
  return isMac ? '⌘' : 'Ctrl'
}

export function primaryModifierPrefix(isMac = isMacPlatform()): string {
  return isMac ? '⌘' : 'Ctrl+'
}

export function altModifierPrefix(isMac = isMacPlatform()): string {
  return isMac ? '⌥' : 'Alt+'
}
