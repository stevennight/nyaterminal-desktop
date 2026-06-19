import type {
  Bootstrap, CommandHistory, Connection, Credential, Group,
  RemoteEntry, Settings, Tag, TerminalStart
} from './types'

type Backend = {
  Bootstrap(): Promise<Bootstrap>
  InitializeVault(password: string): Promise<void>
  Unlock(password: string): Promise<void>
  UnlockWithSystem(): Promise<void>
  EnableSystemUnlock(): Promise<void>
  DisableSystemUnlock(): Promise<void>
  SetLockPassword(password: string): Promise<void>
  ClearLockPassword(): Promise<void>
  Lock(): Promise<void>
  ListGroups(): Promise<Group[]>
  SaveGroup(value: Group): Promise<Group>
  ListTags(): Promise<Tag[]>
  SaveTag(value: Tag): Promise<Tag>
  ListConnections(): Promise<Connection[]>
  SaveConnection(value: Connection): Promise<Connection>
  SaveCredential(value: Credential): Promise<Credential>
  DeleteRecord(id: string): Promise<void>
  GetSettings(): Promise<Settings>
  SaveSettings(value: Settings): Promise<void>
  AddCommandHistory(connectionId: string, command: string, privateSession: boolean): Promise<void>
  SuggestCommands(connectionId: string, prefix: string): Promise<CommandHistory[]>
  StartSSH(request: {
    connectionId: string
    columns: number
    rows: number
    interactionResponses: string[]
  }): Promise<TerminalStart>
  AcceptHostKey(id: string): Promise<void>
  ResizeSSH(sessionId: string, columns: number, rows: number): Promise<void>
  CloseSSH(sessionId: string): Promise<void>
  AnswerSSHChallenge(id: string, answers: string[], cancelled: boolean): Promise<void>
  ListRemote(connectionId: string, remotePath: string): Promise<RemoteEntry[]>
  ChooseLocalDirectory(): Promise<{ token: string; path: string; items: RemoteEntry[] }>
  ListLocal(token: string, relativePath: string): Promise<RemoteEntry[]>
  UploadGranted(connectionId: string, token: string, localRelativePath: string, remotePath: string): Promise<void>
  DownloadGranted(connectionId: string, remotePath: string, token: string, localRelativePath: string): Promise<void>
  UploadFile(connectionId: string, remotePath: string): Promise<void>
  DownloadFile(connectionId: string, remotePath: string, suggestedName: string): Promise<void>
  InitializeSync(serverUrl: string, username: string, password: string, deviceName: string):
    Promise<{ deviceId: string; recoveryCode: string }>
  LoginSync(serverUrl: string, username: string, password: string, deviceId: string): Promise<void>
  SyncNow(syncSecrets: boolean, syncHistory: boolean):
    Promise<{ pushed: number; pulled: number; conflicts: number; cursor: number }>
  BeginDevicePairing(serverUrl: string, deviceName: string): Promise<{
    pairingId: string; deviceId: string; shortCode: string
    qrPayload: string; expiresAt: string
  }>
  ApproveDevicePairing(qrPayload: string): Promise<void>
  ClaimDevicePairing(username: string, password: string, totpCode: string):
    Promise<{ approved: boolean; deviceId?: string }>
  ListSyncDevices(): Promise<Array<{
    id: string; name: string; approved: boolean; revoked: boolean
    createdAt: string; lastSeenAt: string
  }>>
  RevokeSyncDevice(deviceId: string): Promise<void>
  BeginSyncTOTPSetup(): Promise<{ secret: string; setupToken: string; uri: string }>
  ConfirmSyncTOTPSetup(setupToken: string, code: string): Promise<string[]>
}

declare global {
  interface Window {
    go?: { app?: { App?: Backend } }
    runtime?: {
      EventsOn?: (name: string, callback: (...args: unknown[]) => void) => () => void
    }
  }
}

function backend(): Backend {
  const value = window.go?.app?.App
  if (!value) throw new Error('Wails backend is unavailable')
  return value
}

export const api: Backend = new Proxy({} as Backend, {
  get: (_, property: keyof Backend) =>
    (...args: unknown[]) => (backend()[property] as (...values: unknown[]) => unknown)(...args)
})
