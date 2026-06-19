export type VaultStatus = {
  initialized: boolean
  locked: boolean
  quickUnlock: boolean
  quickUnlockMethod: string
  customLockPassword: boolean
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
  legacyAlgorithms: boolean
  syncSecrets?: boolean
  commandHistory: boolean
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
  lockAfterMinutes: number
  disconnectOnLock: boolean
  syncCommandHistory: boolean
  syncSecretsByDefault: boolean
  sensitiveCommandRules: string[]
}

export type Bootstrap = {
  vault: VaultStatus
  groups?: Group[]
  tags?: Tag[]
  connections?: Connection[]
  settings?: Settings
  syncConfigured: boolean
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
