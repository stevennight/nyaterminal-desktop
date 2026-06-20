package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nyaterminal/nyaterminal/desktop/internal/model"
	"github.com/nyaterminal/nyaterminal/desktop/internal/sftpclient"
	"github.com/nyaterminal/nyaterminal/desktop/internal/sshclient"
	"github.com/nyaterminal/nyaterminal/desktop/internal/store"
	"github.com/nyaterminal/nyaterminal/desktop/internal/syncclient"
	"github.com/nyaterminal/nyaterminal/desktop/internal/vault"
	"github.com/nyaterminal/nyaterminal/desktop/internal/zmodemstore"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	mu                 sync.RWMutex
	ctx                context.Context
	dataDir            string
	initErr            error
	vault              *vault.Vault
	store              *store.Store
	ssh                *sshclient.Manager
	sftp               *sftpclient.Service
	sync               *syncclient.Client
	zmodem             *zmodemstore.Store
	syncMu             sync.Mutex
	syncCloseOnce      sync.Once
	syncRunning        bool
	syncPending        bool
	syncLastTrigger    time.Time
	syncStop           chan struct{}
	challengeMu        sync.Mutex
	challenges         map[string]chan challengeResponse
	unlockMu           sync.Mutex
	unlockFailures     int
	unlockBlockedUntil time.Time
}

type challengeResponse struct {
	answers   []string
	cancelled bool
}

type Bootstrap struct {
	Vault          vault.Status               `json:"vault"`
	Groups         []model.Group              `json:"groups,omitempty"`
	Tags           []model.Tag                `json:"tags,omitempty"`
	Connections    []model.Connection         `json:"connections,omitempty"`
	Settings       *model.Settings            `json:"settings,omitempty"`
	Account        *syncclient.AccountSummary `json:"account,omitempty"`
	SyncConfigured bool                       `json:"syncConfigured"`
	SyncSummary    *syncclient.Summary        `json:"syncSummary,omitempty"`
}

type TerminalStart struct {
	Session *sshclient.StartResult    `json:"session,omitempty"`
	HostKey *sshclient.PendingHostKey `json:"hostKey,omitempty"`
}

func New(dataDir string) *App {
	return &App{
		dataDir: dataDir, challenges: make(map[string]chan challengeResponse),
	}
}

func (a *App) initialize() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.vault != nil || a.initErr != nil {
		return a.initErr
	}
	dataDir := a.dataDir
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		a.initErr = err
		return err
	}
	v, err := vault.Open(filepath.Join(dataDir, "vault.db"))
	if err != nil {
		a.initErr = err
		return err
	}
	dataStore := store.New(v)
	sshManager, err := sshclient.NewManager(dataStore)
	if err != nil {
		if closeErr := v.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		a.initErr = err
		return err
	}
	a.vault = v
	a.store = dataStore
	a.ssh = sshManager
	a.sftp = sftpclient.New(sshManager)
	a.sync = syncclient.New(v)
	a.zmodem = zmodemstore.New()
	a.syncStop = make(chan struct{})
	sshManager.SetInteractiveHandler(a.handleInteractiveChallenge)
	return nil
}

func (a *App) Startup(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()
	if err := a.initialize(); err != nil {
		runtime.LogErrorf(ctx, "cannot initialize application: %v", err)
		return
	}
	go a.syncLoop()
}

func (a *App) Shutdown(_ context.Context) {
	_ = a.Close()
}

func (a *App) Close() error {
	a.syncCloseOnce.Do(func() {
		a.syncMu.Lock()
		if a.syncStop != nil {
			close(a.syncStop)
			a.syncStop = nil
		}
		a.syncMu.Unlock()
	})
	if a.zmodem != nil {
		a.zmodem.Close()
	}
	if a.sftp != nil {
		a.sftp.Close()
	}
	if a.ssh != nil {
		_ = a.ssh.Close()
	}
	if a.vault != nil {
		return a.vault.Close()
	}
	return nil
}

func (a *App) BeginZmodemReceive(name string, size int64) (string, error) {
	if name == "" {
		name = "transfer.bin"
	}
	localPath, err := runtime.SaveFileDialog(a.context(), runtime.SaveDialogOptions{
		Title: "保存 ZMODEM 文件", DefaultFilename: filepath.Base(name),
	})
	if err != nil || localPath == "" {
		return "", err
	}
	return a.zmodem.Begin(localPath, size)
}

func (a *App) WriteZmodemReceive(id string, data []byte) error {
	if len(data) > 1<<20 {
		return errors.New("ZMODEM chunk is too large")
	}
	return a.zmodem.Write(id, data)
}

func (a *App) FinishZmodemReceive(id string) error {
	return a.zmodem.Finish(id)
}

func (a *App) CancelZmodemReceive(id string) error {
	return a.zmodem.Cancel(id)
}

func (a *App) Bootstrap() (Bootstrap, error) {
	if err := a.ready(); err != nil {
		return Bootstrap{}, err
	}
	status, err := a.vault.Status(a.context())
	if err != nil {
		return Bootstrap{}, err
	}
	result := Bootstrap{Vault: status}
	if status.Locked {
		return result, nil
	}
	result.Groups, err = a.store.ListGroups(a.context())
	if err != nil {
		return Bootstrap{}, err
	}
	result.Connections, err = a.store.ListConnections(a.context())
	if err != nil {
		return Bootstrap{}, err
	}
	result.Tags, err = a.store.ListTags(a.context())
	if err != nil {
		return Bootstrap{}, err
	}
	settings, err := a.store.Settings(a.context())
	if err != nil {
		return Bootstrap{}, err
	}
	result.Settings = &settings
	if account, err := a.sync.AccountSummary(a.context()); err == nil {
		result.Account = &account
	}
	result.SyncConfigured = a.sync.Configured(a.context())
	if summary, err := a.sync.Summary(a.context()); err == nil && (summary.ServerURL != "" || summary.Configured || summary.LoggedIn) {
		result.SyncSummary = &summary
	}
	return result, nil
}

func (a *App) InitializeVault(password string) error {
	return a.vault.Initialize(a.context(), password)
}

func (a *App) Unlock(password string) error {
	if err := a.allowUnlock(); err != nil {
		return err
	}
	err := a.vault.Unlock(a.context(), password)
	if errors.Is(err, vault.ErrInvalidPassword) {
		err = a.vault.UnlockWithLockPassword(a.context(), password)
	}
	a.recordUnlockResult(err)
	return err
}

func (a *App) UnlockWithSystem() error {
	if err := a.allowUnlock(); err != nil {
		return err
	}
	err := a.vault.UnlockQuick(a.context(), "default")
	a.recordUnlockResult(err)
	return err
}

func (a *App) allowUnlock() error {
	a.unlockMu.Lock()
	defer a.unlockMu.Unlock()
	if time.Now().Before(a.unlockBlockedUntil) {
		return errors.New("too many unlock attempts; try again later")
	}
	return nil
}

func (a *App) recordUnlockResult(err error) {
	a.unlockMu.Lock()
	defer a.unlockMu.Unlock()
	if err == nil {
		a.unlockFailures = 0
		a.unlockBlockedUntil = time.Time{}
		return
	}
	a.unlockFailures++
	if a.unlockFailures >= 5 {
		delay := time.Duration(a.unlockFailures-4) * 15 * time.Second
		if delay > 5*time.Minute {
			delay = 5 * time.Minute
		}
		a.unlockBlockedUntil = time.Now().Add(delay)
	}
}

func (a *App) EnableSystemUnlock() error {
	return a.vault.EnableQuickUnlock(a.context(), "default")
}

func (a *App) DisableSystemUnlock() error {
	return a.vault.DisableQuickUnlock(a.context(), "default")
}

func (a *App) Lock() error {
	settings, err := a.store.Settings(a.context())
	if err == nil && settings.DisconnectOnLock {
		a.sftp.Close()
		_ = a.ssh.Close()
		newManager, managerErr := sshclient.NewManager(a.store)
		if managerErr == nil {
			a.ssh = newManager
			newManager.SetInteractiveHandler(a.handleInteractiveChallenge)
			a.sftp = sftpclient.New(newManager)
		}
	}
	a.vault.Lock()
	runtime.EventsEmit(a.context(), "vault:locked")
	return nil
}

func (a *App) ChangeMasterPassword(oldPassword, newPassword string) error {
	return a.vault.ChangePassword(a.context(), oldPassword, newPassword)
}

func (a *App) SetLockPassword(password string) error {
	return a.vault.SetLockPassword(a.context(), password)
}

func (a *App) ClearLockPassword() error {
	return a.vault.ClearLockPassword(a.context())
}

func (a *App) ListGroups() ([]model.Group, error) {
	return a.store.ListGroups(a.context())
}

func (a *App) SaveGroup(value model.Group) (model.Group, error) {
	result, err := a.store.PutGroup(a.context(), value)
	if err == nil {
		a.triggerSyncSoon()
	}
	return result, err
}

func (a *App) DeleteGroup(id string) error {
	return a.store.DeleteGroup(a.context(), id)
}

func (a *App) ListTags() ([]model.Tag, error) {
	return a.store.ListTags(a.context())
}

func (a *App) SaveTag(value model.Tag) (model.Tag, error) {
	result, err := a.store.PutTag(a.context(), value)
	if err == nil {
		a.triggerSyncSoon()
	}
	return result, err
}

func (a *App) DeleteTag(id string) error {
	return a.store.DeleteTag(a.context(), id)
}

func (a *App) ListConnections() ([]model.Connection, error) {
	return a.store.ListConnections(a.context())
}

func (a *App) SaveConnection(value model.Connection) (model.Connection, error) {
	result, err := a.store.PutConnection(a.context(), value)
	if err == nil {
		a.triggerSyncSoon()
	}
	return result, err
}

func (a *App) DeleteConnection(id string) error {
	return a.store.DeleteConnection(a.context(), id)
}

func (a *App) SaveCredential(value model.Credential) (model.Credential, error) {
	result, err := a.store.PutCredential(a.context(), value)
	if err == nil {
		a.triggerSyncSoon()
	}
	return result, err
}

func (a *App) DeleteRecord(id string) error {
	if err := a.store.Delete(a.context(), id); err != nil {
		return err
	}
	a.triggerSyncSoon()
	return nil
}

func (a *App) GetSettings() (model.Settings, error) {
	return a.store.Settings(a.context())
}

func (a *App) SaveSettings(value model.Settings) error {
	if err := a.store.PutSettings(a.context(), value); err != nil {
		return err
	}
	a.triggerSyncSoon()
	return nil
}

func (a *App) AddCommandHistory(connectionID, command string, private bool) error {
	if err := a.store.AddCommand(a.context(), connectionID, command, private); err != nil {
		return err
	}
	a.triggerSyncSoon()
	return nil
}

func (a *App) SuggestCommands(connectionID, prefix string) ([]model.CommandHistory, error) {
	return a.store.SuggestCommands(a.context(), connectionID, prefix, 20)
}

func (a *App) StartSSH(request sshclient.StartRequest) (TerminalStart, error) {
	result, err := a.ssh.Start(a.context(), request)
	if err == nil {
		return TerminalStart{Session: &result}, nil
	}
	var hostKeyError sshclient.HostKeyError
	if errors.As(err, &hostKeyError) {
		return TerminalStart{HostKey: &hostKeyError.Pending}, nil
	}
	return TerminalStart{}, err
}

func (a *App) AnswerSSHChallenge(id string, answers []string, cancelled bool) error {
	a.challengeMu.Lock()
	responseChannel := a.challenges[id]
	if responseChannel != nil {
		delete(a.challenges, id)
	}
	a.challengeMu.Unlock()
	if responseChannel == nil {
		return errors.New("SSH authentication challenge has expired")
	}
	responseChannel <- challengeResponse{
		answers: append([]string(nil), answers...), cancelled: cancelled,
	}
	return nil
}

func (a *App) AcceptHostKey(pendingID string) error {
	return a.ssh.AcceptHostKey(a.context(), pendingID)
}

func (a *App) ResizeSSH(sessionID string, columns, rows int) error {
	return a.ssh.Resize(sessionID, columns, rows)
}

func (a *App) CloseSSH(sessionID string) {
	a.ssh.CloseSession(sessionID)
}

func (a *App) ListRemote(connectionID, remotePath string) ([]sftpclient.Entry, error) {
	return a.sftp.ListRemote(a.context(), connectionID, remotePath)
}

func (a *App) ChooseLocalDirectory() (sftpclient.LocalLocation, error) {
	localPath, err := runtime.OpenDirectoryDialog(a.context(), runtime.OpenDialogOptions{
		Title: "选择本地 SFTP 工作目录",
	})
	if err != nil || localPath == "" {
		return sftpclient.LocalLocation{}, err
	}
	return a.sftp.GrantLocalDirectory(localPath)
}

func (a *App) ListLocal(token, relativePath string) ([]sftpclient.Entry, error) {
	return a.sftp.ListLocal(token, relativePath)
}

func (a *App) CreateLocalDirectory(token, relativePath string) error {
	return a.sftp.CreateLocalDirectory(token, relativePath)
}

func (a *App) RenameLocal(token, oldRelativePath, newRelativePath string) error {
	return a.sftp.RenameLocal(token, oldRelativePath, newRelativePath)
}

func (a *App) DeleteLocal(token, relativePath string, directory bool) error {
	return a.sftp.DeleteLocal(token, relativePath, directory)
}

func (a *App) UploadGranted(
	connectionID, token, localRelativePath, remotePath string,
) error {
	return a.sftp.UploadGranted(
		a.context(), connectionID, token, localRelativePath, remotePath,
	)
}

func (a *App) DownloadGranted(
	connectionID, remotePath, token, localRelativePath string,
) error {
	return a.sftp.DownloadGranted(
		a.context(), connectionID, remotePath, token, localRelativePath,
	)
}

func (a *App) StartSFTPUpload(
	connectionID, token, localRelativePath, remotePath string, overwrite bool,
) (sftpclient.Transfer, error) {
	return a.sftp.StartUploadGranted(
		connectionID, token, localRelativePath, remotePath, overwrite,
	)
}

func (a *App) StartSFTPDownload(
	connectionID, remotePath, token, localRelativePath string, overwrite bool,
) (sftpclient.Transfer, error) {
	return a.sftp.StartDownloadGranted(
		connectionID, remotePath, token, localRelativePath, overwrite,
	)
}

func (a *App) ListSFTPTransfers() []sftpclient.Transfer {
	return a.sftp.ListTransfers()
}

func (a *App) PauseSFTPTransfer(id string) error {
	return a.sftp.PauseTransfer(id)
}

func (a *App) ResumeSFTPTransfer(id string) error {
	return a.sftp.ResumeTransfer(id)
}

func (a *App) CancelSFTPTransfer(id string) error {
	return a.sftp.CancelTransfer(id)
}

func (a *App) CreateRemoteDirectory(connectionID, remotePath string) error {
	return a.sftp.CreateRemoteDirectory(a.context(), connectionID, remotePath)
}

func (a *App) RenameRemote(connectionID, oldPath, newPath string) error {
	return a.sftp.RenameRemote(a.context(), connectionID, oldPath, newPath)
}

func (a *App) DeleteRemote(connectionID, remotePath string, directory bool) error {
	return a.sftp.DeleteRemote(a.context(), connectionID, remotePath, directory)
}

func (a *App) UploadFile(connectionID, remoteDirectory string) (sftpclient.Transfer, error) {
	localPath, err := runtime.OpenFileDialog(a.context(), runtime.OpenDialogOptions{
		Title: "选择要上传的文件",
	})
	if err != nil || localPath == "" {
		return sftpclient.Transfer{}, err
	}
	remotePath := filepath.ToSlash(filepath.Join(remoteDirectory, filepath.Base(localPath)))
	return a.sftp.StartUpload(connectionID, localPath, remotePath, false)
}

func (a *App) DownloadFile(
	connectionID, remotePath, suggestedName string,
) (sftpclient.Transfer, error) {
	localPath, err := runtime.SaveFileDialog(a.context(), runtime.SaveDialogOptions{
		Title: "保存下载文件", DefaultFilename: filepath.Base(suggestedName),
	})
	if err != nil || localPath == "" {
		return sftpclient.Transfer{}, err
	}
	return a.sftp.StartDownload(connectionID, remotePath, localPath, true)
}

func (a *App) RecoverSync(
	serverURL, username, password, totpCode, deviceName, recoveryCode string,
) (syncclient.SetupResult, error) {
	return a.sync.Recover(
		a.context(), serverURL, username, password, totpCode, deviceName, recoveryCode,
	)
}

func (a *App) RotateSyncRecoveryCode(password, totpCode string) (string, error) {
	return a.sync.RotateRecoveryCode(a.context(), password, totpCode)
}

func (a *App) BootstrapAccount(serverURL, username, password string) (syncclient.TokenPair, error) {
	return a.sync.BootstrapAccount(a.context(), serverURL, username, password)
}

func (a *App) SyncServerStatus(serverURL string) (syncclient.RemoteStatus, error) {
	return a.sync.RemoteStatus(a.context(), serverURL)
}

func (a *App) InitializeSync(deviceName string) (syncclient.SetupResult, error) {
	return a.sync.InitializeSync(a.context(), deviceName)
}

func (a *App) LoginAccount(serverURL, username, password, deviceID, secondFactor string) error {
	return a.sync.LoginAccount(a.context(), serverURL, username, password, deviceID, secondFactor)
}

func (a *App) LogoutAccount() error {
	return a.sync.Logout(a.context())
}

func (a *App) ResetSync(password, totpCode string) error {
	return a.sync.ResetSync(a.context(), password, totpCode)
}

func (a *App) SyncNow(syncSecrets, syncHistory bool) (syncclient.SyncResult, error) {
	return a.sync.Sync(a.context(), syncSecrets, syncHistory)
}

func (a *App) BeginDevicePairing(
	serverURL, deviceName string,
) (syncclient.PairingStart, error) {
	return a.sync.BeginPairing(a.context(), serverURL, deviceName)
}

func (a *App) ApproveDevicePairing(approvalCode string) error {
	return a.sync.ApprovePairing(a.context(), approvalCode)
}

func (a *App) ClaimDevicePairing(
	username, password, totpCode string,
) (syncclient.PairingClaim, error) {
	return a.sync.ClaimPairing(a.context(), username, password, totpCode)
}

func (a *App) ListSyncDevices() ([]syncclient.Device, error) {
	return a.sync.ListDevices(a.context())
}

func (a *App) RevokeSyncDevice(deviceID string) error {
	return a.sync.RevokeDevice(a.context(), deviceID)
}

func (a *App) LeaveSync(password, totpCode string) error {
	return a.sync.LeaveSync(a.context(), password, totpCode)
}

func (a *App) BeginSyncTOTPSetup() (syncclient.TOTPSetup, error) {
	return a.sync.BeginTOTPSetup(a.context())
}

func (a *App) ConfirmSyncTOTPSetup(setupToken, code string) ([]string, error) {
	return a.sync.ConfirmTOTPSetup(a.context(), setupToken, code)
}

func (a *App) DisableSyncTOTP(password, code string) error {
	return a.sync.DisableTOTP(a.context(), password, code)
}

func (a *App) SetSyncAutoEnabled(enabled bool) error {
	return a.sync.SetAutoSyncEnabled(a.context(), enabled)
}

func (a *App) context() context.Context {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.ctx == nil {
		return context.Background()
	}
	return a.ctx
}

func (a *App) ready() error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.initErr != nil {
		return a.initErr
	}
	if a.vault == nil {
		return errors.New("application is not initialized")
	}
	return nil
}

func (a *App) handleInteractiveChallenge(
	ctx context.Context,
	challenge sshclient.InteractiveChallenge,
) ([]string, error) {
	responseChannel := make(chan challengeResponse, 1)
	a.challengeMu.Lock()
	a.challenges[challenge.ID] = responseChannel
	a.challengeMu.Unlock()
	runtime.EventsEmit(a.context(), "ssh:interactive-challenge", challenge)
	defer func() {
		a.challengeMu.Lock()
		delete(a.challenges, challenge.ID)
		a.challengeMu.Unlock()
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case response := <-responseChannel:
		if response.cancelled {
			return nil, errors.New("interactive authentication cancelled")
		}
		return response.answers, nil
	case <-time.After(2 * time.Minute):
		return nil, errors.New("interactive authentication timed out")
	}
}

func (a *App) syncLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-a.syncStop:
			return
		case <-ticker.C:
			a.triggerSyncSoon()
		}
	}
}

func (a *App) triggerSyncSoon() {
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	if a.syncStop == nil || a.syncRunning {
		a.syncPending = true
		return
	}
	if time.Since(a.syncLastTrigger) < 20*time.Second {
		a.syncPending = true
		return
	}
	a.syncLastTrigger = time.Now()
	go a.runSyncOnce()
}

func (a *App) runSyncOnce() {
	a.syncMu.Lock()
	if a.syncRunning || a.syncStop == nil {
		a.syncMu.Unlock()
		return
	}
	a.syncRunning = true
	a.syncMu.Unlock()

	defer func() {
		a.syncMu.Lock()
		a.syncRunning = false
		pending := a.syncPending
		a.syncPending = false
		a.syncMu.Unlock()
		if pending {
			a.triggerSyncSoon()
		}
	}()

	if a.vault == nil || !a.sync.AutoSyncEnabled(a.context()) {
		return
	}
	if !a.sync.LoggedIn(a.context()) || !a.sync.Configured(a.context()) {
		return
	}
	status, err := a.vault.Status(a.context())
	if err != nil || status.Locked {
		return
	}
	settings, err := a.store.Settings(a.context())
	if err != nil {
		return
	}
	_, _ = a.sync.Sync(a.context(), settings.SyncSecretsByDefault, settings.SyncCommandHistory)
}
