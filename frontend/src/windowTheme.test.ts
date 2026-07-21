import { describe, expect, it, vi } from 'vitest'
import { syncWindowTheme, type WindowThemeRuntime } from './windowTheme'

describe('syncWindowTheme', () => {
  it.each([
    {
      theme: 'dark' as const,
      background: [10, 14, 22, 255],
      selectedMethod: 'WindowSetDarkTheme' as const,
      unselectedMethod: 'WindowSetLightTheme' as const,
    },
    {
      theme: 'light' as const,
      background: [245, 247, 250, 255],
      selectedMethod: 'WindowSetLightTheme' as const,
      unselectedMethod: 'WindowSetDarkTheme' as const,
    },
  ])('applies the $theme native window theme and background', ({
    theme, background, selectedMethod, unselectedMethod,
  }) => {
    const runtime: Required<WindowThemeRuntime> = {
      WindowSetDarkTheme: vi.fn(),
      WindowSetLightTheme: vi.fn(),
      WindowSetBackgroundColour: vi.fn(),
    }

    syncWindowTheme(theme, runtime)

    expect(runtime[selectedMethod]).toHaveBeenCalledOnce()
    expect(runtime[unselectedMethod]).not.toHaveBeenCalled()
    expect(runtime.WindowSetBackgroundColour).toHaveBeenCalledWith(...background)
  })

  it('does nothing when the Wails runtime is unavailable', () => {
    expect(() => syncWindowTheme('dark', undefined)).not.toThrow()
  })
})
