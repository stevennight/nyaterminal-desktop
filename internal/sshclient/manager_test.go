package sshclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/nyaterminal/nyaterminal/desktop/internal/model"
	"github.com/nyaterminal/nyaterminal/desktop/internal/store"
	"github.com/nyaterminal/nyaterminal/desktop/internal/vault"
	"golang.org/x/crypto/ssh"
)

func TestPasswordSSHRequiresHostTrustAndDetectsChangedKey(t *testing.T) {
	address, signer, closeServer := startTestSSHServer(t, "secret")
	defer closeServer()
	host, rawPort, _ := net.SplitHostPort(address)
	port, _ := strconv.Atoi(rawPort)

	v, err := vault.Open(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	closeVaultOnCleanup(t, v)
	ctx := context.Background()
	if err := v.Initialize(ctx, "master password with enough entropy"); err != nil {
		t.Fatal(err)
	}
	dataStore := store.New(v)
	credential, err := dataStore.PutCredential(ctx, model.Credential{
		Name: "password", Type: "password", Password: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := dataStore.PutConnection(ctx, model.Connection{
		Name: "test", Host: host, Port: port, Username: "user",
		CredentialID: credential.ID, Authentication: "password",
		Encoding: "utf-8", ConnectTimeoutSec: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(dataStore)
	if err != nil {
		t.Fatal(err)
	}
	closeManagerOnCleanup(t, manager)

	_, err = manager.Start(ctx, StartRequest{ConnectionID: connection.ID, Columns: 80, Rows: 24})
	var hostKeyError HostKeyError
	if !errors.As(err, &hostKeyError) || hostKeyError.Pending.Changed {
		t.Fatalf("first connection did not require host trust: %v", err)
	}
	if err := manager.AcceptHostKey(ctx, hostKeyError.Pending.ID); err != nil {
		t.Fatal(err)
	}
	result, err := manager.Start(ctx, StartRequest{ConnectionID: connection.ID, Columns: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	manager.CloseSession(result.SessionID)

	_, otherPrivate, _ := ed25519.GenerateKey(rand.Reader)
	otherSigner, _ := ssh.NewSignerFromKey(otherPrivate)
	if err := dataStore.PutHostTrust(ctx, store.HostTrust{
		HostPort:    net.JoinHostPort(host, rawPort),
		Algorithm:   otherSigner.PublicKey().Type(),
		Fingerprint: ssh.FingerprintSHA256(otherSigner.PublicKey()),
		PublicKey:   otherSigner.PublicKey().Marshal(),
	}); err != nil {
		t.Fatal(err)
	}
	_, err = manager.Start(ctx, StartRequest{ConnectionID: connection.ID, Columns: 80, Rows: 24})
	if !errors.As(err, &hostKeyError) || !hostKeyError.Pending.Changed {
		t.Fatalf("changed host key was not blocked: %v", err)
	}
	if ssh.FingerprintSHA256(signer.PublicKey()) != hostKeyError.Pending.Fingerprint {
		t.Fatal("changed-key warning did not report the server key")
	}
}

func TestPortForwardingIsReserved(t *testing.T) {
	manager := &Manager{}
	_, err := manager.StartPortForward(context.Background(), PortForwardRequest{
		ConnectionID: "connection-id", Mode: "local",
		ListenHost: "127.0.0.1", ListenPort: 0,
		TargetHost: "127.0.0.1", TargetPort: 22,
	})
	if !errors.Is(err, ErrPortForwardingReserved) {
		t.Fatalf("expected reserved port forwarding error, got %v", err)
	}
	if err := manager.StopPortForward("forward-id"); !errors.Is(err, ErrPortForwardingReserved) {
		t.Fatalf("expected reserved port forwarding stop error, got %v", err)
	}
}

func TestPasswordConnectionWithoutStoredCredentialRequestsAuthPrompt(t *testing.T) {
	address, _, closeServer := startTestSSHServer(t, "secret")
	defer closeServer()
	host, rawPort, _ := net.SplitHostPort(address)
	port, _ := strconv.Atoi(rawPort)

	v, err := vault.Open(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	closeVaultOnCleanup(t, v)
	ctx := context.Background()
	if err := v.Initialize(ctx, "master password with enough entropy"); err != nil {
		t.Fatal(err)
	}
	dataStore := store.New(v)
	connection, err := dataStore.PutConnection(ctx, model.Connection{
		Name: "test", Host: host, Port: port, Username: "user",
		CredentialID: "missing-credential", Authentication: "password",
		Encoding: "utf-8", ConnectTimeoutSec: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(dataStore)
	if err != nil {
		t.Fatal(err)
	}
	closeManagerOnCleanup(t, manager)

	_, err = manager.Start(ctx, StartRequest{ConnectionID: connection.ID, Columns: 80, Rows: 24})
	var prompt AuthPromptError
	if !errors.As(err, &prompt) {
		t.Fatalf("expected auth prompt error, got %v", err)
	}
	if prompt.Kind != "password" || prompt.Reason != "missing" {
		t.Fatalf("unexpected prompt: %#v", prompt)
	}
}

func startTestSSHServer(t *testing.T, password string) (string, ssh.Signer, func()) {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{
		PasswordCallback: func(metadata ssh.ConnMetadata, provided []byte) (*ssh.Permissions, error) {
			if metadata.User() == "user" && string(provided) == password {
				return nil, nil
			}
			return nil, errors.New("invalid credentials")
		},
	}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			raw, err := listener.Accept()
			if err != nil {
				return
			}
			go serveTestSSHConnection(raw, config)
		}
	}()
	closeFn := func() {
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}
	return listener.Addr().String(), signer, closeFn
}

func serveTestSSHConnection(raw net.Conn, config *ssh.ServerConfig) {
	connection, channels, requests, err := ssh.NewServerConn(raw, config)
	if err != nil {
		_ = raw.Close()
		return
	}
	defer func() {
		_ = connection.Close()
	}()
	go ssh.DiscardRequests(requests)
	for incoming := range channels {
		if incoming.ChannelType() != "session" {
			_ = incoming.Reject(ssh.UnknownChannelType, "unsupported")
			continue
		}
		channel, requests, err := incoming.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer func() {
				_ = channel.Close()
			}()
			for request := range requests {
				switch request.Type {
				case "pty-req", "shell":
					_ = request.Reply(true, nil)
				default:
					_ = request.Reply(false, nil)
				}
			}
		}()
	}
}

func closeVaultOnCleanup(t *testing.T, v *vault.Vault) {
	t.Helper()
	t.Cleanup(func() {
		if err := v.Close(); err != nil {
			t.Errorf("close vault: %v", err)
		}
	})
}

func closeManagerOnCleanup(t *testing.T, manager *Manager) {
	t.Helper()
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Errorf("close SSH manager: %v", err)
		}
	})
}
