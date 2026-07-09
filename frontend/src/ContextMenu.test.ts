import { describe, expect, it } from 'vitest'
import { shouldCloseContextMenuOnScroll } from './ContextMenu'

describe('ContextMenu', () => {
  it('keeps menus open when xterm output scrolls the viewport', () => {
    expect(shouldCloseContextMenuOnScroll(fakeTarget(true))).toBe(false)
  })

  it('still closes menus for ordinary scroll targets', () => {
    expect(shouldCloseContextMenuOnScroll(fakeTarget(false))).toBe(true)
  })

  it('closes menus when the scroll target is unavailable', () => {
    expect(shouldCloseContextMenuOnScroll(null)).toBe(true)
  })
})

function fakeTarget(matchesTerminalViewport: boolean) {
  return {
    closest: (selector: string) => matchesTerminalViewport && selector === '.xterm-viewport' ? {} : null,
  } as unknown as EventTarget
}
