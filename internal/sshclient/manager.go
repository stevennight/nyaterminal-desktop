package sshclient

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/nyaterminal/nyaterminal/desktop/internal/model"
	"github.com/nyaterminal/nyaterminal/desktop/internal/store"
	sshagent "github.com/xanzy/ssh-agent"
	"golang.org/x/crypto/ssh"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

var ErrPortForwardingReserved = errors.New("SSH port forwarding is reserved for a future release")

type Manager struct {
	mu                 sync.RWMutex
	store              *store.Store
	listener           net.Listener
	server             *http.Server
	address            string
	sessions           map[string]*terminalSession
	pending            map[string]PendingHostKey
	interactiveHandler func(context.Context, InteractiveChallenge) ([]string, error)
}

type terminalSession struct {
	id           string
	connectionID string
	token        string
	attached     bool
	client       *ssh.Client
	session      *ssh.Session
	stdin        io.WriteCloser
	encoding     encoding.Encoding
	output       chan []byte
	done         chan struct{}
	closeOnce    sync.Once
}

type StartRequest struct {
	ConnectionID         string   `json:"connectionId"`
	Columns              int      `json:"columns"`
	Rows                 int      `json:"rows"`
	InteractionResponses []string `json:"interactionResponses"`
}

type StartResult struct {
	SessionID string `json:"sessionId"`
	URL       string `json:"url"`
}

type PortForwardRequest struct {
	ConnectionID string `json:"connectionId"`
	Mode         string `json:"mode"`
	ListenHost   string `json:"listenHost"`
	ListenPort   int    `json:"listenPort"`
	TargetHost   string `json:"targetHost"`
	TargetPort   int    `json:"targetPort"`
}

type PortForwardHandle struct {
	ID          string `json:"id"`
	ListenAddr  string `json:"listenAddr"`
	Description string `json:"description"`
}

type PendingHostKey struct {
	ID          string `json:"id"`
	HostPort    string `json:"hostPort"`
	Algorithm   string `json:"algorithm"`
	Fingerprint string `json:"fingerprint"`
	PublicKey   []byte `json:"publicKey"`
	Changed     bool   `json:"changed"`
}

type InteractiveChallenge struct {
	ID          string   `json:"id"`
	User        string   `json:"user"`
	Instruction string   `json:"instruction"`
	Questions   []string `json:"questions"`
	Echoes      []bool   `json:"echoes"`
}

type HostKeyError struct {
	Pending PendingHostKey
}

func (e HostKeyError) Error() string {
	if e.Pending.Changed {
		return "SSH host key has changed"
	}
	return "SSH host key is not trusted"
}

func NewManager(dataStore *store.Store) (*Manager, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	manager := &Manager{
		store: dataStore, listener: listener, address: listener.Addr().String(),
		sessions: make(map[string]*terminalSession),
		pending:  make(map[string]PendingHostKey),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/session/", manager.handleWebSocket)
	manager.server = &http.Server{
		Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second,
	}
	go func() { _ = manager.server.Serve(listener) }()
	return manager, nil
}

func (m *Manager) SetInteractiveHandler(handler func(context.Context, InteractiveChallenge) ([]string, error)) {
	m.mu.Lock()
	m.interactiveHandler = handler
	m.mu.Unlock()
}

func (m *Manager) Close() error {
	m.mu.Lock()
	for _, session := range m.sessions {
		session.close()
	}
	m.sessions = make(map[string]*terminalSession)
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return m.server.Shutdown(ctx)
}

func (m *Manager) Start(ctx context.Context, request StartRequest) (StartResult, error) {
	connection, err := m.store.GetConnection(ctx, request.ConnectionID)
	if err != nil {
		return StartResult{}, err
	}
	var credential model.Credential
	if connection.CredentialID != "" {
		credential, err = m.store.GetCredential(ctx, connection.CredentialID)
		if err != nil {
			return StartResult{}, err
		}
	}
	client, err := m.dial(ctx, connection, credential, request.InteractionResponses)
	if err != nil {
		return StartResult{}, err
	}
	sshSession, err := client.NewSession()
	if err != nil {
		client.Close()
		return StartResult{}, err
	}
	columns, rows := request.Columns, request.Rows
	if columns < 20 {
		columns = 120
	}
	if rows < 5 {
		rows = 32
	}
	if err := sshSession.RequestPty("xterm-256color", rows, columns, ssh.TerminalModes{
		ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400,
	}); err != nil {
		sshSession.Close()
		client.Close()
		return StartResult{}, err
	}
	stdin, err := sshSession.StdinPipe()
	if err != nil {
		sshSession.Close()
		client.Close()
		return StartResult{}, err
	}
	id := uuid.NewString()
	token, err := randomToken(32)
	if err != nil {
		sshSession.Close()
		client.Close()
		return StartResult{}, err
	}
	entry := &terminalSession{
		id: id, connectionID: connection.ID,
		token: token, client: client, session: sshSession, stdin: stdin,
		encoding: terminalEncoding(connection.Encoding),
		output:   make(chan []byte, 256), done: make(chan struct{}),
	}
	sshSession.Stdout = channelWriter{session: entry}
	sshSession.Stderr = channelWriter{session: entry}
	if err := sshSession.Shell(); err != nil {
		entry.close()
		return StartResult{}, err
	}
	m.mu.Lock()
	m.sessions[id] = entry
	m.mu.Unlock()
	go m.keepAlive(connection, entry)
	go func() {
		_ = sshSession.Wait()
		entry.close()
		m.mu.Lock()
		delete(m.sessions, id)
		m.mu.Unlock()
	}()
	return StartResult{
		SessionID: id,
		URL:       "ws://" + m.address + "/session/" + id + "?token=" + token,
	}, nil
}

func (m *Manager) BorrowConnection(connectionID string) *ssh.Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, session := range m.sessions {
		if session.connectionID == connectionID {
			select {
			case <-session.done:
			default:
				return session.client
			}
		}
	}
	return nil
}

func (m *Manager) DialConnection(ctx context.Context, connectionID string, responses []string) (*ssh.Client, error) {
	connection, err := m.store.GetConnection(ctx, connectionID)
	if err != nil {
		return nil, err
	}
	var credential model.Credential
	if connection.CredentialID != "" {
		credential, err = m.store.GetCredential(ctx, connection.CredentialID)
		if err != nil {
			return nil, err
		}
	}
	return m.dial(ctx, connection, credential, responses)
}

func (m *Manager) StartPortForward(
	context.Context,
	PortForwardRequest,
) (PortForwardHandle, error) {
	return PortForwardHandle{}, ErrPortForwardingReserved
}

func (m *Manager) StopPortForward(string) error {
	return ErrPortForwardingReserved
}

func (m *Manager) Resize(sessionID string, columns, rows int) error {
	m.mu.RLock()
	session := m.sessions[sessionID]
	m.mu.RUnlock()
	if session == nil || columns < 1 || rows < 1 {
		return errors.New("session not found")
	}
	return session.session.WindowChange(rows, columns)
}

func (m *Manager) CloseSession(sessionID string) {
	m.mu.Lock()
	session := m.sessions[sessionID]
	delete(m.sessions, sessionID)
	m.mu.Unlock()
	if session != nil {
		session.close()
	}
}

func (m *Manager) PendingHostKey(id string) (PendingHostKey, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	value, ok := m.pending[id]
	return value, ok
}

func (m *Manager) AcceptHostKey(ctx context.Context, id string) error {
	m.mu.Lock()
	pending, ok := m.pending[id]
	if ok {
		delete(m.pending, id)
	}
	m.mu.Unlock()
	if !ok {
		return errors.New("pending host key not found")
	}
	return m.store.PutHostTrust(ctx, store.HostTrust{
		HostPort: pending.HostPort, Algorithm: pending.Algorithm,
		Fingerprint: pending.Fingerprint, PublicKey: pending.PublicKey,
	})
}

func (m *Manager) dial(ctx context.Context, connection model.Connection, credential model.Credential, responses []string) (*ssh.Client, error) {
	m.mu.RLock()
	interactiveHandler := m.interactiveHandler
	m.mu.RUnlock()
	authMethods, agentConnection, err := authMethods(
		ctx, connection, credential, responses, interactiveHandler,
	)
	if err != nil {
		return nil, err
	}
	if agentConnection != nil {
		defer agentConnection.Close()
	}
	timeout := time.Duration(connection.ConnectTimeoutSec) * time.Second
	algorithms := ssh.SupportedAlgorithms()
	config := &ssh.ClientConfig{
		User: connection.Username, Auth: authMethods,
		HostKeyCallback: m.hostKeyCallback(ctx),
		HostKeyAlgorithms: append(
			[]string(nil), algorithms.HostKeys...,
		),
		Timeout: timeout,
	}
	config.Config.KeyExchanges = append([]string(nil), algorithms.KeyExchanges...)
	config.Config.Ciphers = append([]string(nil), algorithms.Ciphers...)
	config.Config.MACs = append([]string(nil), algorithms.MACs...)
	if connection.LegacyAlgorithms {
		insecureAlgorithms := ssh.InsecureAlgorithms()
		config.HostKeyAlgorithms = append(
			config.HostKeyAlgorithms,
			insecureAlgorithms.HostKeys...,
		)
		config.Config.KeyExchanges = append(
			config.Config.KeyExchanges,
			insecureAlgorithms.KeyExchanges...,
		)
		config.Config.Ciphers = append(
			config.Config.Ciphers,
			insecureAlgorithms.Ciphers...,
		)
		config.Config.MACs = append(
			config.Config.MACs,
			insecureAlgorithms.MACs...,
		)
	}
	address := net.JoinHostPort(connection.Host, strconv.Itoa(connection.Port))
	dialer := net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	netConn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	sshConn, channels, requests, err := ssh.NewClientConn(netConn, address, config)
	if err != nil {
		netConn.Close()
		return nil, err
	}
	return ssh.NewClient(sshConn, channels, requests), nil
}

func (m *Manager) hostKeyCallback(ctx context.Context) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		hostPort := hostname
		fingerprint := ssh.FingerprintSHA256(key)
		trust, err := m.store.GetHostTrust(ctx, hostPort)
		if err == nil {
			if trust.Algorithm == key.Type() && fingerprint == trust.Fingerprint &&
				string(trust.PublicKey) == string(key.Marshal()) {
				return nil
			}
			pending := m.addPending(hostPort, key, true)
			return HostKeyError{Pending: pending}
		}
		pending := m.addPending(hostPort, key, false)
		return HostKeyError{Pending: pending}
	}
}

func (m *Manager) addPending(hostPort string, key ssh.PublicKey, changed bool) PendingHostKey {
	pending := PendingHostKey{
		ID: uuid.NewString(), HostPort: hostPort, Algorithm: key.Type(),
		Fingerprint: ssh.FingerprintSHA256(key), PublicKey: key.Marshal(), Changed: changed,
	}
	m.mu.Lock()
	m.pending[pending.ID] = pending
	m.mu.Unlock()
	return pending
}

func authMethods(
	ctx context.Context,
	connection model.Connection,
	credential model.Credential,
	responses []string,
	interactiveHandler func(context.Context, InteractiveChallenge) ([]string, error),
) ([]ssh.AuthMethod, net.Conn, error) {
	var methods []ssh.AuthMethod
	var agentConnection net.Conn
	switch connection.Authentication {
	case "password":
		if credential.Password == "" {
			return nil, nil, errors.New("password is not configured")
		}
		methods = append(methods, ssh.Password(credential.Password))
	case "private_key":
		var signer ssh.Signer
		var err error
		if credential.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(
				[]byte(credential.PrivateKeyPEM), []byte(credential.Passphrase),
			)
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(credential.PrivateKeyPEM))
		}
		if err != nil {
			return nil, nil, errors.New("invalid private key or passphrase")
		}
		methods = append(methods, ssh.PublicKeys(signer))
	case "agent":
		agentClient, conn, err := sshagent.New()
		if err != nil {
			return nil, nil, fmt.Errorf("connect SSH agent: %w", err)
		}
		agentConnection = conn
		methods = append(methods, ssh.PublicKeysCallback(agentClient.Signers))
	case "interactive":
		index := 0
		methods = append(methods, ssh.KeyboardInteractive(func(
			user, instruction string, questions []string, echoes []bool,
		) ([]string, error) {
			if interactiveHandler != nil {
				answers, err := interactiveHandler(ctx, InteractiveChallenge{
					ID: uuid.NewString(), User: user, Instruction: instruction,
					Questions: append([]string(nil), questions...),
					Echoes:    append([]bool(nil), echoes...),
				})
				if err != nil {
					return nil, err
				}
				if len(answers) != len(questions) {
					return nil, errors.New("interactive authentication response count mismatch")
				}
				return answers, nil
			}
			answers := make([]string, len(questions))
			for questionIndex := range questions {
				if index >= len(responses) {
					return nil, errors.New("interactive authentication requires more responses")
				}
				answers[questionIndex] = responses[index]
				index++
			}
			return answers, nil
		}))
	default:
		return nil, nil, errors.New("unsupported authentication type")
	}
	return methods, agentConnection, nil
}

func (m *Manager) keepAlive(connection model.Connection, session *terminalSession) {
	if connection.KeepAliveSeconds <= 0 {
		return
	}
	ticker := time.NewTicker(time.Duration(connection.KeepAliveSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-session.done:
			return
		case <-ticker.C:
			_, _, err := session.client.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				session.close()
				return
			}
		}
	}
}

func (m *Manager) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/session/")
	m.mu.Lock()
	session := m.sessions[id]
	if session == nil || session.attached || r.URL.Query().Get("token") != session.token {
		m.mu.Unlock()
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	session.attached = true
	session.token = ""
	m.mu.Unlock()

	connection, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"wails.localhost", "localhost", "127.0.0.1"},
	})
	if err != nil {
		session.close()
		return
	}
	defer connection.CloseNow()

	ctx := r.Context()
	readDone := make(chan error, 1)
	go func() {
		for {
			messageType, data, err := connection.Read(ctx)
			if err != nil {
				readDone <- err
				return
			}
			if messageType != websocket.MessageBinary && messageType != websocket.MessageText {
				continue
			}
			if messageType == websocket.MessageText && session.encoding != nil {
				data, err = session.encoding.NewEncoder().Bytes(data)
				if err != nil {
					readDone <- errors.New("terminal input cannot be represented in selected encoding")
					return
				}
			}
			if _, err := session.stdin.Write(data); err != nil {
				readDone <- err
				return
			}
		}
	}()
	for {
		select {
		case <-session.done:
			_ = connection.Close(websocket.StatusNormalClosure, "session closed")
			return
		case <-readDone:
			session.close()
			return
		case data := <-session.output:
			writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := connection.Write(writeCtx, websocket.MessageBinary, data)
			cancel()
			if err != nil {
				session.close()
				return
			}
		}
	}
}

func terminalEncoding(label string) encoding.Encoding {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "gbk":
		return simplifiedchinese.GBK
	case "big5":
		return traditionalchinese.Big5
	case "shift_jis", "shift-jis", "sjis":
		return japanese.ShiftJIS
	case "euc-kr", "euckr":
		return korean.EUCKR
	default:
		return nil
	}
}

type channelWriter struct {
	session *terminalSession
}

func (w channelWriter) Write(data []byte) (int, error) {
	copyOfData := append([]byte(nil), data...)
	select {
	case <-w.session.done:
		return 0, io.ErrClosedPipe
	case w.session.output <- copyOfData:
		return len(data), nil
	}
}

func (s *terminalSession) close() {
	s.closeOnce.Do(func() {
		close(s.done)
		_ = s.stdin.Close()
		_ = s.session.Close()
		_ = s.client.Close()
	})
}

func randomToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
