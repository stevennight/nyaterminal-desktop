package syncclient

import (
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nyaterminal/nyaterminal/desktop/internal/store"
	"github.com/nyaterminal/nyaterminal/desktop/internal/vault"
)

func fillRandom(t *testing.T, data []byte) {
	t.Helper()
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
}

func closeServer(t *testing.T, server *httptest.Server) {
	t.Helper()
	t.Cleanup(server.Close)
}

func closeVault(t *testing.T, v *vault.Vault) {
	t.Helper()
	t.Cleanup(func() {
		if err := v.Close(); err != nil {
			t.Errorf("close vault: %v", err)
		}
	})
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode response: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func TestSyncCiphertextAuthenticatesMetadata(t *testing.T) {
	key := make([]byte, 32)
	fillRandom(t, key)
	plain := []byte(`{"id":"connection-1","host":"example.test"}`)
	aad := syncAAD("connection", "connection-1", 1)
	nonce, ciphertext, err := seal(key, plain, aad)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := open(key, nonce, ciphertext, aad)
	if err != nil || string(restored) != string(plain) {
		t.Fatalf("round trip failed: %v", err)
	}
	if _, err := open(key, nonce, ciphertext, syncAAD("credential", "connection-1", 1)); err == nil {
		t.Fatal("ciphertext accepted with altered entity metadata")
	}
}

func TestVersionVectorsDetectConcurrency(t *testing.T) {
	left := map[string]int64{"device-a": 2, "device-b": 1}
	right := map[string]int64{"device-a": 1, "device-b": 2}
	if compareVectors(left, right) != vectorConcurrent {
		t.Fatal("concurrent vectors were not detected")
	}
	merged := mergeVectors(left, right)
	if merged["device-a"] != 2 || merged["device-b"] != 2 {
		t.Fatalf("unexpected merged vector: %#v", merged)
	}
	if compareVectors(merged, left) != vectorAfter {
		t.Fatal("merged vector must dominate its input")
	}
}

func TestConcurrentJSONFieldsMergeWithoutOverwritingIndependentChanges(t *testing.T) {
	baseVector := map[string]int64{"device-a": 1, "device-b": 1}
	localData := []byte(`{"id":"one","name":"local","host":"old.example"}`)
	remoteData := []byte(`{"id":"one","name":"original","host":"remote.example"}`)
	_, localFields, err := makeSyncEnvelope(
		localData, nil,
		map[string]int64{"device-a": 2, "device-b": 1}, "device-a",
	)
	if err != nil {
		t.Fatal(err)
	}
	_, remoteFields, err := makeSyncEnvelope(
		remoteData, nil,
		map[string]int64{"device-a": 1, "device-b": 2}, "device-b",
	)
	if err != nil {
		t.Fatal(err)
	}
	// id was unchanged on both devices, so give it the common base clock.
	idHash := sha256.Sum256([]byte(`"one"`))
	localFields["id"] = FieldState{Vector: baseVector, Writer: "device-a", Hash: idHash[:]}
	remoteFields["id"] = FieldState{Vector: baseVector, Writer: "device-a", Hash: idHash[:]}
	// name changed only locally; host changed only remotely.
	originalNameHash := sha256.Sum256([]byte(`"original"`))
	remoteFields["name"] = FieldState{Vector: baseVector, Writer: "device-a", Hash: originalNameHash[:]}
	oldHostHash := sha256.Sum256([]byte(`"old.example"`))
	localFields["host"] = FieldState{Vector: baseVector, Writer: "device-a", Hash: oldHostHash[:]}

	merged, _, err := mergeJSONFields(localData, remoteData, localFields, remoteFields)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]string
	if err := json.Unmarshal(merged, &value); err != nil {
		t.Fatal(err)
	}
	if value["name"] != "local" || value["host"] != "remote.example" {
		t.Fatalf("independent changes were not merged: %s", merged)
	}
}

func TestVectorIsAuthenticatedBySyncAAD(t *testing.T) {
	key := make([]byte, 32)
	fillRandom(t, key)
	vector := map[string]int64{"device-a": 2, "device-b": 1}
	aad := syncRecordAAD("connection", "one", 2, vector)
	nonce, ciphertext, err := seal(key, []byte(`{"data":"secret"}`), aad)
	if err != nil {
		t.Fatal(err)
	}
	tampered := map[string]int64{"device-a": 2, "device-b": 2}
	if _, err := open(
		key, nonce, ciphertext,
		syncRecordAAD("connection", "one", 2, tampered),
	); err == nil {
		t.Fatal("ciphertext accepted a tampered version vector")
	}
}

func TestCredentialSyncPolicyUsesCredentialThenConnectionThenGlobal(t *testing.T) {
	if !credentialPayloadAllowed(
		"credential", []byte(`{"syncOverride":true}`), false,
		map[string]bool{"credential": false},
	) {
		t.Fatal("credential override did not take precedence")
	}
	if credentialPayloadAllowed(
		"credential", []byte(`{"name":"key"}`), true,
		map[string]bool{"credential": false},
	) {
		t.Fatal("connection override was ignored")
	}
	if !credentialPayloadAllowed(
		"credential", []byte(`{"name":"key"}`), false,
		map[string]bool{"credential": true},
	) {
		t.Fatal("enabled connection override was ignored")
	}
	if !credentialPayloadAllowed("other", []byte(`{"name":"key"}`), true, nil) {
		t.Fatal("global default was ignored")
	}
}

func TestAuthorizedRequestPersistsRefreshedTokens(t *testing.T) {
	var sawNewAccess bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			writeJSON(t, w, TokenPair{
				AccessToken: "new-access", RefreshToken: "new-refresh",
				AccessExpiresAt:  time.Now().UTC().Add(time.Hour),
				RefreshExpiresAt: time.Now().UTC().Add(24 * time.Hour),
			})
		case "/ok":
			if r.Header.Get("Authorization") != "Bearer new-access" {
				http.Error(w, "wrong token", http.StatusUnauthorized)
				return
			}
			sawNewAccess = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	closeServer(t, server)

	ctx := context.Background()
	v, err := vault.Open(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	closeVault(t, v)
	if err := v.Initialize(ctx, "master password with enough entropy"); err != nil {
		t.Fatal(err)
	}
	client := New(v)
	profile := Profile{
		ServerURL: server.URL, DeviceID: "device-a", DeviceName: "test",
		SyncRootKey: make([]byte, 32),
	}
	session := AccountSession{
		ServerURL: server.URL, DeviceID: "device-a", DeviceName: "test",
		AccessToken: "old-access", RefreshToken: "old-refresh",
		AccessExpiresAt:  time.Now().UTC().Add(-time.Minute),
		RefreshExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	if err := client.saveProfile(ctx, profile); err != nil {
		t.Fatal(err)
	}
	if err := client.saveAccountSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	if err := v.Put(ctx, store.TypeSyncState, stateID, State{Records: map[string]RecordState{}}); err != nil {
		t.Fatal(err)
	}
	if err := client.authorizedRequest(ctx, &session, http.MethodGet, "/ok", nil, nil); err != nil {
		t.Fatal(err)
	}
	if !sawNewAccess {
		t.Fatal("authorized request did not use refreshed access token")
	}
	loaded, err := client.loadAccountSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AccessToken != "new-access" || loaded.RefreshToken != "new-refresh" {
		t.Fatalf("refreshed tokens were not persisted: %#v", loaded)
	}
}

func TestLoginGeneratesDeviceIDWhenMissing(t *testing.T) {
	var seenDeviceID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/login" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			DeviceID string `json:"deviceId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		seenDeviceID = body.DeviceID
		writeJSON(t, w, TokenPair{
			AccessToken: "a", RefreshToken: "b",
			AccessExpiresAt:  time.Now().UTC().Add(time.Hour),
			RefreshExpiresAt: time.Now().UTC().Add(24 * time.Hour),
		})
	}))
	closeServer(t, server)

	ctx := context.Background()
	v, err := vault.Open(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	closeVault(t, v)
	if err := v.Initialize(ctx, "master password with enough entropy"); err != nil {
		t.Fatal(err)
	}
	client := New(v)
	if err := client.Login(ctx, server.URL, "owner", "correct horse battery staple", "", ""); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(seenDeviceID) == "" {
		t.Fatal("login did not generate a device id")
	}
	session, err := client.loadAccountSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if session.DeviceID != seenDeviceID {
		t.Fatalf("device id was not persisted: %#v vs %q", session, seenDeviceID)
	}
}

func TestLoginSendsSecondFactor(t *testing.T) {
	var seenSecondFactor string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/login" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			TOTPCode string `json:"totpCode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		seenSecondFactor = body.TOTPCode
		writeJSON(t, w, TokenPair{
			AccessToken: "a", RefreshToken: "b",
			AccessExpiresAt:  time.Now().UTC().Add(time.Hour),
			RefreshExpiresAt: time.Now().UTC().Add(24 * time.Hour),
		})
	}))
	closeServer(t, server)

	ctx := context.Background()
	v, err := vault.Open(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	closeVault(t, v)
	if err := v.Initialize(ctx, "master password with enough entropy"); err != nil {
		t.Fatal(err)
	}
	client := New(v)
	if err := client.Login(
		ctx, server.URL, "owner", "correct horse battery staple", "device-a", "145651",
	); err != nil {
		t.Fatal(err)
	}
	if seenSecondFactor != "145651" {
		t.Fatalf("second factor was not sent: %q", seenSecondFactor)
	}
}

func TestRequestAlwaysSetsOriginWhenEndpointHasHost(t *testing.T) {
	var seenOrigin string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenOrigin = r.Header.Get("Origin")
		w.WriteHeader(http.StatusNoContent)
	}))
	closeServer(t, server)

	ctx := context.Background()
	v, err := vault.Open(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	closeVault(t, v)
	if err := v.Initialize(ctx, "master password with enough entropy"); err != nil {
		t.Fatal(err)
	}
	client := New(v)
	if err := client.request(ctx, http.MethodPost, server.URL+"/origin", "", map[string]any{"ok": true}, nil); err != nil {
		t.Fatal(err)
	}
	if seenOrigin != server.URL {
		t.Fatalf("expected origin %q, got %q", server.URL, seenOrigin)
	}
}

func TestRecoveryBundleDoesNotContainRootKey(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	code, bundle, err := createRecoveryBundle(key, 1)
	if err != nil {
		t.Fatal(err)
	}
	if code == "" || bundle["ciphertext"] == nil {
		t.Fatal("recovery bundle was not created")
	}
	if base64.RawURLEncoding.EncodeToString(key) == code {
		t.Fatal("recovery code exposed the synchronization root key")
	}
}

func TestRecoveryCodeCanUnwrapRootKey(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	code, rawBundle, err := createRecoveryBundle(key, 7)
	if err != nil {
		t.Fatal(err)
	}
	salt := rawBundle["salt"].([]byte)
	wrappingKey, verifier, err := recoveryMaterial(code, salt)
	if err != nil {
		t.Fatal(err)
	}
	defer wipe(wrappingKey)
	if string(verifier) != string(rawBundle["verifier"].([]byte)) {
		t.Fatal("recovery verifier mismatch")
	}
	restored, err := open(
		wrappingKey,
		rawBundle["nonce"].([]byte),
		rawBundle["ciphertext"].([]byte),
		[]byte("nyaterminal:recovery:v1"),
	)
	if err != nil || string(restored) != string(key) {
		t.Fatalf("recovery failed: %v", err)
	}
}

func TestPairingPackageRoundTrip(t *testing.T) {
	oldPrivate, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	newPrivate, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signingPublic, signingPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	oldShared, err := oldPrivate.ECDH(newPrivate.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	newShared, err := newPrivate.ECDH(oldPrivate.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	oldKey, err := derivePairingKey(oldShared, "pairing-id")
	if err != nil {
		t.Fatal(err)
	}
	newKey, err := derivePairingKey(newShared, "pairing-id")
	if err != nil {
		t.Fatal(err)
	}
	rootKey := make([]byte, 32)
	fillRandom(t, rootKey)
	nonce, ciphertext, err := seal(
		oldKey, rootKey, []byte("nyaterminal:pairing-package:v1:pairing-id"),
	)
	if err != nil {
		t.Fatal(err)
	}
	message := pairingApprovalMessage(
		"pairing-id", "new-device", "old-device",
		oldPrivate.PublicKey().Bytes(), nonce, ciphertext,
	)
	signature := ed25519.Sign(signingPrivate, message)
	if !ed25519.Verify(signingPublic, message, signature) {
		t.Fatal("pairing signature did not verify")
	}
	restored, err := open(
		newKey, nonce, ciphertext, []byte("nyaterminal:pairing-package:v1:pairing-id"),
	)
	if err != nil || string(restored) != string(rootKey) {
		t.Fatalf("pairing package round trip failed: %v", err)
	}
}
