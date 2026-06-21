import { describe, expect, it } from 'vitest'
import {
  resolveTerminalThemeColors,
  TERMINAL_THEME_PRESETS,
  terminalChromeVariables,
  terminalXtermTheme,
} from './terminalThemes'

describe('terminalThemes', () => {
  it('falls back to preset colors when custom values are invalid', () => {
    const colors = resolveTerminalThemeColors({
      terminalThemePreset: 'dracula',
      terminalThemeColors: {
        ...TERMINAL_THEME_PRESETS[0].colors,
        background: '#123456',
        foreground: 'bad-value',
      },
    })

    expect(colors.background).toBe('#123456')
    expect(colors.foreground).toBe(TERMINAL_THEME_PRESETS[1].colors.foreground)
  })

  it('maps resolved colors to xterm and chrome variables', () => {
    const colors = TERMINAL_THEME_PRESETS[2].colors
    const theme = terminalXtermTheme(colors)
    const chrome = terminalChromeVariables(colors)

    expect(theme.background).toBe(colors.background)
    expect(theme.selectionBackground).toContain('rgba(')
    expect(chrome['--terminal-bg']).toBe(colors.background)
    expect(chrome['--terminal-panel']).toMatch(/^#[0-9A-F]{6}$/)
  })
})
