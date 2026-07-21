export type WindowThemeName = 'dark' | 'light'

export type WindowThemeRuntime = {
  WindowSetDarkTheme?: () => void
  WindowSetLightTheme?: () => void
  WindowSetBackgroundColour?: (red: number, green: number, blue: number, alpha: number) => void
}

const WINDOW_BACKGROUND: Record<WindowThemeName, readonly [number, number, number, number]> = {
  dark: [10, 14, 22, 255],
  light: [245, 247, 250, 255],
}

export function syncWindowTheme(
  theme: WindowThemeName,
  runtime: WindowThemeRuntime | undefined = currentWindowRuntime(),
) {
  if (!runtime) return

  if (theme === 'dark') runtime.WindowSetDarkTheme?.()
  else runtime.WindowSetLightTheme?.()

  runtime.WindowSetBackgroundColour?.(...WINDOW_BACKGROUND[theme])
}

function currentWindowRuntime(): WindowThemeRuntime | undefined {
  if (typeof window === 'undefined') return undefined
  return window.runtime
}
