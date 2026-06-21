import type { Settings, TerminalThemeColors } from './types'

export type TerminalThemePreset = {
  id: string
  label: string
  description: string
  colors: TerminalThemeColors
}

export const TERMINAL_THEME_PRESETS: TerminalThemePreset[] = [
  {
    id: 'default',
    label: 'Nya Default',
    description: '当前默认风格，适合长时间工作。',
    colors: {
      background: '#0A0E16',
      foreground: '#DCE3EE',
      cursor: '#77E4D4',
      cursorAccent: '#0A0E16',
      selectionBackground: '#325A6A',
      selectionForeground: '#DCE3EE',
      black: '#111827',
      red: '#F7768E',
      green: '#70D6A1',
      yellow: '#F2C94C',
      blue: '#6CB6FF',
      magenta: '#C792EA',
      cyan: '#72D7E6',
      white: '#DCE3EE',
      brightBlack: '#68788E',
      brightRed: '#FF8A9A',
      brightGreen: '#8DE5B8',
      brightYellow: '#FFD479',
      brightBlue: '#8CC5FF',
      brightMagenta: '#D9B8FF',
      brightCyan: '#9AEAF2',
      brightWhite: '#F5F7FA',
    },
  },
  {
    id: 'dracula',
    label: 'Dracula',
    description: '高对比紫粉系，社区里很常见。',
    colors: {
      background: '#282A36',
      foreground: '#F8F8F2',
      cursor: '#F8F8F2',
      cursorAccent: '#282A36',
      selectionBackground: '#44475A',
      selectionForeground: '#F8F8F2',
      black: '#21222C',
      red: '#FF5555',
      green: '#50FA7B',
      yellow: '#F1FA8C',
      blue: '#BD93F9',
      magenta: '#FF79C6',
      cyan: '#8BE9FD',
      white: '#F8F8F2',
      brightBlack: '#6272A4',
      brightRed: '#FF6E6E',
      brightGreen: '#69FF94',
      brightYellow: '#FFFFA5',
      brightBlue: '#D6ACFF',
      brightMagenta: '#FF92DF',
      brightCyan: '#A4FFFF',
      brightWhite: '#FFFFFF',
    },
  },
  {
    id: 'nord',
    label: 'Nord',
    description: '冷静的蓝灰调，适合低刺激界面。',
    colors: {
      background: '#2E3440',
      foreground: '#D8DEE9',
      cursor: '#88C0D0',
      cursorAccent: '#2E3440',
      selectionBackground: '#4C566A',
      selectionForeground: '#ECEFF4',
      black: '#3B4252',
      red: '#BF616A',
      green: '#A3BE8C',
      yellow: '#EBCB8B',
      blue: '#81A1C1',
      magenta: '#B48EAD',
      cyan: '#88C0D0',
      white: '#E5E9F0',
      brightBlack: '#4C566A',
      brightRed: '#D06F79',
      brightGreen: '#B1D196',
      brightYellow: '#F0D399',
      brightBlue: '#8CAAC7',
      brightMagenta: '#C0A5BA',
      brightCyan: '#93CCDC',
      brightWhite: '#ECEFF4',
    },
  },
  {
    id: 'solarized-dark',
    label: 'Solarized Dark',
    description: '经典低对比方案，阅读代码很舒服。',
    colors: {
      background: '#002B36',
      foreground: '#839496',
      cursor: '#93A1A1',
      cursorAccent: '#002B36',
      selectionBackground: '#073642',
      selectionForeground: '#EEE8D5',
      black: '#073642',
      red: '#DC322F',
      green: '#859900',
      yellow: '#B58900',
      blue: '#268BD2',
      magenta: '#D33682',
      cyan: '#2AA198',
      white: '#EEE8D5',
      brightBlack: '#002B36',
      brightRed: '#CB4B16',
      brightGreen: '#586E75',
      brightYellow: '#657B83',
      brightBlue: '#839496',
      brightMagenta: '#6C71C4',
      brightCyan: '#93A1A1',
      brightWhite: '#FDF6E3',
    },
  },
  {
    id: 'one-dark',
    label: 'One Dark',
    description: 'Atom / VS Code 系常见配色，层次清晰。',
    colors: {
      background: '#282C34',
      foreground: '#ABB2BF',
      cursor: '#528BFF',
      cursorAccent: '#282C34',
      selectionBackground: '#3E4451',
      selectionForeground: '#E6EAF2',
      black: '#282C34',
      red: '#E06C75',
      green: '#98C379',
      yellow: '#E5C07B',
      blue: '#61AFEF',
      magenta: '#C678DD',
      cyan: '#56B6C2',
      white: '#ABB2BF',
      brightBlack: '#5C6370',
      brightRed: '#E06C75',
      brightGreen: '#98C379',
      brightYellow: '#D19A66',
      brightBlue: '#61AFEF',
      brightMagenta: '#C678DD',
      brightCyan: '#56B6C2',
      brightWhite: '#FFFFFF',
    },
  },
  {
    id: 'tokyo-night',
    label: 'Tokyo Night',
    description: '偏霓虹但不过分艳，终端观感很稳。',
    colors: {
      background: '#1A1B26',
      foreground: '#C0CAF5',
      cursor: '#C0CAF5',
      cursorAccent: '#1A1B26',
      selectionBackground: '#33467C',
      selectionForeground: '#C0CAF5',
      black: '#15161E',
      red: '#F7768E',
      green: '#9ECE6A',
      yellow: '#E0AF68',
      blue: '#7AA2F7',
      magenta: '#BB9AF7',
      cyan: '#7DCFFF',
      white: '#A9B1D6',
      brightBlack: '#414868',
      brightRed: '#F7768E',
      brightGreen: '#9ECE6A',
      brightYellow: '#E0AF68',
      brightBlue: '#7AA2F7',
      brightMagenta: '#BB9AF7',
      brightCyan: '#7DCFFF',
      brightWhite: '#C0CAF5',
    },
  },
]

export const TERMINAL_THEME_GROUPS: Array<{
  title: string
  fields: Array<{ key: keyof TerminalThemeColors; label: string }>
}> = [
  {
    title: '界面元素',
    fields: [
      { key: 'background', label: '背景' },
      { key: 'foreground', label: '前景' },
      { key: 'cursor', label: '光标' },
      { key: 'cursorAccent', label: '光标文字' },
      { key: 'selectionBackground', label: '选区背景' },
      { key: 'selectionForeground', label: '选区文字' },
    ],
  },
  {
    title: 'ANSI 基础色',
    fields: [
      { key: 'black', label: '黑' },
      { key: 'red', label: '红' },
      { key: 'green', label: '绿' },
      { key: 'yellow', label: '黄' },
      { key: 'blue', label: '蓝' },
      { key: 'magenta', label: '洋红' },
      { key: 'cyan', label: '青' },
      { key: 'white', label: '白' },
    ],
  },
  {
    title: 'ANSI 高亮色',
    fields: [
      { key: 'brightBlack', label: '亮黑' },
      { key: 'brightRed', label: '亮红' },
      { key: 'brightGreen', label: '亮绿' },
      { key: 'brightYellow', label: '亮黄' },
      { key: 'brightBlue', label: '亮蓝' },
      { key: 'brightMagenta', label: '亮洋红' },
      { key: 'brightCyan', label: '亮青' },
      { key: 'brightWhite', label: '亮白' },
    ],
  },
]

const presetMap = Object.fromEntries(TERMINAL_THEME_PRESETS.map(preset => [preset.id, preset]))

export function cloneTerminalThemeColors(colors: TerminalThemeColors): TerminalThemeColors {
  return { ...colors }
}

export function terminalThemePreset(id?: string) {
  return id ? presetMap[id] : undefined
}

export function resolveTerminalThemeColors(settings: Pick<Settings, 'terminalThemePreset' | 'terminalThemeColors'>) {
  const fallback = terminalThemePreset(settings.terminalThemePreset)?.colors ?? TERMINAL_THEME_PRESETS[0].colors
  return normalizeTerminalThemeColors(settings.terminalThemeColors, fallback)
}

export function normalizeTerminalThemeColors(
  colors: Partial<TerminalThemeColors> | undefined,
  fallback: TerminalThemeColors = TERMINAL_THEME_PRESETS[0].colors,
): TerminalThemeColors {
  return {
    background: normalizeHex(colors?.background, fallback.background),
    foreground: normalizeHex(colors?.foreground, fallback.foreground),
    cursor: normalizeHex(colors?.cursor, fallback.cursor),
    cursorAccent: normalizeHex(colors?.cursorAccent, fallback.cursorAccent),
    selectionBackground: normalizeHex(colors?.selectionBackground, fallback.selectionBackground),
    selectionForeground: normalizeHex(colors?.selectionForeground, fallback.selectionForeground),
    black: normalizeHex(colors?.black, fallback.black),
    red: normalizeHex(colors?.red, fallback.red),
    green: normalizeHex(colors?.green, fallback.green),
    yellow: normalizeHex(colors?.yellow, fallback.yellow),
    blue: normalizeHex(colors?.blue, fallback.blue),
    magenta: normalizeHex(colors?.magenta, fallback.magenta),
    cyan: normalizeHex(colors?.cyan, fallback.cyan),
    white: normalizeHex(colors?.white, fallback.white),
    brightBlack: normalizeHex(colors?.brightBlack, fallback.brightBlack),
    brightRed: normalizeHex(colors?.brightRed, fallback.brightRed),
    brightGreen: normalizeHex(colors?.brightGreen, fallback.brightGreen),
    brightYellow: normalizeHex(colors?.brightYellow, fallback.brightYellow),
    brightBlue: normalizeHex(colors?.brightBlue, fallback.brightBlue),
    brightMagenta: normalizeHex(colors?.brightMagenta, fallback.brightMagenta),
    brightCyan: normalizeHex(colors?.brightCyan, fallback.brightCyan),
    brightWhite: normalizeHex(colors?.brightWhite, fallback.brightWhite),
  }
}

export function terminalChromeVariables(colors: TerminalThemeColors) {
  return {
    '--terminal-bg': colors.background,
    '--terminal-fg': colors.foreground,
    '--terminal-panel': mixHex(colors.background, colors.foreground, 0.08),
    '--terminal-panel-strong': mixHex(colors.background, colors.foreground, 0.14),
    '--terminal-line': mixHex(colors.background, colors.foreground, 0.18),
    '--terminal-muted': mixHex(colors.foreground, colors.background, 0.38),
    '--terminal-hover': rgba(colors.selectionBackground, 0.35),
    '--terminal-shadow': rgba(colors.black, 0.45),
    '--terminal-danger': colors.brightRed,
    '--terminal-danger-line': rgba(colors.red, 0.45),
    '--terminal-selection': rgba(colors.selectionBackground, 0.28),
  }
}

export function terminalXtermTheme(colors: TerminalThemeColors) {
  return {
    background: colors.background,
    foreground: colors.foreground,
    cursor: colors.cursor,
    cursorAccent: colors.cursorAccent,
    selectionBackground: rgba(colors.selectionBackground, 0.5),
    selectionForeground: colors.selectionForeground,
    black: colors.black,
    red: colors.red,
    green: colors.green,
    yellow: colors.yellow,
    blue: colors.blue,
    magenta: colors.magenta,
    cyan: colors.cyan,
    white: colors.white,
    brightBlack: colors.brightBlack,
    brightRed: colors.brightRed,
    brightGreen: colors.brightGreen,
    brightYellow: colors.brightYellow,
    brightBlue: colors.brightBlue,
    brightMagenta: colors.brightMagenta,
    brightCyan: colors.brightCyan,
    brightWhite: colors.brightWhite,
  }
}

function normalizeHex(value: string | undefined, fallback: string) {
  return /^#[0-9a-fA-F]{6}$/.test(value ?? '') ? (value ?? '').toUpperCase() : fallback.toUpperCase()
}

function rgba(value: string, alpha: number) {
  const rgb = parseHex(value)
  if (!rgb) return value
  return `rgba(${rgb[0]}, ${rgb[1]}, ${rgb[2]}, ${clamp(alpha, 0, 1)})`
}

function mixHex(from: string, to: string, ratio: number) {
  const source = parseHex(from)
  const target = parseHex(to)
  if (!source || !target) return from
  const next = source.map((channel, index) =>
    Math.round(channel + (target[index] - channel) * clamp(ratio, 0, 1))
  ) as [number, number, number]
  return `#${next.map(value => value.toString(16).padStart(2, '0')).join('')}`.toUpperCase()
}

function parseHex(value: string) {
  const match = /^#([0-9a-fA-F]{6})$/.exec(value)
  if (!match) return undefined
  const hex = match[1]
  return [
    Number.parseInt(hex.slice(0, 2), 16),
    Number.parseInt(hex.slice(2, 4), 16),
    Number.parseInt(hex.slice(4, 6), 16),
  ] as const
}

function clamp(value: number, min: number, max: number) {
  return Math.min(max, Math.max(min, value))
}
