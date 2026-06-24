export type VaultStatus = {
  initialized: boolean
  locked: boolean
  quickUnlock: boolean
  quickUnlockMethod: string
}

export type Group = {
  id: string
  parentId?: string
  name: string
  sortOrder: number
  createdAt?: string
  updatedAt?: string
}

export type Tag = {
  id: string
  name: string
  color: string
  createdAt?: string
  updatedAt?: string
}

export type Connection = {
  id: string
  groupId?: string
  name: string
  remark?: string
  host: string
  port: number
  username: string
  credentialId?: string
  authentication: 'password' | 'private_key' | 'agent' | 'interactive'
  tags: string[]
  sortOrder: number
  encoding: string
  keepAliveSeconds: number
  connectTimeoutSeconds: number
  autoReconnect?: boolean
  legacyAlgorithms: boolean
  syncSecrets?: boolean
  commandHistory: boolean
  createdAt?: string
  updatedAt?: string
}

export type Credential = {
  id: string
  name: string
  type: Connection['authentication']
  username?: string
  password?: string
  privateKeyPem?: string
  passphrase?: string
}

export type Settings = {
  theme: string
  fontFamily: string
  fontSize: number
  terminalThemePreset: string
  terminalThemeColors: TerminalThemeColors
  lockAfterMinutes: number
  disconnectOnLock: boolean
  autoReconnect: boolean
  syncCommandHistory: boolean
  syncSecretsByDefault: boolean
  sensitiveCommandRules: string[]
}

export type TerminalThemeColors = {
  background: string
  foreground: string
  cursor: string
  cursorAccent: string
  selectionBackground: string
  selectionForeground: string
  black: string
  red: string
  green: string
  yellow: string
  blue: string
  magenta: string
  cyan: string
  white: string
  brightBlack: string
  brightRed: string
  brightGreen: string
  brightYellow: string
  brightBlue: string
  brightMagenta: string
  brightCyan: string
  brightWhite: string
}

export type SyncSummary = {
  configured: boolean
  serverUrl?: string
  username?: string
  deviceName?: string
  deviceId?: string
  loggedIn?: boolean
  serverInitialized: boolean
  syncInitialized: boolean
  autoSyncEnabled: boolean
  lastSyncedAt?: string
  lastAttemptAt?: string
  lastSuccessAt?: string
  lastPushed?: number
  lastPulled?: number
  lastConflicts?: number
  lastError?: string
  running: boolean
}

export type AccountSummary = {
  loggedIn: boolean
  serverUrl?: string
  username?: string
  deviceName?: string
  deviceId?: string
  totpEnabled?: boolean
  configured?: boolean
  serverInitialized: boolean
  syncInitialized: boolean
  accessExpiresAt?: string
  refreshExpiresAt?: string
}

export type Bootstrap = {
  vault: VaultStatus
  groups?: Group[]
  tags?: Tag[]
  connections?: Connection[]
  settings?: Settings
  account?: AccountSummary
  syncConfigured: boolean
  syncSummary?: SyncSummary
}

export type PendingHostKey = {
  id: string
  hostPort: string
  algorithm: string
  fingerprint: string
  changed: boolean
}

export type TerminalStart = {
  session?: { sessionId: string; url: string }
  hostKey?: PendingHostKey
  authPrompt?: {
    kind: 'password' | 'private_key'
    reason: 'missing' | 'invalid'
    message: string
  }
}

export type RemoteEntry = {
  name: string
  path: string
  size: number
  mode: string
  isDir: boolean
  modTime: string
}

export type SFTPTransfer = {
  id: string
  connectionId: string
  name: string
  direction: 'upload' | 'download'
  status: 'queued' | 'running' | 'paused' | 'completed' | 'failed' | 'cancelled'
  bytesDone: number
  totalBytes: number
  error?: string
  createdAt: string
  updatedAt: string
}

export type CommandHistory = {
  id: string
  connectionId: string
  command: string
  useCount: number
  lastUsedAt: string
}

export type InteractiveChallenge = {
  id: string
  user: string
  instruction: string
  questions: string[]
  echoes: boolean[]
}
