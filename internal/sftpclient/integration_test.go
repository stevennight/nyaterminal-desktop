package sftpclient

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/nyaterminal/nyaterminal-desktop/internal/model"
	"github.com/nyaterminal/nyaterminal-desktop/internal/sshclient"
	"github.com/nyaterminal/nyaterminal-desktop/internal/store"
	"github.com/nyaterminal/nyaterminal-desktop/internal/vault"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func TestSFTPTransferQueueUploadsAndResumesDownload(t *testing.T) {
	remoteRoot := t.TempDir()
	address, signer, closeServer := startSFTPServer(t, remoteRoot)
	defer closeServer()
	host, rawPort, _ := net.SplitHostPort(address)
	port, _ := strconv.Atoi(rawPort)

	v, err := vault.Open(filepath.Join(t.TempDir(), "vault.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer v.Close()
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
		Name: "sftp", Host: host, Port: port, Username: "user",
		CredentialID: credential.ID, Authentication: "password",
		Encoding: "utf-8", ConnectTimeoutSec: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := dataStore.PutHostTrust(ctx, store.HostTrust{
		HostPort:    net.JoinHostPort(host, rawPort),
		Algorithm:   signer.PublicKey().Type(),
		Fingerprint: ssh.FingerprintSHA256(signer.PublicKey()),
		PublicKey:   signer.PublicKey().Marshal(),
	}); err != nil {
		t.Fatal(err)
	}
	manager, err := sshclient.NewManager(dataStore)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	service := New(manager)

	workingDirectory, err := service.RemoteWorkingDirectory(ctx, connection.ID)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(workingDirectory) != filepath.Clean(remoteRoot) {
		t.Fatalf("unexpected remote working directory: %q", workingDirectory)
	}

	payload := bytes.Repeat([]byte("NyaTerminal-SFTP-"), 64*1024)
	localRoot := t.TempDir()
	source := filepath.Join(localRoot, "source.bin")
	if err := os.WriteFile(source, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	upload, err := service.StartUpload(connection.ID, source, "uploaded.bin", false)
	if err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, service, upload.ID)
	remoteData, err := os.ReadFile(filepath.Join(remoteRoot, "uploaded.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(remoteData, payload) {
		t.Fatal("uploaded data differs")
	}

	destination := filepath.Join(localRoot, "downloaded.bin")
	partial := destination + ".nyapart"
	if err := os.WriteFile(partial, payload[:len(payload)/3], 0o600); err != nil {
		t.Fatal(err)
	}
	download, err := service.StartDownload(
		connection.ID, "uploaded.bin", destination, false,
	)
	if err != nil {
		t.Fatal(err)
	}
	waitTransfer(t, service, download.ID)
	downloaded, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(downloaded, payload) {
		t.Fatal("resumed download differs")
	}
}

func waitTransfer(t *testing.T, service *Service, id string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, transfer := range service.ListTransfers() {
			if transfer.ID != id {
				continue
			}
			switch transfer.Status {
			case "completed":
				return
			case "failed", "cancelled":
				t.Fatalf("transfer ended as %s: %s", transfer.Status, transfer.Error)
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("transfer timed out")
}

func startSFTPServer(t *testing.T, root string) (string, ssh.Signer, func()) {
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
		PasswordCallback: func(metadata ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if metadata.User() == "user" && string(password) == "secret" {
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
			go serveSFTPConnection(raw, config, root)
		}
	}()
	return listener.Addr().String(), signer, func() {
		_ = listener.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}
}

func serveSFTPConnection(raw net.Conn, config *ssh.ServerConfig, root string) {
	connection, channels, requests, err := ssh.NewServerConn(raw, config)
	if err != nil {
		_ = raw.Close()
		return
	}
	defer connection.Close()
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
			defer channel.Close()
			for request := range requests {
				if request.Type != "subsystem" || !bytes.Contains(request.Payload, []byte("sftp")) {
					_ = request.Reply(false, nil)
					continue
				}
				_ = request.Reply(true, nil)
				server, err := sftp.NewServer(channel, sftp.WithServerWorkingDirectory(root))
				if err == nil {
					_ = server.Serve()
					_ = server.Close()
				}
				return
			}
		}()
	}
}
