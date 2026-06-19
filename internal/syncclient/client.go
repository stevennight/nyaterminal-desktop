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
	Cursor  int64                  `json:"cursor"`
	Records map[string]RecordState `json:"records"`
}

type RecordState struct {
	Version int64  `json:"version"`
	Hash    []byte `json:"hash"`
}

type SetupResult struct {
	DeviceID     string `json:"deviceId"`
	RecoveryCode string `json:"recoveryCode"`
}

type SyncResult struct {
	Pushed    int   `json:"pushed"`
	Pulled    int   `json:"pulled"`
	Conflicts int   `json:"conflicts"`
	Cursor    int64 `json:"cursor"`
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
	ID          string    `json:"id"`
	EntityType  string    `json:"entityType"`
	EntityID    string    `json:"entityId"`
	DeviceID    string    `json:"deviceId,omitempty"`
	Version     int64     `json:"version"`
	LogicalTime int64     `json:"logicalTime,omitempty"`
	Tombstone   bool      `json:"tombstone"`
	Nonce       []byte    `json:"nonce"`
	Ciphertext  []byte    `json:"ciphertext"`
	ContentHash []byte    `json:"contentHash"`
	UpdatedAt   time.Time `json:"updatedAt,omitempty"`
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
		DeviceName: deviceName, ExchangePrivateKey: exchangePrivate.Bytes(),
		ExchangePublicKey: exchangePrivate.PublicKey().Bytes(),
		SigningPrivateKey: signingPrivate, SigningPublicKey: signingPublic,
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
	recoveryCode, bundle, err := createRecoveryBundle(syncRootKey)
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

func (c *Client) Sync(ctx context.Context, syncSecrets, syncHistory bool) (SyncResult, error) {
	profile, state, err := c.load(ctx)
	if err != nil {
		return SyncResult{}, err
	}
	allowed := map[string]bool{
		store.TypeGroup: true, store.TypeConnection: true, store.TypeTag: true,
		store.TypeSettings: true,
	}
	if syncSecrets {
		allowed[store.TypeCredential] = true
	}
	if syncHistory {
		allowed[store.TypeHistory] = true
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
	for _, record := range records {
		plainHash := sha256.Sum256(record.Data)
		previous := state.Records[record.ID]
		if bytes.Equal(previous.Hash, plainHash[:]) {
			continue
		}
		version := previous.Version + 1
		nonce, ciphertext, err := seal(profile.SyncRootKey, record.Data, syncAAD(record.Type, record.ID, version))
		if err != nil {
			return SyncResult{}, err
		}
		contentHash := sha256.Sum256(ciphertext)
		outgoing = append(outgoing, serverRecord{
			ID: uuid.NewString(), EntityType: record.Type, EntityID: record.ID,
			Version: version, Nonce: nonce, Ciphertext: ciphertext, ContentHash: contentHash[:],
		})
		state.Records[record.ID] = RecordState{Version: version, Hash: plainHash[:]}
	}
	if len(outgoing) > 0 {
		if err := c.authorizedRequest(ctx, &profile, http.MethodPost, "/api/v1/sync/push",
			map[string]any{"records": outgoing}, nil); err != nil {
			return SyncResult{}, err
		}
	}
	pulled, next, conflicts, err := c.pull(ctx, &profile, &state, allowed)
	if err != nil {
		return SyncResult{}, err
	}
	state.Cursor = next
	if err := c.vault.Put(ctx, store.TypeSyncState, stateID, state); err != nil {
		return SyncResult{}, err
	}
	if err := c.saveProfile(ctx, profile); err != nil {
		return SyncResult{}, err
	}
	return SyncResult{
		Pushed: len(outgoing), Pulled: pulled, Conflicts: conflicts, Cursor: next,
	}, nil
}

func (c *Client) pull(ctx context.Context, profile *Profile, state *State, allowed map[string]bool) (int, int64, int, error) {
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
	localHash := make(map[string][]byte, len(local))
	for _, record := range local {
		sum := sha256.Sum256(record.Data)
		localHash[record.ID] = sum[:]
		wipe(record.Data)
	}
	applied, conflicts := 0, 0
	for _, record := range response.Records {
		if !allowed[record.EntityType] {
			continue
		}
		previous := state.Records[record.EntityID]
		if record.Version <= previous.Version {
			continue
		}
		plain, err := open(profile.SyncRootKey, record.Nonce, record.Ciphertext,
			syncAAD(record.EntityType, record.EntityID, record.Version))
		if err != nil {
			return applied, state.Cursor, conflicts, errors.New("synchronization ciphertext authentication failed")
		}
		sum := sha256.Sum256(plain)
		locallyChanged := previous.Version > 0 && !bytes.Equal(localHash[record.EntityID], previous.Hash)
		targetID := record.EntityID
		if locallyChanged && record.EntityType == store.TypeCredential {
			targetID = uuid.NewString()
			plain, err = makeCredentialConflict(plain, targetID)
			if err != nil {
				wipe(plain)
				return applied, state.Cursor, conflicts, err
			}
			conflicts++
		}
		if !record.Tombstone {
			if err := c.vault.PutJSON(ctx, record.EntityType, targetID, plain); err != nil {
				wipe(plain)
				return applied, state.Cursor, conflicts, err
			}
		} else {
			if err := c.vault.Delete(ctx, targetID); err != nil {
				wipe(plain)
				return applied, state.Cursor, conflicts, err
			}
		}
		wipe(plain)
		state.Records[targetID] = RecordState{Version: record.Version, Hash: sum[:]}
		applied++
	}
	return applied, response.Next, conflicts, nil
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
	return profile, state, nil
}

func (c *Client) saveProfile(ctx context.Context, profile Profile) error {
	return c.vault.Put(ctx, store.TypeSyncProfile, profileID, profile)
}

func createRecoveryBundle(syncRootKey []byte) (string, map[string]any, error) {
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
		"generation": 1, "salt": salt, "nonce": nonce,
		"ciphertext": ciphertext, "verifier": verifier[:],
	}, nil
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

func syncAAD(recordType, id string, version int64) []byte {
	return []byte(fmt.Sprintf("nyaterminal:sync:v1:%s:%s:%d", recordType, id, version))
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
			builder.WriteByte('-')
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
