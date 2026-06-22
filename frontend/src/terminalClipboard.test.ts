import { describe, expect, it } from 'vitest'
import { chunkTerminalInput, clampTerminalMenuPosition, getTerminalClipboardAction } from './terminalClipboard'

describe('terminalClipboard', () => {
  it('maps Ctrl+Shift+C to copy', () => {
    expect(getTerminalClipboardAction(key('c', true, true))).toBe('copy')
  })

  it('maps Ctrl+Shift+V to paste', () => {
    expect(getTerminalClipboardAction(key('V', true, true))).toBe('paste')
  })

  it('ignores Ctrl+C without shift', () => {
    expect(getTerminalClipboardAction(key('c', true, false))).toBeUndefined()
  })

  it('clamps the terminal menu inside the viewport', () => {
    expect(clampTerminalMenuPosition(999, 999, 320, 240)).toEqual({ left: 132, top: 897 })
  })

  it('places the menu above the cursor when near the bottom edge', () => {
    expect(clampTerminalMenuPosition(40, 230, 320, 240)).toEqual({ left: 40, top: 128 })
  })

  it('chunks large pasted input below the websocket message limit', () => {
    const input = `${'a'.repeat(9)}你${'b'.repeat(8)}`
    const chunks = chunkTerminalInput(input, 10)
    expect(chunks.join('')).toBe(input)
    expect(chunks.map(chunk => new TextEncoder().encode(chunk).length)).toEqual([9, 10, 1])
  })
})

function key(key: string, ctrlKey: boolean, shiftKey: boolean) {
  return {
    key,
    ctrlKey,
    shiftKey,
    metaKey: false
  } as KeyboardEvent
}
