import { describe, expect, it } from 'vitest'
import {
  altModifierPrefix,
  isMacPlatform,
  isPrimaryModifier,
  primaryModifierKeyLabel,
  primaryModifierPrefix,
} from './platformShortcuts'

describe('platformShortcuts', () => {
  it('detects macOS user agents', () => {
    expect(isMacPlatform('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)')).toBe(true)
    expect(isMacPlatform('Mozilla/5.0 (Windows NT 10.0; Win64; x64)')).toBe(false)
    expect(isMacPlatform('Mozilla/5.0 (X11; Linux x86_64)')).toBe(false)
  })

  it('matches only Command on macOS so Control stays available for the terminal', () => {
    expect(isPrimaryModifier({ metaKey: true, ctrlKey: false, altKey: false }, true)).toBe(true)
    expect(isPrimaryModifier({ metaKey: false, ctrlKey: true, altKey: false }, true)).toBe(false)
    expect(isPrimaryModifier({ metaKey: true, ctrlKey: true, altKey: false }, true)).toBe(false)
    expect(isPrimaryModifier({ metaKey: true, ctrlKey: false, altKey: true }, true)).toBe(false)
  })

  it('matches only Control on Windows and Linux', () => {
    expect(isPrimaryModifier({ metaKey: false, ctrlKey: true, altKey: false }, false)).toBe(true)
    expect(isPrimaryModifier({ metaKey: true, ctrlKey: false, altKey: false }, false)).toBe(false)
    expect(isPrimaryModifier({ metaKey: true, ctrlKey: true, altKey: false }, false)).toBe(false)
    expect(isPrimaryModifier({ metaKey: false, ctrlKey: true, altKey: true }, false)).toBe(false)
  })

  it('formats modifier labels per platform', () => {
    expect(primaryModifierKeyLabel(true)).toBe('⌘')
    expect(primaryModifierPrefix(true)).toBe('⌘')
    expect(altModifierPrefix(true)).toBe('⌥')

    expect(primaryModifierKeyLabel(false)).toBe('Ctrl')
    expect(primaryModifierPrefix(false)).toBe('Ctrl+')
    expect(altModifierPrefix(false)).toBe('Alt+')
  })
})
