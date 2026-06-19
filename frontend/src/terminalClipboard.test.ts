import { describe, expect, it } from 'vitest'
import { clampTerminalMenuPosition, getTerminalClipboardAction } from './terminalClipboard'

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
    expect(clampTerminalMenuPosition(999, 999, 320, 240)).toEqual({ left: 132, top: 108 })
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
