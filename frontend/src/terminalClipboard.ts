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
  viewportHeight: number
) {
  const width = 180
  const height = 124
  const left = Math.min(clientX, Math.max(8, viewportWidth - width - 8))
  const top = Math.min(clientY, Math.max(8, viewportHeight - height - 8))
  return { left, top }
}
