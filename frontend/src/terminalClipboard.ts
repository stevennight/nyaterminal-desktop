export type TerminalClipboardAction = 'copy' | 'paste' | undefined

export function getTerminalClipboardAction(event: KeyboardEvent): TerminalClipboardAction {
  if (!(event.ctrlKey || event.metaKey)) return undefined
  const key = event.key.toLowerCase()
  if (key === 'c' && event.shiftKey) return 'copy'
  if (key === 'v' && event.shiftKey) return 'paste'
  return undefined
}

export function clampTerminalMenuPosition(
  clientX: number,
  clientY: number,
  viewportWidth: number,
  viewportHeight: number,
  menuWidth = 180,
  menuHeight = 102
) {
  const margin = 8
  const left = Math.min(clientX, Math.max(margin, viewportWidth - menuWidth - margin))
  const top = clientY + menuHeight + margin > viewportHeight
    ? Math.max(margin, clientY - menuHeight)
    : Math.max(margin, clientY)
  return { left, top }
}
