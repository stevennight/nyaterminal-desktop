export type TerminalClipboardAction = 'copy' | 'paste' | undefined

const encoder = new TextEncoder()

export function getTerminalClipboardAction(event: KeyboardEvent): TerminalClipboardAction {
  if (!(event.ctrlKey || event.metaKey)) return undefined
  const key = event.key.toLowerCase()
  if (key === 'c' && event.shiftKey) return 'copy'
  if (key === 'v' && event.shiftKey) return 'paste'
  return undefined
}

export function chunkTerminalInput(text: string, maxBytes = 16 * 1024) {
  const chunks: string[] = []
  let chunk = ''
  let chunkBytes = 0
  for (const char of text) {
    const charBytes = encoder.encode(char).length
    if (chunk && chunkBytes + charBytes > maxBytes) {
      chunks.push(chunk)
      chunk = ''
      chunkBytes = 0
    }
    chunk += char
    chunkBytes += charBytes
  }
  if (chunk) chunks.push(chunk)
  return chunks
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
