import { describe, expect, it } from 'vitest'
import {
  directSuggestionShortcutIndex,
  isSuggestionDeleteKey,
  isSuggestionDismissKey,
  nextSuggestionIndex,
} from './terminalSuggestions'

describe('terminalSuggestions', () => {
  it('supports Alt with the number row', () => {
    expect(directSuggestionShortcutIndex(key('2', 'Digit2'), 5)).toBe(1)
  })

  it('supports Alt with the numpad', () => {
    expect(directSuggestionShortcutIndex(key('2', 'Numpad2'), 5)).toBe(1)
  })

  it('supports Alt+1 for the first suggestion', () => {
    expect(directSuggestionShortcutIndex(key('1', 'Digit1'), 5)).toBe(0)
  })

  it('ignores shortcuts outside the visible suggestion range', () => {
    expect(directSuggestionShortcutIndex(key('5', 'Digit5'), 3)).toBe(-1)
  })

  it('wraps keyboard navigation through the list', () => {
    expect(nextSuggestionIndex(0, 3, -1)).toBe(2)
    expect(nextSuggestionIndex(2, 3, 1)).toBe(0)
  })

  it('uses Escape to dismiss suggestions', () => {
    expect(isSuggestionDismissKey({ ...key('Escape', 'Escape'), altKey: false })).toBe(true)
  })

  it('uses plain Delete to delete the selected suggestion', () => {
    expect(isSuggestionDeleteKey({ ...key('Delete', 'Delete'), altKey: false })).toBe(true)
    expect(isSuggestionDeleteKey({ ...key('Delete', 'Delete'), altKey: false, ctrlKey: true })).toBe(false)
  })
})

function key(value: string, code: string) {
  return {
    key: value,
    code,
    altKey: true,
    ctrlKey: false,
    metaKey: false,
    shiftKey: false,
  } as KeyboardEvent
}
