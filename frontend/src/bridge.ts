import type {
  Bootstrap, CommandHistory, Connection, Credential, Group,
  RemoteEntry, Settings, SFTPTransfer, Tag, TerminalStart
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
  ChangeMasterPassword(oldPassword: string, newPassword: string): Promise<void>
  Lock(): Promise<void>
  ListGroups(): Promise<Group[]>
  SaveGroup(value: Group): Promise<Group>
  DeleteGroup(id: string): Promise<void>
  ListTags(): Promise<Tag[]>
  SaveTag(value: Tag): Promise<Tag>
  DeleteTag(id: string): Promise<void>
  ListConnections(): Promise<Connection[]>
  SaveConnection(value: Connection): Promise<Connection>
  DeleteConnection(id: string): Promise<void>
  SaveCredential(value: Credential): Promise<Credential>
  DeleteRecord(id: string): Promise<void>
  GetSettings(): Promise<Settings>
  SaveSettings(value: Settings): Promise<void>
  AddCommandHistory(connectionId: string, command: string, privateSession: boolean): Promise<void>
  DeleteCommandHistory(connectionId: string, command: string): Promise<void>
  SuggestCommands(connectionId: string, prefix: string): Promise<CommandHistory[]>
  StartSSH(request: {
    connectionId: string
    columns: number
    rows: number
    interactionResponses: string[]
    credentialOverride?: {
      name?: string
      type?: Connection['authentication']
      password?: string
      privateKeyPem?: string
      passphrase?: string
    }
  }): Promise<TerminalStart>
  AcceptHostKey(id: string): Promise<void>
  ResizeSSH(sessionId: string, columns: number, rows: number): Promise<void>
  CloseSSH(sessionId: string): Promise<void>
  AnswerSSHChallenge(id: string, answers: string[], cancelled: boolean): Promise<void>
  ListRemote(connectionId: string, remotePath: string): Promise<RemoteEntry[]>
  ChooseLocalDirectory(): Promise<{ token: string; path: string; items: RemoteEntry[] }>
  ListLocal(token: string, relativePath: string): Promise<RemoteEntry[]>
  CreateLocalDirectory(token: string, relativePath: string): Promise<void>
  RenameLocal(token: string, oldRelativePath: string, newRelativePath: string): Promise<void>
  DeleteLocal(token: string, relativePath: string, directory: boolean): Promise<void>
  UploadGranted(connectionId: string, token: string, localRelativePath: string, remotePath: string): Promise<void>
  DownloadGranted(connectionId: string, remotePath: string, token: string, localRelativePath: string): Promise<void>
  StartSFTPUpload(
    connectionId: string, token: string, localRelativePath: string,
    remotePath: string, overwrite: boolean
  ): Promise<SFTPTransfer>
  StartSFTPDownload(
    connectionId: string, remotePath: string, token: string,
    localRelativePath: string, overwrite: boolean
  ): Promise<SFTPTransfer>
  ListSFTPTransfers(): Promise<SFTPTransfer[]>
  PauseSFTPTransfer(id: string): Promise<void>
  ResumeSFTPTransfer(id: string): Promise<void>
  CancelSFTPTransfer(id: string): Promise<void>
  BeginZmodemReceive(name: string, size: number): Promise<string>
  WriteZmodemReceive(id: string, data: number[] | Uint8Array): Promise<void>
  FinishZmodemReceive(id: string): Promise<void>
  CancelZmodemReceive(id: string): Promise<void>
  CreateRemoteDirectory(connectionId: string, remotePath: string): Promise<void>
  RenameRemote(connectionId: string, oldPath: string, newPath: string): Promise<void>
  DeleteRemote(connectionId: string, remotePath: string, directory: boolean): Promise<void>
  UploadFile(connectionId: string, remotePath: string): Promise<SFTPTransfer>
  DownloadFile(connectionId: string, remotePath: string, suggestedName: string): Promise<SFTPTransfer>
  BootstrapAccount(serverUrl: string, username: string, password: string):
    Promise<{ accessToken: string; refreshToken: string; accessExpiresAt: string; refreshExpiresAt: string }>
  SyncServerStatus(serverUrl: string): Promise<{
    serverInitialized: boolean
    syncInitialized: boolean
    recoveryUpdatedAt?: string
    recoveryGeneration?: number
  }>
  InitializeSync(deviceName: string): Promise<{ deviceId: string; recoveryCode: string }>
  RecoverSync(
    serverUrl: string, username: string, password: string, totpCode: string,
    deviceName: string, recoveryCode: string
  ): Promise<{ deviceId: string; recoveryCode: string }>
  RotateSyncRecoveryCode(password: string, totpCode: string): Promise<string>
  LoginAccount(
    serverUrl: string, username: string, password: string, deviceId: string, secondFactor: string
  ): Promise<void>
  LogoutAccount(): Promise<void>
  ResetSync(password: string, totpCode: string): Promise<void>
  SyncNow(syncSecrets: boolean, syncHistory: boolean):
    Promise<{ pushed: number; pulled: number; conflicts: number; cursor: number }>
  BeginDevicePairing(serverUrl: string, deviceName: string): Promise<{
    pairingId: string; deviceId: string; shortCode: string
    approvalCode: string; expiresAt: string
  }>
  ApproveDevicePairing(approvalCode: string): Promise<void>
  ClaimDevicePairing(username: string, password: string, totpCode: string):
    Promise<{ approved: boolean; deviceId?: string }>
  ListSyncDevices(): Promise<Array<{
    id: string; name: string; approved: boolean; revoked: boolean
    createdAt: string; lastSeenAt: string
  }>>
  RevokeSyncDevice(deviceId: string): Promise<void>
  SetDeviceName(deviceName: string): Promise<void>
  BeginSyncTOTPSetup(): Promise<{ secret: string; setupToken: string; uri: string }>
  ConfirmSyncTOTPSetup(setupToken: string, code: string): Promise<string[]>
  DisableSyncTOTP(password: string, code: string): Promise<void>
  SetSyncAutoEnabled(enabled: boolean): Promise<void>
  LeaveSync(password: string, totpCode: string): Promise<void>
}

declare global {
  interface Window {
    go?: { app?: { App?: Backend } }
    runtime?: {
      EventsOn?: (name: string, callback: (...args: unknown[]) => void) => () => void
      BrowserOpenURL?: (url: string) => void
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
