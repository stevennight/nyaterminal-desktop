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

  it('maps Cmd+C and Cmd+V on macOS without requiring shift', () => {
    expect(getTerminalClipboardAction(key('c', false, false, true))).toBe('copy')
    expect(getTerminalClipboardAction(key('v', false, false, true))).toBe('paste')
  })

  it('maps Cmd+Shift+C and Cmd+Shift+V on macOS', () => {
    expect(getTerminalClipboardAction(key('C', false, true, true))).toBe('copy')
    expect(getTerminalClipboardAction(key('V', false, true, true))).toBe('paste')
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

function key(key: string, ctrlKey: boolean, shiftKey: boolean, metaKey = false) {
  return {
    key,
    ctrlKey,
    shiftKey,
    metaKey,
    altKey: false
  } as KeyboardEvent
}
