import { describe, expect, it } from 'vitest'
import {
  resolveTerminalThemeColors,
  TERMINAL_THEME_PRESETS,
  terminalChromeVariables,
  terminalXtermTheme,
} from './terminalThemes'

describe('terminalThemes', () => {
  it('includes the supported light presets with complete color palettes', () => {
    const expectedLightPresets = {
      white: '#FFFFFF',
      'solarized-light': '#FDF6E3',
      'one-light': '#FAFAFA',
      'gruvbox-light': '#FBF1C7',
      'catppuccin-latte': '#EFF1F5',
      'rose-pine-dawn': '#FAF4ED',
    }

    for (const [id, background] of Object.entries(expectedLightPresets)) {
      const preset = TERMINAL_THEME_PRESETS.find(item => item.id === id)
      expect(preset, `missing ${id} preset`).toBeDefined()
      expect(preset?.colors.background).toBe(background)
      expect(Object.values(preset?.colors ?? {})).toHaveLength(22)
      expect(Object.values(preset?.colors ?? {}).every(color => /^#[0-9A-F]{6}$/.test(color))).toBe(true)
    }
  })

  it('falls back to preset colors when custom values are invalid', () => {
    const dracula = TERMINAL_THEME_PRESETS.find(preset => preset.id === 'dracula')
    expect(dracula).toBeDefined()

    const colors = resolveTerminalThemeColors({
      terminalThemePreset: 'dracula',
      terminalThemeColors: {
        ...TERMINAL_THEME_PRESETS[0].colors,
        background: '#123456',
        foreground: 'bad-value',
      },
    })

    expect(colors.background).toBe('#123456')
    expect(colors.foreground).toBe(dracula?.colors.foreground)
  })

  it('provides a readable white terminal preset', () => {
    const white = TERMINAL_THEME_PRESETS.find(preset => preset.id === 'white')
    expect(white).toBeDefined()

    expect(white?.colors.background).toBe('#FFFFFF')
    expect(white?.colors.foreground).toBe('#24292F')
    expect(white?.colors.white).not.toBe(white?.colors.background)
    expect(white?.colors.brightWhite).not.toBe(white?.colors.background)
  })

  it('maps resolved colors to xterm and chrome variables', () => {
    const colors = TERMINAL_THEME_PRESETS.find(preset => preset.id === 'nord')?.colors
    expect(colors).toBeDefined()
    if (!colors) return

    const theme = terminalXtermTheme(colors)
    const chrome = terminalChromeVariables(colors)

    expect(theme.background).toBe(colors.background)
    expect(theme.selectionBackground).toContain('rgba(')
    expect(chrome['--terminal-bg']).toBe(colors.background)
    expect(chrome['--terminal-panel']).toMatch(/^#[0-9A-F]{6}$/)
  })
})
