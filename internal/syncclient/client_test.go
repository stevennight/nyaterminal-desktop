package syncclient

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func TestSyncCiphertextAuthenticatesMetadata(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
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

func TestRecoveryBundleDoesNotContainRootKey(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	code, bundle, err := createRecoveryBundle(key)
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
	_, _ = rand.Read(rootKey)
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
