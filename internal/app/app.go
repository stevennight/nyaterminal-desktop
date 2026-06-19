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
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	mu          sync.RWMutex
	ctx         context.Context
	dataDir     string
	initErr     error
	vault       *vault.Vault
	store       *store.Store
	ssh         *sshclient.Manager
	sftp        *sftpclient.Service
	sync        *syncclient.Client
	challengeMu sync.Mutex
	challenges  map[string]chan challengeResponse
}

type challengeResponse struct {
	answers   []string
	cancelled bool
}

type Bootstrap struct {
	Vault       vault.Status       `json:"vault"`
	Groups      []model.Group      `json:"groups,omitempty"`
	Tags        []model.Tag        `json:"tags,omitempty"`
	Connections []model.Connection `json:"connections,omitempty"`
	Settings    *model.Settings    `json:"settings,omitempty"`
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
		v.Close()
		a.initErr = err
		return err
	}
	a.vault = v
	a.store = dataStore
	a.ssh = sshManager
	a.sftp = sftpclient.New(sshManager)
	a.sync = syncclient.New(v)
	sshManager.SetInteractiveHandler(a.handleInteractiveChallenge)
	return nil
}

func (a *App) Startup(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()
	if err := a.initialize(); err != nil {
		runtime.LogErrorf(ctx, "cannot initialize application: %v", err)
	}
}

func (a *App) Shutdown(context.Context) {
	_ = a.Close()
}

func (a *App) Close() error {
	if a.ssh != nil {
		_ = a.ssh.Close()
	}
	if a.vault != nil {
		return a.vault.Close()
	}
	return nil
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
	return result, nil
}

func (a *App) InitializeVault(password string) error {
	return a.vault.Initialize(a.context(), password)
}

func (a *App) Unlock(password string) error {
	err := a.vault.Unlock(a.context(), password)
	if errors.Is(err, vault.ErrInvalidPassword) {
		return a.vault.UnlockWithLockPassword(a.context(), password)
	}
	return err
}

func (a *App) UnlockWithSystem() error {
	return a.vault.UnlockQuick(a.context(), "default")
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
	return a.store.PutGroup(a.context(), value)
}

func (a *App) ListTags() ([]model.Tag, error) {
	return a.store.ListTags(a.context())
}

func (a *App) SaveTag(value model.Tag) (model.Tag, error) {
	return a.store.PutTag(a.context(), value)
}

func (a *App) ListConnections() ([]model.Connection, error) {
	return a.store.ListConnections(a.context())
}

func (a *App) SaveConnection(value model.Connection) (model.Connection, error) {
	return a.store.PutConnection(a.context(), value)
}

func (a *App) SaveCredential(value model.Credential) (model.Credential, error) {
	return a.store.PutCredential(a.context(), value)
}

func (a *App) DeleteRecord(id string) error {
	return a.store.Delete(a.context(), id)
}

func (a *App) GetSettings() (model.Settings, error) {
	return a.store.Settings(a.context())
}

func (a *App) SaveSettings(value model.Settings) error {
	return a.store.PutSettings(a.context(), value)
}

func (a *App) AddCommandHistory(connectionID, command string, private bool) error {
	return a.store.AddCommand(a.context(), connectionID, command, private)
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

func (a *App) UploadFile(connectionID, remoteDirectory string) error {
	localPath, err := runtime.OpenFileDialog(a.context(), runtime.OpenDialogOptions{
		Title: "选择要上传的文件",
	})
	if err != nil || localPath == "" {
		return err
	}
	remotePath := filepath.ToSlash(filepath.Join(remoteDirectory, filepath.Base(localPath)))
	return a.sftp.Upload(a.context(), connectionID, localPath, remotePath)
}

func (a *App) DownloadFile(connectionID, remotePath, suggestedName string) error {
	localPath, err := runtime.SaveFileDialog(a.context(), runtime.SaveDialogOptions{
		Title: "保存下载文件", DefaultFilename: filepath.Base(suggestedName),
	})
	if err != nil || localPath == "" {
		return err
	}
	return a.sftp.Download(a.context(), connectionID, remotePath, localPath)
}

func (a *App) InitializeSync(serverURL, username, password, deviceName string) (syncclient.SetupResult, error) {
	return a.sync.Initialize(a.context(), serverURL, username, password, deviceName)
}

func (a *App) LoginSync(serverURL, username, password, deviceID string) error {
	return a.sync.Login(a.context(), serverURL, username, password, deviceID)
}

func (a *App) SyncNow(syncSecrets, syncHistory bool) (syncclient.SyncResult, error) {
	return a.sync.Sync(a.context(), syncSecrets, syncHistory)
}

func (a *App) BeginDevicePairing(
	serverURL, deviceName string,
) (syncclient.PairingStart, error) {
	return a.sync.BeginPairing(a.context(), serverURL, deviceName)
}

func (a *App) ApproveDevicePairing(qrPayload string) error {
	return a.sync.ApprovePairing(a.context(), qrPayload)
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

func (a *App) BeginSyncTOTPSetup() (syncclient.TOTPSetup, error) {
	return a.sync.BeginTOTPSetup(a.context())
}

func (a *App) ConfirmSyncTOTPSetup(setupToken, code string) ([]string, error) {
	return a.sync.ConfirmTOTPSetup(a.context(), setupToken, code)
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
