package syncclient

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nyaterminal/nyaterminal/desktop/internal/store"
	"github.com/nyaterminal/nyaterminal/desktop/internal/vault"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

const (
	profileID = "sync:profile"
	stateID   = "sync:state"
	pairingID = "sync:pending-pairing"
)

type Client struct {
	vault *vault.Vault
	http  *http.Client
}

type Profile struct {
	ServerURL          string    `json:"serverUrl"`
	Username           string    `json:"username"`
	DeviceID           string    `json:"deviceId"`
	DeviceName         string    `json:"deviceName"`
	AutoSyncEnabled    *bool     `json:"autoSyncEnabled,omitempty"`
	ExchangePrivateKey []byte    `json:"exchangePrivateKey"`
	ExchangePublicKey  []byte    `json:"exchangePublicKey"`
	SigningPrivateKey  []byte    `json:"signingPrivateKey"`
	SigningPublicKey   []byte    `json:"signingPublicKey"`
	SyncRootKey        []byte    `json:"syncRootKey"`
	AccessToken        string    `json:"accessToken"`
	RefreshToken       string    `json:"refreshToken"`
	AccessExpiresAt    time.Time `json:"accessExpiresAt"`
	RefreshExpiresAt   time.Time `json:"refreshExpiresAt"`
}

type State struct {
	Cursor       int64                   `json:"cursor"`
	Records      map[string]RecordState  `json:"records"`
	Deferred     map[string]serverRecord `json:"deferred,omitempty"`
	LastSyncedAt time.Time               `json:"lastSyncedAt,omitempty"`
	Sync         SyncStatus              `json:"sync,omitempty"`
}

type RecordState struct {
	Version   int64                 `json:"version"`
	Vector    map[string]int64      `json:"vector,omitempty"`
	Hash      []byte                `json:"hash"`
	Fields    map[string]FieldState `json:"fields,omitempty"`
	Tombstone bool                  `json:"tombstone,omitempty"`
}

type FieldState struct {
	Vector map[string]int64 `json:"vector"`
	Writer string           `json:"writer"`
	Hash   []byte           `json:"hash"`
}

type syncEnvelope struct {
	Format int                   `json:"_nyaSync"`
	Data   json.RawMessage       `json:"data"`
	Fields map[string]FieldState `json:"fields,omitempty"`
}

type SetupResult struct {
	DeviceID     string `json:"deviceId"`
	RecoveryCode string `json:"recoveryCode"`
}

type recoveryBundle struct {
	ID         string    `json:"id"`
	Generation int64     `json:"generation"`
	Salt       []byte    `json:"salt"`
	Nonce      []byte    `json:"nonce"`
	Ciphertext []byte    `json:"ciphertext"`
	Verifier   []byte    `json:"verifier"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type SyncResult struct {
	Pushed    int   `json:"pushed"`
	Pulled    int   `json:"pulled"`
	Conflicts int   `json:"conflicts"`
	Cursor    int64 `json:"cursor"`
}

type SyncStatus struct {
	LastAttemptAt time.Time `json:"lastAttemptAt,omitempty"`
	LastSuccessAt time.Time `json:"lastSuccessAt,omitempty"`
	LastPushed    int       `json:"lastPushed,omitempty"`
	LastPulled    int       `json:"lastPulled,omitempty"`
	LastConflicts int       `json:"lastConflicts,omitempty"`
	LastError     string    `json:"lastError,omitempty"`
	Running       bool      `json:"running"`
}

type Summary struct {
	Configured      bool      `json:"configured"`
	ServerURL       string    `json:"serverUrl,omitempty"`
	Username        string    `json:"username,omitempty"`
	DeviceName      string    `json:"deviceName,omitempty"`
	DeviceID        string    `json:"deviceId,omitempty"`
	AutoSyncEnabled bool      `json:"autoSyncEnabled"`
	LastSyncedAt    time.Time `json:"lastSyncedAt,omitempty"`
	LastAttemptAt   time.Time `json:"lastAttemptAt,omitempty"`
	LastSuccessAt   time.Time `json:"lastSuccessAt,omitempty"`
	LastPushed      int       `json:"lastPushed,omitempty"`
	LastPulled      int       `json:"lastPulled,omitempty"`
	LastConflicts   int       `json:"lastConflicts,omitempty"`
	LastError       string    `json:"lastError,omitempty"`
	Running         bool      `json:"running"`
}

type PairingStart struct {
	PairingID string    `json:"pairingId"`
	DeviceID  string    `json:"deviceId"`
	ShortCode string    `json:"shortCode"`
	QRPayload string    `json:"qrPayload"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type PairingClaim struct {
	Approved bool   `json:"approved"`
	DeviceID string `json:"deviceId,omitempty"`
}

type Device struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Approved   bool      `json:"approved"`
	Revoked    bool      `json:"revoked"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
}

type TOTPSetup struct {
	Secret     string `json:"secret"`
	SetupToken string `json:"setupToken"`
	URI        string `json:"uri"`
}

type pendingPairing struct {
	ServerURL          string    `json:"serverUrl"`
	DeviceID           string    `json:"deviceId"`
	DeviceName         string    `json:"deviceName"`
	PairingID          string    `json:"pairingId"`
	ClaimToken         string    `json:"claimToken"`
	ShortCode          string    `json:"shortCode"`
	QRPayload          string    `json:"qrPayload"`
	ExpiresAt          time.Time `json:"expiresAt"`
	ExchangePrivateKey []byte    `json:"exchangePrivateKey"`
	ExchangePublicKey  []byte    `json:"exchangePublicKey"`
	SigningPrivateKey  []byte    `json:"signingPrivateKey"`
	SigningPublicKey   []byte    `json:"signingPublicKey"`
}

type pairingQR struct {
	Version           int    `json:"version"`
	ServerURL         string `json:"serverUrl"`
	PairingID         string `json:"pairingId"`
	DeviceID          string `json:"deviceId"`
	DeviceName        string `json:"deviceName"`
	ExchangePublicKey []byte `json:"exchangePublicKey"`
	SigningPublicKey  []byte `json:"signingPublicKey"`
	ShortCode         string `json:"shortCode"`
}

type serverRecord struct {
	ID            string           `json:"id"`
	EntityType    string           `json:"entityType"`
	EntityID      string           `json:"entityId"`
	DeviceID      string           `json:"deviceId,omitempty"`
	Version       int64            `json:"version"`
	VersionVector map[string]int64 `json:"versionVector,omitempty"`
	LogicalTime   int64            `json:"logicalTime,omitempty"`
	Tombstone     bool             `json:"tombstone"`
	Nonce         []byte           `json:"nonce"`
	Ciphertext    []byte           `json:"ciphertext"`
	ContentHash   []byte           `json:"contentHash"`
	UpdatedAt     time.Time        `json:"updatedAt,omitempty"`
}

func New(v *vault.Vault) *Client {
	return &Client{
		vault: v,
		http:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) Initialize(ctx context.Context, serverURL, username, password, deviceName string) (SetupResult, error) {
	serverURL, err := validateServerURL(serverURL)
	if err != nil {
		return SetupResult{}, err
	}
	if strings.TrimSpace(deviceName) == "" {
		return SetupResult{}, errors.New("device name is required")
	}
	exchangePrivate, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return SetupResult{}, err
	}
	signingPublic, signingPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return SetupResult{}, err
	}
	syncRootKey := make([]byte, chacha20poly1305.KeySize)
	if _, err := rand.Read(syncRootKey); err != nil {
		return SetupResult{}, err
	}
	request := map[string]any{
		"username": username, "password": password, "deviceName": deviceName,
		"exchangePublicKey": exchangePrivate.PublicKey().Bytes(),
		"signingPublicKey":  signingPublic,
	}
	var response struct {
		DeviceID string    `json:"deviceId"`
		Tokens   tokenPair `json:"tokens"`
	}
	if err := c.request(ctx, http.MethodPost, serverURL+"/api/v1/auth/initialize", "", request, &response); err != nil {
		return SetupResult{}, err
	}
	profile := Profile{
		ServerURL: serverURL, Username: username, DeviceID: response.DeviceID,
		DeviceName: deviceName, AutoSyncEnabled: boolPtr(true),
		ExchangePrivateKey: exchangePrivate.Bytes(),
		ExchangePublicKey:  exchangePrivate.PublicKey().Bytes(),
		SigningPrivateKey:  signingPrivate, SigningPublicKey: signingPublic,
		SyncRootKey: syncRootKey, AccessToken: response.Tokens.AccessToken,
		RefreshToken:     response.Tokens.RefreshToken,
		AccessExpiresAt:  response.Tokens.AccessExpiresAt,
		RefreshExpiresAt: response.Tokens.RefreshExpiresAt,
	}
	if err := c.vault.Put(ctx, store.TypeSyncProfile, profileID, profile); err != nil {
		return SetupResult{}, err
	}
	if err := c.vault.Put(ctx, store.TypeSyncState, stateID, State{Records: map[string]RecordState{}}); err != nil {
		return SetupResult{}, err
	}
	recoveryCode, bundle, err := createRecoveryBundle(syncRootKey, 1)
	if err != nil {
		return SetupResult{}, err
	}
	if err := c.authorizedRequest(ctx, &profile, http.MethodPut, "/api/v1/recovery", bundle, nil); err != nil {
		return SetupResult{}, err
	}
	if err := c.saveProfile(ctx, profile); err != nil {
		return SetupResult{}, err
	}
	return SetupResult{DeviceID: profile.DeviceID, RecoveryCode: recoveryCode}, nil
}

func (c *Client) Recover(
	ctx context.Context,
	serverURL, username, password, totpCode, deviceName, recoveryCode string,
) (SetupResult, error) {
	serverURL, err := validateServerURL(serverURL)
	if err != nil {
		return SetupResult{}, err
	}
	if strings.TrimSpace(deviceName) == "" {
		return SetupResult{}, errors.New("device name is required")
	}
	var challenge struct {
		Generation int64  `json:"generation"`
		Salt       []byte `json:"salt"`
	}
	if err := c.request(
		ctx, http.MethodGet, serverURL+"/api/v1/recovery/challenge", "",
		nil, &challenge,
	); err != nil {
		return SetupResult{}, err
	}
	wrappingKey, verifier, err := recoveryMaterial(recoveryCode, challenge.Salt)
	if err != nil {
		return SetupResult{}, err
	}
	defer wipe(wrappingKey)

	exchangePrivate, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return SetupResult{}, err
	}
	signingPublic, signingPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return SetupResult{}, err
	}
	var response struct {
		DeviceID string         `json:"deviceId"`
		Tokens   tokenPair      `json:"tokens"`
		Bundle   recoveryBundle `json:"bundle"`
	}
	if err := c.request(
		ctx, http.MethodPost, serverURL+"/api/v1/recovery/restore", "",
		map[string]any{
			"username": username, "password": password, "totpCode": totpCode,
			"deviceName":        deviceName,
			"exchangePublicKey": exchangePrivate.PublicKey().Bytes(),
			"signingPublicKey":  signingPublic,
			"generation":        challenge.Generation, "verifier": verifier,
		}, &response,
	); err != nil {
		return SetupResult{}, err
	}
	if response.Bundle.Generation != challenge.Generation ||
		!bytes.Equal(response.Bundle.Salt, challenge.Salt) ||
		!bytes.Equal(response.Bundle.Verifier, verifier) {
		return SetupResult{}, errors.New("recovery bundle changed during recovery")
	}
	syncRootKey, err := open(
		wrappingKey, response.Bundle.Nonce, response.Bundle.Ciphertext,
		[]byte("nyaterminal:recovery:v1"),
	)
	if err != nil || len(syncRootKey) != chacha20poly1305.KeySize {
		wipe(syncRootKey)
		return SetupResult{}, errors.New("recovery code is invalid")
	}
	defer wipe(syncRootKey)
	profile := Profile{
		ServerURL: serverURL, Username: username, DeviceID: response.DeviceID,
		DeviceName: deviceName, AutoSyncEnabled: boolPtr(true),
		ExchangePrivateKey: exchangePrivate.Bytes(),
		ExchangePublicKey:  exchangePrivate.PublicKey().Bytes(),
		SigningPrivateKey:  signingPrivate, SigningPublicKey: signingPublic,
		SyncRootKey: append([]byte(nil), syncRootKey...),
		AccessToken: response.Tokens.AccessToken, RefreshToken: response.Tokens.RefreshToken,
		AccessExpiresAt:  response.Tokens.AccessExpiresAt,
		RefreshExpiresAt: response.Tokens.RefreshExpiresAt,
	}
	if err := c.saveProfile(ctx, profile); err != nil {
		return SetupResult{}, err
	}
	if err := c.vault.Put(ctx, store.TypeSyncState, stateID, State{
		Records: map[string]RecordState{}, Deferred: map[string]serverRecord{},
	}); err != nil {
		return SetupResult{}, err
	}
	return SetupResult{DeviceID: response.DeviceID}, nil
}

func (c *Client) RotateRecoveryCode(ctx context.Context) (string, error) {
	profile, _, err := c.load(ctx)
	if err != nil {
		return "", err
	}
	var current recoveryBundle
	if err := c.authorizedRequest(
		ctx, &profile, http.MethodGet, "/api/v1/recovery", nil, &current,
	); err != nil {
		return "", err
	}
	code, bundle, err := createRecoveryBundle(profile.SyncRootKey, current.Generation+1)
	if err != nil {
		return "", err
	}
	if err := c.authorizedRequest(
		ctx, &profile, http.MethodPut, "/api/v1/recovery", bundle, nil,
	); err != nil {
		return "", err
	}
	if err := c.saveProfile(ctx, profile); err != nil {
		return "", err
	}
	return code, nil
}

func (c *Client) Login(ctx context.Context, serverURL, username, password, deviceID string) error {
	serverURL, err := validateServerURL(serverURL)
	if err != nil {
		return err
	}
	var profile Profile
	if err := c.vault.Get(ctx, store.TypeSyncProfile, profileID, &profile); err != nil {
		return errors.New("this device has not been paired")
	}
	var tokens tokenPair
	err = c.request(ctx, http.MethodPost, serverURL+"/api/v1/auth/login", "", map[string]any{
		"username": username, "password": password, "deviceId": deviceID,
	}, &tokens)
	if err != nil {
		return err
	}
	profile.ServerURL = serverURL
	profile.Username = username
	profile.DeviceID = deviceID
	profile.AccessToken = tokens.AccessToken
	profile.RefreshToken = tokens.RefreshToken
	profile.AccessExpiresAt = tokens.AccessExpiresAt
	profile.RefreshExpiresAt = tokens.RefreshExpiresAt
	return c.saveProfile(ctx, profile)
}

func (c *Client) BeginPairing(
	ctx context.Context, serverURL, deviceName string,
) (PairingStart, error) {
	serverURL, err := validateServerURL(serverURL)
	if err != nil {
		return PairingStart{}, err
	}
	if strings.TrimSpace(deviceName) == "" {
		return PairingStart{}, errors.New("device name is required")
	}
	exchangePrivate, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return PairingStart{}, err
	}
	signingPublic, signingPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return PairingStart{}, err
	}
	var response struct {
		PairingID  string    `json:"pairingId"`
		DeviceID   string    `json:"deviceId"`
		ClaimToken string    `json:"claimToken"`
		ShortCode  string    `json:"shortCode"`
		QRPayload  string    `json:"qrPayload"`
		ExpiresAt  time.Time `json:"expiresAt"`
	}
	err = c.request(ctx, http.MethodPost, serverURL+"/api/v1/devices/pairing", "", map[string]any{
		"name": deviceName, "exchangePublicKey": exchangePrivate.PublicKey().Bytes(),
		"signingPublicKey": signingPublic,
	}, &response)
	if err != nil {
		return PairingStart{}, err
	}
	pending := pendingPairing{
		ServerURL: serverURL, DeviceID: response.DeviceID, DeviceName: deviceName,
		PairingID: response.PairingID, ClaimToken: response.ClaimToken,
		ShortCode: response.ShortCode, QRPayload: response.QRPayload,
		ExpiresAt: response.ExpiresAt, ExchangePrivateKey: exchangePrivate.Bytes(),
		ExchangePublicKey: exchangePrivate.PublicKey().Bytes(),
		SigningPrivateKey: signingPrivate, SigningPublicKey: signingPublic,
	}
	if err := c.vault.Put(ctx, store.TypePairing, pairingID, pending); err != nil {
		return PairingStart{}, err
	}
	return PairingStart{
		PairingID: response.PairingID, DeviceID: response.DeviceID,
		ShortCode: response.ShortCode, QRPayload: response.QRPayload,
		ExpiresAt: response.ExpiresAt,
	}, nil
}

func (c *Client) ApprovePairing(ctx context.Context, qrPayload string) error {
	profile, _, err := c.load(ctx)
	if err != nil {
		return err
	}
	var qr pairingQR
	if err := json.Unmarshal([]byte(qrPayload), &qr); err != nil {
		return errors.New("invalid pairing QR payload")
	}
	serverURL, err := validateServerURL(qr.ServerURL)
	if err != nil || serverURL != profile.ServerURL || qr.Version != 1 {
		return errors.New("pairing request belongs to a different server")
	}
	var remote pairingQR
	if err := c.authorizedRequest(
		ctx, &profile, http.MethodGet, "/api/v1/devices/pairing/"+url.PathEscape(qr.PairingID),
		nil, &remote,
	); err != nil {
		return err
	}
	if remote.PairingID != qr.PairingID || remote.DeviceID != qr.DeviceID ||
		remote.ShortCode != qr.ShortCode ||
		!bytes.Equal(remote.ExchangePublicKey, qr.ExchangePublicKey) ||
		!bytes.Equal(remote.SigningPublicKey, qr.SigningPublicKey) {
		return errors.New("pairing request changed after QR generation")
	}
	oldPrivate, err := ecdh.X25519().NewPrivateKey(profile.ExchangePrivateKey)
	if err != nil {
		return err
	}
	newPublic, err := ecdh.X25519().NewPublicKey(qr.ExchangePublicKey)
	if err != nil {
		return err
	}
	shared, err := oldPrivate.ECDH(newPublic)
	if err != nil {
		return err
	}
	packageKey, err := derivePairingKey(shared, qr.PairingID)
	wipe(shared)
	if err != nil {
		return err
	}
	defer wipe(packageKey)
	plain, err := json.Marshal(map[string]any{
		"syncRootKey":    profile.SyncRootKey,
		"targetDeviceId": qr.DeviceID,
		"issuedAt":       time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	defer wipe(plain)
	nonce, ciphertext, err := seal(packageKey, plain, []byte("nyaterminal:pairing-package:v1:"+qr.PairingID))
	if err != nil {
		return err
	}
	message := pairingApprovalMessage(
		qr.PairingID, qr.DeviceID, profile.DeviceID,
		profile.ExchangePublicKey, nonce, ciphertext,
	)
	signature := ed25519.Sign(ed25519.PrivateKey(profile.SigningPrivateKey), message)
	err = c.authorizedRequest(
		ctx, &profile, http.MethodPost,
		"/api/v1/devices/pairing/"+url.PathEscape(qr.PairingID)+"/approve",
		map[string]any{"nonce": nonce, "ciphertext": ciphertext, "signature": signature}, nil,
	)
	if err != nil {
		return err
	}
	return c.saveProfile(ctx, profile)
}

func (c *Client) ClaimPairing(
	ctx context.Context, username, password, totpCode string,
) (PairingClaim, error) {
	var pending pendingPairing
	if err := c.vault.Get(ctx, store.TypePairing, pairingID, &pending); err != nil {
		return PairingClaim{}, errors.New("there is no pending pairing request")
	}
	if time.Now().UTC().After(pending.ExpiresAt) {
		return PairingClaim{}, errors.New("pairing request has expired")
	}
	var response struct {
		Approved            bool   `json:"approved"`
		DeviceID            string `json:"deviceId"`
		ApproverDeviceID    string `json:"approverDeviceId"`
		ApproverExchangeKey []byte `json:"approverExchangeKey"`
		ApproverSigningKey  []byte `json:"approverSigningKey"`
		Nonce               []byte `json:"nonce"`
		Ciphertext          []byte `json:"ciphertext"`
		Signature           []byte `json:"signature"`
	}
	err := c.request(
		ctx, http.MethodPost,
		pending.ServerURL+"/api/v1/devices/pairing/"+url.PathEscape(pending.PairingID)+"/claim",
		"", map[string]string{"claimToken": pending.ClaimToken}, &response,
	)
	if err != nil {
		return PairingClaim{}, err
	}
	if !response.Approved {
		return PairingClaim{Approved: false}, nil
	}
	message := pairingApprovalMessage(
		pending.PairingID, pending.DeviceID, response.ApproverDeviceID,
		response.ApproverExchangeKey, response.Nonce, response.Ciphertext,
	)
	if len(response.ApproverSigningKey) != ed25519.PublicKeySize ||
		!ed25519.Verify(response.ApproverSigningKey, message, response.Signature) {
		return PairingClaim{}, errors.New("pairing package signature is invalid")
	}
	newPrivate, err := ecdh.X25519().NewPrivateKey(pending.ExchangePrivateKey)
	if err != nil {
		return PairingClaim{}, err
	}
	oldPublic, err := ecdh.X25519().NewPublicKey(response.ApproverExchangeKey)
	if err != nil {
		return PairingClaim{}, err
	}
	shared, err := newPrivate.ECDH(oldPublic)
	if err != nil {
		return PairingClaim{}, err
	}
	packageKey, err := derivePairingKey(shared, pending.PairingID)
	wipe(shared)
	if err != nil {
		return PairingClaim{}, err
	}
	defer wipe(packageKey)
	plain, err := open(
		packageKey, response.Nonce, response.Ciphertext,
		[]byte("nyaterminal:pairing-package:v1:"+pending.PairingID),
	)
	if err != nil {
		return PairingClaim{}, errors.New("pairing package authentication failed")
	}
	defer wipe(plain)
	var contents struct {
		SyncRootKey    []byte    `json:"syncRootKey"`
		TargetDeviceID string    `json:"targetDeviceId"`
		IssuedAt       time.Time `json:"issuedAt"`
	}
	if err := json.Unmarshal(plain, &contents); err != nil ||
		contents.TargetDeviceID != pending.DeviceID ||
		len(contents.SyncRootKey) != chacha20poly1305.KeySize ||
		time.Since(contents.IssuedAt) > 15*time.Minute {
		return PairingClaim{}, errors.New("pairing package contents are invalid")
	}
	var tokens tokenPair
	if err := c.request(ctx, http.MethodPost, pending.ServerURL+"/api/v1/auth/login", "", map[string]any{
		"username": username, "password": password, "deviceId": pending.DeviceID,
		"totpCode": totpCode,
	}, &tokens); err != nil {
		return PairingClaim{}, err
	}
	profile := Profile{
		ServerURL: pending.ServerURL, Username: username,
		DeviceID: pending.DeviceID, DeviceName: pending.DeviceName,
		ExchangePrivateKey: pending.ExchangePrivateKey,
		ExchangePublicKey:  pending.ExchangePublicKey,
		SigningPrivateKey:  pending.SigningPrivateKey,
		SigningPublicKey:   pending.SigningPublicKey,
		SyncRootKey:        append([]byte(nil), contents.SyncRootKey...),
		AccessToken:        tokens.AccessToken, RefreshToken: tokens.RefreshToken,
		AccessExpiresAt: tokens.AccessExpiresAt, RefreshExpiresAt: tokens.RefreshExpiresAt,
	}
	if err := c.saveProfile(ctx, profile); err != nil {
		return PairingClaim{}, err
	}
	if err := c.vault.Put(ctx, store.TypeSyncState, stateID, State{Records: map[string]RecordState{}}); err != nil {
		return PairingClaim{}, err
	}
	if err := c.vault.Delete(ctx, pairingID); err != nil {
		return PairingClaim{}, err
	}
	return PairingClaim{Approved: true, DeviceID: pending.DeviceID}, nil
}

func (c *Client) ListDevices(ctx context.Context) ([]Device, error) {
	profile, _, err := c.load(ctx)
	if err != nil {
		return nil, err
	}
	var response struct {
		Devices []Device `json:"devices"`
	}
	if err := c.authorizedRequest(
		ctx, &profile, http.MethodGet, "/api/v1/devices", nil, &response,
	); err != nil {
		return nil, err
	}
	if err := c.saveProfile(ctx, profile); err != nil {
		return nil, err
	}
	return response.Devices, nil
}

func (c *Client) RevokeDevice(ctx context.Context, deviceID string) error {
	profile, _, err := c.load(ctx)
	if err != nil {
		return err
	}
	if err := c.authorizedRequest(
		ctx, &profile, http.MethodDelete,
		"/api/v1/devices/"+url.PathEscape(deviceID), nil, nil,
	); err != nil {
		return err
	}
	return c.saveProfile(ctx, profile)
}

func (c *Client) BeginTOTPSetup(ctx context.Context) (TOTPSetup, error) {
	profile, _, err := c.load(ctx)
	if err != nil {
		return TOTPSetup{}, err
	}
	var setup TOTPSetup
	if err := c.authorizedRequest(
		ctx, &profile, http.MethodPost, "/api/v1/auth/totp/setup", map[string]any{}, &setup,
	); err != nil {
		return TOTPSetup{}, err
	}
	if err := c.saveProfile(ctx, profile); err != nil {
		return TOTPSetup{}, err
	}
	return setup, nil
}

func (c *Client) ConfirmTOTPSetup(
	ctx context.Context, setupToken, code string,
) ([]string, error) {
	profile, _, err := c.load(ctx)
	if err != nil {
		return nil, err
	}
	var response struct {
		RecoveryCodes []string `json:"recoveryCodes"`
	}
	if err := c.authorizedRequest(
		ctx, &profile, http.MethodPost, "/api/v1/auth/totp/confirm",
		map[string]string{"setupToken": setupToken, "code": code}, &response,
	); err != nil {
		return nil, err
	}
	if err := c.saveProfile(ctx, profile); err != nil {
		return nil, err
	}
	return response.RecoveryCodes, nil
}

func (c *Client) DisableTOTP(ctx context.Context, password, code string) error {
	profile, _, err := c.load(ctx)
	if err != nil {
		return err
	}
	if err := c.authorizedRequest(
		ctx, &profile, http.MethodDelete, "/api/v1/auth/totp",
		map[string]string{"password": password, "code": code}, nil,
	); err != nil {
		return err
	}
	return c.saveProfile(ctx, profile)
}

func (c *Client) Sync(ctx context.Context, syncSecrets, syncHistory bool) (result SyncResult, err error) {
	profile, state, err := c.load(ctx)
	if err != nil {
		return SyncResult{}, err
	}
	state.Sync.Running = true
	state.Sync.LastAttemptAt = time.Now().UTC()
	if err := c.vault.Put(ctx, store.TypeSyncState, stateID, state); err != nil {
		return SyncResult{}, err
	}
	defer func() {
		state.Sync.Running = false
		if err != nil {
			state.Sync.LastError = err.Error()
		} else {
			state.Sync.LastError = ""
			state.Sync.LastSuccessAt = time.Now().UTC()
			state.Sync.LastPushed = result.Pushed
			state.Sync.LastPulled = result.Pulled
			state.Sync.LastConflicts = result.Conflicts
			state.LastSyncedAt = state.Sync.LastSuccessAt
		}
		if putErr := c.vault.Put(ctx, store.TypeSyncState, stateID, state); putErr != nil && err == nil {
			err = putErr
		}
		if saveErr := c.saveProfile(ctx, profile); saveErr != nil && err == nil {
			err = saveErr
		}
	}()
	allowed := map[string]bool{
		store.TypeGroup: true, store.TypeConnection: true, store.TypeTag: true,
		store.TypeSettings: true, store.TypeCredential: true,
	}
	if syncHistory {
		allowed[store.TypeHistory] = true
	}
	credentialOverrides, err := c.credentialSyncOverrides(ctx)
	if err != nil {
		return SyncResult{}, err
	}
	records, err := c.vault.ExportRecords(ctx, allowed)
	if err != nil {
		return SyncResult{}, err
	}
	defer func() {
		for index := range records {
			wipe(records[index].Data)
		}
	}()
	var outgoing []serverRecord
	var pushedDeletions []string
	for _, record := range records {
		if record.Type == store.TypeCredential &&
			!credentialPayloadAllowed(record.ID, record.Data, syncSecrets, credentialOverrides) {
			continue
		}
		plainHash := sha256.Sum256(record.Data)
		previous := state.Records[record.ID]
		if bytes.Equal(previous.Hash, plainHash[:]) {
			continue
		}
		vector := incrementVector(previous.Vector, profile.DeviceID, previous.Version)
		version := vector[profile.DeviceID]
		payload, fields, err := makeSyncEnvelope(record.Data, previous.Fields, vector, profile.DeviceID)
		if err != nil {
			return SyncResult{}, err
		}
		nonce, ciphertext, err := seal(
			profile.SyncRootKey, payload,
			syncRecordAAD(record.Type, record.ID, version, vector),
		)
		wipe(payload)
		if err != nil {
			return SyncResult{}, err
		}
		contentHash := sha256.Sum256(ciphertext)
		outgoing = append(outgoing, serverRecord{
			ID: uuid.NewString(), EntityType: record.Type, EntityID: record.ID,
			Version: version, VersionVector: vector,
			Nonce: nonce, Ciphertext: ciphertext, ContentHash: contentHash[:],
		})
		state.Records[record.ID] = RecordState{
			Version: version, Vector: vector, Hash: plainHash[:], Fields: fields,
		}
	}
	deletions, err := store.New(c.vault).ListDeletions(ctx)
	if err != nil {
		return SyncResult{}, err
	}
	for _, deletion := range deletions {
		previous, wasSynced := state.Records[deletion.EntityID]
		if !wasSynced || previous.Version == 0 {
			if err := c.vault.Delete(ctx, deletion.ID); err != nil {
				return SyncResult{}, err
			}
			continue
		}
		plain, err := json.Marshal(deletion)
		if err != nil {
			return SyncResult{}, err
		}
		vector := incrementVector(previous.Vector, profile.DeviceID, previous.Version)
		version := vector[profile.DeviceID]
		nonce, ciphertext, err := seal(
			profile.SyncRootKey, plain,
			syncRecordAAD(deletion.EntityType, deletion.EntityID, version, vector),
		)
		if err != nil {
			wipe(plain)
			return SyncResult{}, err
		}
		plainHash := sha256.Sum256(plain)
		wipe(plain)
		contentHash := sha256.Sum256(ciphertext)
		outgoing = append(outgoing, serverRecord{
			ID: uuid.NewString(), EntityType: deletion.EntityType,
			EntityID: deletion.EntityID, Version: version, Tombstone: true,
			VersionVector: vector,
			Nonce:         nonce, Ciphertext: ciphertext, ContentHash: contentHash[:],
		})
		state.Records[deletion.EntityID] = RecordState{
			Version: version, Vector: vector, Hash: plainHash[:], Tombstone: true,
		}
		pushedDeletions = append(pushedDeletions, deletion.ID)
	}
	if len(outgoing) > 0 {
		if err := c.authorizedRequest(ctx, &profile, http.MethodPost, "/api/v1/sync/push",
			map[string]any{"records": outgoing}, nil); err != nil {
			return SyncResult{}, err
		}
		for _, id := range pushedDeletions {
			if err := c.vault.Delete(ctx, id); err != nil {
				return SyncResult{}, err
			}
		}
	}
	pulled, next, conflicts, err := c.pull(
		ctx, &profile, &state, allowed, syncSecrets, credentialOverrides,
	)
	if err != nil {
		return SyncResult{}, err
	}
	state.Cursor = next
	result = SyncResult{
		Pushed: len(outgoing), Pulled: pulled, Conflicts: conflicts, Cursor: next,
	}
	return result, nil
}

func (c *Client) Configured(ctx context.Context) bool {
	var profile Profile
	return c.vault.Get(ctx, store.TypeSyncProfile, profileID, &profile) == nil &&
		profile.ServerURL != "" && profile.DeviceID != "" &&
		len(profile.SyncRootKey) == chacha20poly1305.KeySize
}

func (c *Client) AutoSyncEnabled(ctx context.Context) bool {
	var profile Profile
	if err := c.vault.Get(ctx, store.TypeSyncProfile, profileID, &profile); err != nil {
		return false
	}
	if profile.AutoSyncEnabled == nil {
		return true
	}
	return *profile.AutoSyncEnabled
}

func (c *Client) Summary(ctx context.Context) (Summary, error) {
	profile, state, err := c.load(ctx)
	if err != nil {
		return Summary{}, err
	}
	autoSyncEnabled := true
	if profile.AutoSyncEnabled != nil {
		autoSyncEnabled = *profile.AutoSyncEnabled
	}
	return Summary{
		Configured:      true,
		ServerURL:       profile.ServerURL,
		Username:        profile.Username,
		DeviceName:      profile.DeviceName,
		DeviceID:        profile.DeviceID,
		AutoSyncEnabled: autoSyncEnabled,
		LastSyncedAt:    state.LastSyncedAt,
		LastAttemptAt:   state.Sync.LastAttemptAt,
		LastSuccessAt:   state.Sync.LastSuccessAt,
		LastPushed:      state.Sync.LastPushed,
		LastPulled:      state.Sync.LastPulled,
		LastConflicts:   state.Sync.LastConflicts,
		LastError:       state.Sync.LastError,
		Running:         state.Sync.Running,
	}, nil
}

func (c *Client) SetAutoSyncEnabled(ctx context.Context, enabled bool) error {
	profile, _, err := c.load(ctx)
	if err != nil {
		return err
	}
	profile.AutoSyncEnabled = boolPtr(enabled)
	return c.saveProfile(ctx, profile)
}

func (c *Client) pull(
	ctx context.Context,
	profile *Profile,
	state *State,
	allowed map[string]bool,
	syncSecrets bool,
	credentialOverrides map[string]bool,
) (int, int64, int, error) {
	var response struct {
		Records []serverRecord `json:"records"`
		Next    int64          `json:"next"`
	}
	path := fmt.Sprintf("/api/v1/sync/pull?after=%d&limit=1000", state.Cursor)
	if err := c.authorizedRequest(ctx, profile, http.MethodGet, path, nil, &response); err != nil {
		return 0, state.Cursor, 0, err
	}
	local, err := c.vault.ExportRecords(ctx, allowed)
	if err != nil {
		return 0, state.Cursor, 0, err
	}
	localData := make(map[string][]byte, len(local))
	for _, record := range local {
		localData[record.ID] = append([]byte(nil), record.Data...)
		wipe(record.Data)
	}
	defer func() {
		for _, data := range localData {
			wipe(data)
		}
	}()
	if state.Deferred == nil {
		state.Deferred = make(map[string]serverRecord)
	}
	pending := make([]serverRecord, 0, len(state.Deferred)+len(response.Records))
	for _, record := range state.Deferred {
		pending = append(pending, record)
	}
	pending = append(pending, response.Records...)
	applied, conflicts := 0, 0
	for _, record := range pending {
		if !allowed[record.EntityType] {
			state.Deferred[record.ID] = record
			continue
		}
		delete(state.Deferred, record.ID)
		previous := state.Records[record.EntityID]
		remoteVector := normalizedRecordVector(record)
		relation := compareVectors(previous.Vector, remoteVector)
		if relation == vectorEqual || relation == vectorAfter {
			continue
		}
		plain, err := open(profile.SyncRootKey, record.Nonce, record.Ciphertext,
			syncRecordAAD(
				record.EntityType, record.EntityID, record.Version, record.VersionVector,
			))
		if err != nil {
			return applied, state.Cursor, conflicts, errors.New("synchronization ciphertext authentication failed")
		}
		remoteData, remoteFields, err := parseSyncEnvelope(
			plain, remoteVector, record.DeviceID,
		)
		wipe(plain)
		if err != nil {
			return applied, state.Cursor, conflicts, errors.New("synchronization payload is invalid")
		}
		if record.EntityType == store.TypeCredential &&
			!credentialPayloadAllowed(record.EntityID, remoteData, syncSecrets, credentialOverrides) {
			state.Deferred[record.ID] = record
			wipe(remoteData)
			continue
		}
		sum := sha256.Sum256(remoteData)
		localRecord := localData[record.EntityID]
		localSum := sha256.Sum256(localRecord)
		locallyChanged := previous.Version > 0 &&
			!previous.Tombstone &&
			!bytes.Equal(localSum[:], previous.Hash)
		concurrent := relation == vectorConcurrent || locallyChanged
		targetID := record.EntityID
		if !record.Tombstone && record.EntityType == store.TypeCredential &&
			concurrent {
			targetID = uuid.NewString()
			remoteData, err = makeCredentialConflict(remoteData, targetID)
			if err != nil {
				wipe(remoteData)
				return applied, state.Cursor, conflicts, err
			}
			conflicts++
		}
		finalData := remoteData
		finalFields := remoteFields
		finalVector := mergeVectors(previous.Vector, remoteVector)
		if concurrent && !record.Tombstone && record.EntityType != store.TypeCredential &&
			len(localRecord) > 0 {
			finalData, finalFields, err = mergeJSONFields(
				localRecord, remoteData, previous.Fields, remoteFields,
			)
			wipe(remoteData)
			if err != nil {
				return applied, state.Cursor, conflicts, err
			}
		}
		if !record.Tombstone {
			if err := c.vault.PutJSON(ctx, record.EntityType, targetID, finalData); err != nil {
				wipe(finalData)
				return applied, state.Cursor, conflicts, err
			}
			sum = sha256.Sum256(finalData)
			localData[targetID] = append([]byte(nil), finalData...)
		} else {
			if concurrent && len(localRecord) > 0 {
				// Preserve the local edit and force a causally newer resurrection
				// on the next push instead of silently accepting a concurrent delete.
				state.Records[targetID] = RecordState{
					Vector: finalVector, Version: maxVector(finalVector),
					Hash: nil, Fields: previous.Fields,
				}
				conflicts++
				wipe(finalData)
				applied++
				continue
			}
			if err := c.vault.Delete(ctx, targetID); err != nil {
				wipe(finalData)
				return applied, state.Cursor, conflicts, err
			}
			delete(localData, targetID)
		}
		wipe(finalData)
		state.Records[targetID] = RecordState{
			Version: maxVector(finalVector), Vector: finalVector,
			Hash: sum[:], Fields: finalFields, Tombstone: record.Tombstone,
		}
		applied++
	}
	return applied, response.Next, conflicts, nil
}

func (c *Client) credentialSyncOverrides(ctx context.Context) (map[string]bool, error) {
	connections, err := store.New(c.vault).ListConnections(ctx)
	if err != nil {
		return nil, err
	}
	overrides := make(map[string]bool)
	for _, connection := range connections {
		if connection.CredentialID != "" && connection.SyncSecrets != nil {
			overrides[connection.CredentialID] = *connection.SyncSecrets
		}
	}
	return overrides, nil
}

func credentialPayloadAllowed(
	credentialID string,
	data []byte,
	defaultValue bool,
	connectionOverrides map[string]bool,
) bool {
	var policy struct {
		SyncOverride *bool `json:"syncOverride"`
	}
	if json.Unmarshal(data, &policy) == nil && policy.SyncOverride != nil {
		return *policy.SyncOverride
	}
	if value, ok := connectionOverrides[credentialID]; ok {
		return value
	}
	return defaultValue
}

type vectorRelation int

const (
	vectorEqual vectorRelation = iota
	vectorBefore
	vectorAfter
	vectorConcurrent
)

func normalizedRecordVector(record serverRecord) map[string]int64 {
	if len(record.VersionVector) > 0 {
		return cloneVector(record.VersionVector)
	}
	if record.DeviceID != "" && record.Version > 0 {
		return map[string]int64{record.DeviceID: record.Version}
	}
	return map[string]int64{}
}

func incrementVector(previous map[string]int64, deviceID string, legacyVersion int64) map[string]int64 {
	vector := cloneVector(previous)
	if len(vector) == 0 && legacyVersion > 0 {
		vector[deviceID] = legacyVersion
	}
	vector[deviceID]++
	return vector
}

func cloneVector(source map[string]int64) map[string]int64 {
	result := make(map[string]int64, len(source)+1)
	for id, counter := range source {
		if counter > 0 {
			result[id] = counter
		}
	}
	return result
}

func mergeVectors(left, right map[string]int64) map[string]int64 {
	result := cloneVector(left)
	for id, counter := range right {
		if counter > result[id] {
			result[id] = counter
		}
	}
	return result
}

func compareVectors(left, right map[string]int64) vectorRelation {
	leftGreater, rightGreater := false, false
	for id, value := range left {
		if value > right[id] {
			leftGreater = true
		} else if value < right[id] {
			rightGreater = true
		}
	}
	for id, value := range right {
		if _, ok := left[id]; ok {
			continue
		}
		if value > 0 {
			rightGreater = true
		}
	}
	switch {
	case leftGreater && rightGreater:
		return vectorConcurrent
	case leftGreater:
		return vectorAfter
	case rightGreater:
		return vectorBefore
	default:
		return vectorEqual
	}
}

func maxVector(vector map[string]int64) int64 {
	var maximum int64
	for _, counter := range vector {
		if counter > maximum {
			maximum = counter
		}
	}
	return maximum
}

func makeSyncEnvelope(
	data []byte,
	previous map[string]FieldState,
	vector map[string]int64,
	writer string,
) ([]byte, map[string]FieldState, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return nil, nil, err
	}
	fields := make(map[string]FieldState, len(object)+len(previous))
	for name, value := range object {
		hash := sha256.Sum256(value)
		old, exists := previous[name]
		if exists && bytes.Equal(old.Hash, hash[:]) {
			fields[name] = old
			continue
		}
		fields[name] = FieldState{
			Vector: cloneVector(vector), Writer: writer, Hash: hash[:],
		}
	}
	for name := range previous {
		if _, exists := object[name]; !exists {
			fields[name] = FieldState{
				Vector: cloneVector(vector), Writer: writer, Hash: nil,
			}
		}
	}
	envelope := syncEnvelope{
		Format: 2, Data: append(json.RawMessage(nil), data...), Fields: fields,
	}
	payload, err := json.Marshal(envelope)
	wipe(envelope.Data)
	return payload, fields, err
}

func parseSyncEnvelope(
	payload []byte,
	fallbackVector map[string]int64,
	writer string,
) ([]byte, map[string]FieldState, error) {
	var envelope syncEnvelope
	if json.Unmarshal(payload, &envelope) == nil && envelope.Format == 2 &&
		json.Valid(envelope.Data) {
		return append([]byte(nil), envelope.Data...), envelope.Fields, nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(payload, &object); err != nil {
		return nil, nil, err
	}
	fields := make(map[string]FieldState, len(object))
	for name, value := range object {
		hash := sha256.Sum256(value)
		fields[name] = FieldState{
			Vector: cloneVector(fallbackVector), Writer: writer, Hash: hash[:],
		}
	}
	return append([]byte(nil), payload...), fields, nil
}

func mergeJSONFields(
	localData, remoteData []byte,
	localFields, remoteFields map[string]FieldState,
) ([]byte, map[string]FieldState, error) {
	var localObject, remoteObject map[string]json.RawMessage
	if err := json.Unmarshal(localData, &localObject); err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal(remoteData, &remoteObject); err != nil {
		return nil, nil, err
	}
	result := make(map[string]json.RawMessage, len(localObject)+len(remoteObject))
	fields := make(map[string]FieldState, len(localFields)+len(remoteFields))
	names := make(map[string]bool, len(localFields)+len(remoteFields))
	for name := range localFields {
		names[name] = true
	}
	for name := range remoteFields {
		names[name] = true
	}
	for name := range localObject {
		names[name] = true
	}
	for name := range remoteObject {
		names[name] = true
	}
	for name := range names {
		localClock, hasLocalClock := localFields[name]
		remoteClock, hasRemoteClock := remoteFields[name]
		useRemote := !hasLocalClock
		if hasLocalClock && hasRemoteClock {
			switch compareVectors(localClock.Vector, remoteClock.Vector) {
			case vectorBefore:
				useRemote = true
			case vectorConcurrent, vectorEqual:
				useRemote = remoteClock.Writer > localClock.Writer
			}
		}
		if useRemote {
			if value, exists := remoteObject[name]; exists && len(remoteClock.Hash) > 0 {
				result[name] = value
			}
			if hasRemoteClock {
				fields[name] = remoteClock
			}
		} else {
			if value, exists := localObject[name]; exists && len(localClock.Hash) > 0 {
				result[name] = value
			}
			if hasLocalClock {
				fields[name] = localClock
			}
		}
	}
	merged, err := json.Marshal(result)
	return merged, fields, err
}

type tokenPair struct {
	AccessToken      string    `json:"accessToken"`
	RefreshToken     string    `json:"refreshToken"`
	AccessExpiresAt  time.Time `json:"accessExpiresAt"`
	RefreshExpiresAt time.Time `json:"refreshExpiresAt"`
}

func (c *Client) authorizedRequest(ctx context.Context, profile *Profile, method, path string, request, response any) error {
	if time.Until(profile.AccessExpiresAt) < time.Minute {
		var tokens tokenPair
		if err := c.request(ctx, http.MethodPost, profile.ServerURL+"/api/v1/auth/refresh", "",
			map[string]string{"refreshToken": profile.RefreshToken}, &tokens); err != nil {
			return err
		}
		profile.AccessToken = tokens.AccessToken
		profile.RefreshToken = tokens.RefreshToken
		profile.AccessExpiresAt = tokens.AccessExpiresAt
		profile.RefreshExpiresAt = tokens.RefreshExpiresAt
		if err := c.saveProfile(ctx, *profile); err != nil {
			return err
		}
	}
	return c.request(ctx, method, profile.ServerURL+path, profile.AccessToken, request, response)
}

func (c *Client) request(ctx context.Context, method, endpoint, token string, request, response any) error {
	var body io.Reader
	if request != nil {
		encoded, err := json.Marshal(request)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	httpRequest.Header.Set("Accept", "application/json")
	if request != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+token)
		if method != http.MethodGet && method != http.MethodHead &&
			method != http.MethodOptions {
			nonce := make([]byte, 24)
			if _, err := rand.Read(nonce); err != nil {
				return err
			}
			httpRequest.Header.Set("X-Nya-Nonce", base64.RawURLEncoding.EncodeToString(nonce))
			httpRequest.Header.Set("X-Nya-Timestamp",
				fmt.Sprintf("%d", time.Now().UTC().Unix()))
		}
	}
	httpResponse, err := c.http.Do(httpRequest)
	if err != nil {
		return err
	}
	defer httpResponse.Body.Close()
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(httpResponse.Body, 4096))
		return fmt.Errorf("sync server returned %d: %s", httpResponse.StatusCode, strings.TrimSpace(string(data)))
	}
	if response == nil || httpResponse.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(io.LimitReader(httpResponse.Body, 8<<20)).Decode(response)
}

func (c *Client) load(ctx context.Context) (Profile, State, error) {
	var profile Profile
	if err := c.vault.Get(ctx, store.TypeSyncProfile, profileID, &profile); err != nil {
		return Profile{}, State{}, errors.New("synchronization is not configured")
	}
	var state State
	if err := c.vault.Get(ctx, store.TypeSyncState, stateID, &state); err != nil {
		state = State{Records: map[string]RecordState{}}
	}
	if state.Records == nil {
		state.Records = map[string]RecordState{}
	}
	if state.Deferred == nil {
		state.Deferred = map[string]serverRecord{}
	}
	return profile, state, nil
}

func (c *Client) saveProfile(ctx context.Context, profile Profile) error {
	return c.vault.Put(ctx, store.TypeSyncProfile, profileID, profile)
}

func createRecoveryBundle(syncRootKey []byte, generation int64) (string, map[string]any, error) {
	if generation < 1 {
		return "", nil, errors.New("invalid recovery generation")
	}
	recoverySecret := make([]byte, 32)
	salt := make([]byte, 16)
	if _, err := rand.Read(recoverySecret); err != nil {
		return "", nil, err
	}
	if _, err := rand.Read(salt); err != nil {
		return "", nil, err
	}
	code := base64.RawURLEncoding.EncodeToString(recoverySecret)
	wrappingKey := argon2.IDKey([]byte(code), salt, 3, 64*1024, 2, 32)
	defer wipe(wrappingKey)
	nonce, ciphertext, err := seal(wrappingKey, syncRootKey, []byte("nyaterminal:recovery:v1"))
	if err != nil {
		return "", nil, err
	}
	verifierInput := append([]byte("nyaterminal:recovery-verifier:v1"), wrappingKey...)
	verifier := sha256.Sum256(verifierInput)
	wipe(verifierInput)
	return formatRecoveryCode(code), map[string]any{
		"generation": generation, "salt": salt, "nonce": nonce,
		"ciphertext": ciphertext, "verifier": verifier[:],
	}, nil
}

func recoveryMaterial(code string, salt []byte) ([]byte, []byte, error) {
	canonical := normalizeRecoveryCode(code)
	raw, err := base64.RawURLEncoding.DecodeString(canonical)
	if err != nil || len(raw) != 32 || len(salt) < 16 || len(salt) > 64 {
		wipe(raw)
		return nil, nil, errors.New("recovery code is invalid")
	}
	canonical = base64.RawURLEncoding.EncodeToString(raw)
	wipe(raw)
	wrappingKey := argon2.IDKey([]byte(canonical), salt, 3, 64*1024, 2, 32)
	verifierInput := append([]byte("nyaterminal:recovery-verifier:v1"), wrappingKey...)
	verifier := sha256.Sum256(verifierInput)
	wipe(verifierInput)
	return wrappingKey, verifier[:], nil
}

func normalizeRecoveryCode(value string) string {
	compact := strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "").
		Replace(strings.TrimSpace(value))
	// Versions before the space-separated format inserted a dash after each
	// four Base64URL characters. Remove only those separator positions so a
	// real '-' from the Base64URL alphabet is preserved.
	if len(compact) == 53 {
		var legacy strings.Builder
		for index, char := range compact {
			if (index+1)%5 == 0 && char == '-' {
				continue
			}
			legacy.WriteRune(char)
		}
		if legacy.Len() == 43 {
			return legacy.String()
		}
	}
	return compact
}

func validateServerURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(value), "/"))
	if err != nil || parsed.Host == "" {
		return "", errors.New("invalid synchronization server URL")
	}
	if parsed.Scheme != "https" &&
		!(parsed.Scheme == "http" && (parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost")) {
		return "", errors.New("synchronization requires HTTPS outside localhost")
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func boolPtr(value bool) *bool {
	return &value
}

func syncAAD(recordType, id string, version int64) []byte {
	return []byte(fmt.Sprintf("nyaterminal:sync:v1:%s:%s:%d", recordType, id, version))
}

func syncRecordAAD(
	recordType, id string,
	version int64,
	vector map[string]int64,
) []byte {
	if len(vector) == 0 {
		return syncAAD(recordType, id, version)
	}
	encoded, _ := json.Marshal(vector)
	return []byte(fmt.Sprintf(
		"nyaterminal:sync:v2:%s:%s:%d:%s",
		recordType, id, version, encoded,
	))
}

func derivePairingKey(shared []byte, pairingID string) ([]byte, error) {
	reader := hkdf.New(
		sha256.New, shared, []byte(pairingID), []byte("nyaterminal:pairing-key:v1"),
	)
	key := make([]byte, chacha20poly1305.KeySize)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, err
	}
	return key, nil
}

func pairingApprovalMessage(
	requestID, targetDeviceID, approverDeviceID string,
	approverExchangeKey, nonce, ciphertext []byte,
) []byte {
	value := []byte(
		"nyaterminal:pairing-approval:v1:" +
			requestID + ":" + targetDeviceID + ":" + approverDeviceID,
	)
	value = append(value, approverExchangeKey...)
	value = append(value, nonce...)
	value = append(value, ciphertext...)
	sum := sha256.Sum256(value)
	return sum[:]
}

func seal(key, plaintext, aad []byte) ([]byte, []byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return nonce, aead.Seal(nil, nonce, plaintext, aad), nil
}

func open(key, nonce, ciphertext, aad []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != aead.NonceSize() {
		return nil, errors.New("invalid nonce")
	}
	return aead.Open(nil, nonce, ciphertext, aad)
}

func makeCredentialConflict(data []byte, id string) ([]byte, error) {
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	value["id"] = id
	name, _ := value["name"].(string)
	value["name"] = name + "（同步冲突）"
	return json.Marshal(value)
}

func formatRecoveryCode(value string) string {
	var builder strings.Builder
	for index, char := range value {
		if index > 0 && index%4 == 0 {
			builder.WriteByte(' ')
		}
		builder.WriteRune(char)
	}
	return builder.String()
}

func wipe(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
