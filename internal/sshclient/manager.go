package sshclient

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

var errHostKeyRejected = errors.New("host key was not trusted")

type pendingChallenge struct {
	challenge HostKeyChallenge
	response  chan string
}

type managedSession struct {
	mu            sync.Mutex
	snapshot      SessionSnapshot
	cfg           ConnectConfig
	ctx           context.Context
	cancel        context.CancelFunc
	client        *ssh.Client
	shell         *ssh.Session
	stdin         io.WriteCloser
	retryNow      chan struct{}
	stopReconnect bool
}

type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*managedSession
	pending  map[string]*pendingChallenge
	known    *KnownHostStore
	emit     EmitFunc
}

func NewManager(dataDir string, emit EmitFunc) *Manager {
	return &Manager{sessions: map[string]*managedSession{}, pending: map[string]*pendingChallenge{}, known: NewKnownHostStore(dataDir), emit: emit}
}

func (m *Manager) Connect(cfg ConnectConfig) (SessionSnapshot, error) {
	cfg.Name = strings.TrimSpace(cfg.Name)
	cfg.Host = strings.TrimSpace(cfg.Host)
	cfg.Username = strings.TrimSpace(cfg.Username)
	if cfg.Host == "" || cfg.Username == "" {
		return SessionSnapshot{}, errors.New("host and username are required")
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return SessionSnapshot{}, errors.New("port must be between 1 and 65535")
	}
	if cfg.TimeoutSec <= 0 {
		cfg.TimeoutSec = 10
	}
	if cfg.Terminal.Term == "" {
		cfg.Terminal.Term = "xterm-256color"
	}
	if cfg.Name == "" {
		cfg.Name = cfg.Host
	}
	ctx, cancel := context.WithCancel(context.Background())
	now := time.Now()
	ms := &managedSession{
		cfg: cfg, ctx: ctx, cancel: cancel, retryNow: make(chan struct{}, 1),
		snapshot: SessionSnapshot{
			ID: runtimeID("session"), ProfileID: cfg.ProfileID, Name: cfg.Name,
			Target: net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port)),
			State:  "connecting", Stage: "tcp_connect", Message: "正在建立网络连接", StartedAtUnixMs: now.UnixMilli(),
		},
	}
	m.mu.Lock()
	m.sessions[ms.snapshot.ID] = ms
	m.mu.Unlock()
	m.emitState(ms)
	go m.run(ms)
	return ms.copySnapshot(), nil
}

func (m *Manager) Sessions() []SessionSnapshot {
	m.mu.RLock()
	items := make([]*managedSession, 0, len(m.sessions))
	for _, session := range m.sessions {
		items = append(items, session)
	}
	m.mu.RUnlock()
	result := make([]SessionSnapshot, 0, len(items))
	for _, session := range items {
		result = append(result, session.copySnapshot())
	}
	sort.Slice(result, func(i, j int) bool { return result[i].StartedAtUnixMs < result[j].StartedAtUnixMs })
	return result
}

func (m *Manager) Write(sessionID, data string) error {
	ms, err := m.session(sessionID)
	if err != nil {
		return err
	}
	ms.mu.Lock()
	stdin := ms.stdin
	ms.mu.Unlock()
	if stdin == nil {
		return errors.New("session shell is not connected")
	}
	_, err = io.WriteString(stdin, data)
	return err
}

func (m *Manager) Resize(sessionID string, cols, rows int) error {
	if cols < 1 || rows < 1 {
		return errors.New("terminal size must be positive")
	}
	ms, err := m.session(sessionID)
	if err != nil {
		return err
	}
	ms.mu.Lock()
	shell := ms.shell
	ms.mu.Unlock()
	if shell == nil {
		return nil
	}
	return shell.WindowChange(rows, cols)
}

func (m *Manager) RetryNow(sessionID string) error {
	ms, err := m.session(sessionID)
	if err != nil {
		return err
	}
	select {
	case ms.retryNow <- struct{}{}:
	default:
	}
	return nil
}

func (m *Manager) Disconnect(sessionID string) error {
	ms, err := m.session(sessionID)
	if err != nil {
		return err
	}
	ms.mu.Lock()
	ms.stopReconnect = true
	cancel := ms.cancel
	shell, client := ms.shell, ms.client
	ms.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if shell != nil {
		_ = shell.Close()
	}
	if client != nil {
		_ = client.Close()
	}
	return nil
}

func (m *Manager) CloseSession(sessionID string) error {
	if err := m.Disconnect(sessionID); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.sessions, sessionID)
	m.mu.Unlock()
	m.emitEvent(EventSessionClosed, map[string]string{"session_id": sessionID})
	return nil
}

func (m *Manager) CloseAll() {
	m.mu.RLock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.RUnlock()
	for _, id := range ids {
		_ = m.Disconnect(id)
	}
}

func (m *Manager) ResolveHostKey(challengeID, action string) error {
	m.mu.RLock()
	pending := m.pending[challengeID]
	m.mu.RUnlock()
	if pending == nil {
		return fmt.Errorf("host key challenge %q is no longer active", challengeID)
	}
	switch action {
	case "reject", "trust_once", "trust_save", "replace":
	default:
		return fmt.Errorf("invalid host key action %q", action)
	}
	select {
	case pending.response <- action:
		return nil
	default:
		return errors.New("host key challenge already resolved")
	}
}

func (m *Manager) run(ms *managedSession) {
	everConnected := false
	attempt := 0
	for {
		connected, err := m.runOnce(ms)
		everConnected = everConnected || connected
		if ms.ctx.Err() != nil {
			m.setFinalState(ms, "disconnected", "会话已断开", "", "")
			break
		}
		if err == nil {
			m.setFinalState(ms, "disconnected", "远端 Shell 已结束", "", "")
			break
		}
		coded := classifyRuntimeError(err, connected)
		if everConnected && coded.Retryable && ms.cfg.Reconnect.Enabled && !ms.reconnectDisabled() {
			attempt++
			delay := reconnectDelay(attempt)
			m.setReconnectState(ms, attempt, int(delay/time.Second), coded)
			timer := time.NewTimer(delay)
			select {
			case <-ms.ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				m.setFinalState(ms, "disconnected", "会话已断开", "", "")
				ms.clearCredentials()
				return
			case <-ms.retryNow:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
			case <-timer.C:
			}
			continue
		}
		state := "failed"
		if coded.Code == "HOST_KEY_CHANGED" {
			state = "security_blocked"
		}
		m.setFinalState(ms, state, userMessage(coded), coded.Code, coded.Error())
		break
	}
	ms.clearCredentials()
}

func (m *Manager) runOnce(ms *managedSession) (bool, error) {
	cfg := ms.cfg
	address := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
	m.setState(ms, "connecting", "tcp_connect", "正在连接 "+address, "", "")
	dialer := net.Dialer{Timeout: time.Duration(cfg.TimeoutSec) * time.Second}
	if cfg.KeepaliveSec > 0 {
		dialer.KeepAlive = time.Duration(cfg.KeepaliveSec) * time.Second
	}
	raw, err := dialer.DialContext(ms.ctx, "tcp", address)
	if err != nil {
		return false, &runtimeError{Code: networkCode(err), Stage: "tcp_connect", Retryable: true, Err: err}
	}
	methods, closers, err := buildAuthMethods(cfg.Auth, cfg.Credentials)
	if err != nil {
		_ = raw.Close()
		return false, err
	}
	defer func() {
		for _, closer := range closers {
			_ = closer.Close()
		}
	}()

	clientConfig := &ssh.ClientConfig{
		User: cfg.Username, Auth: methods, Timeout: time.Duration(cfg.TimeoutSec) * time.Second,
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return m.verifyHostKey(ms, address, key)
		},
	}
	m.setState(ms, "connecting", "ssh_handshake", "正在协商 SSH 连接", "", "")
	conn, chans, reqs, err := ssh.NewClientConn(raw, address, clientConfig)
	if err != nil {
		_ = raw.Close()
		return false, classifyHandshakeError(err)
	}
	client := ssh.NewClient(conn, chans, reqs)
	ms.mu.Lock()
	ms.client = client
	ms.mu.Unlock()
	defer func() { _ = client.Close(); ms.mu.Lock(); ms.client = nil; ms.mu.Unlock() }()

	m.setState(ms, "connecting", "open_session", "正在创建远端会话", "", "")
	shell, err := client.NewSession()
	if err != nil {
		return false, &runtimeError{Code: "SESSION_OPEN_FAILED", Stage: "open_session", Retryable: false, Err: err}
	}
	defer shell.Close()
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}
	if err := shell.RequestPty(cfg.Terminal.Term, 24, 80, modes); err != nil {
		return false, &runtimeError{Code: "PTY_REQUEST_FAILED", Stage: "open_session", Retryable: false, Err: err}
	}
	stdin, err := shell.StdinPipe()
	if err != nil {
		return false, &runtimeError{Code: "SHELL_OPEN_FAILED", Stage: "open_shell", Retryable: false, Err: err}
	}
	stdout, err := shell.StdoutPipe()
	if err != nil {
		return false, &runtimeError{Code: "SHELL_OPEN_FAILED", Stage: "open_shell", Retryable: false, Err: err}
	}
	stderr, err := shell.StderrPipe()
	if err != nil {
		return false, &runtimeError{Code: "SHELL_OPEN_FAILED", Stage: "open_shell", Retryable: false, Err: err}
	}
	if err := shell.Shell(); err != nil {
		return false, &runtimeError{Code: "SHELL_OPEN_FAILED", Stage: "open_shell", Retryable: false, Err: err}
	}
	ms.mu.Lock()
	ms.shell = shell
	ms.stdin = stdin
	ms.mu.Unlock()
	defer func() { ms.mu.Lock(); ms.shell = nil; ms.stdin = nil; ms.mu.Unlock() }()
	m.setConnected(ms)
	go m.pumpOutput(ms.snapshot.ID, stdout)
	go m.pumpOutput(ms.snapshot.ID, stderr)
	wait := make(chan error, 1)
	go func() { wait <- shell.Wait() }()
	select {
	case <-ms.ctx.Done():
		return true, ms.ctx.Err()
	case err := <-wait:
		if err == nil {
			return true, nil
		}
		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) {
			return true, nil
		}
		return true, err
	}
}

func (m *Manager) verifyHostKey(ms *managedSession, address string, key ssh.PublicKey) error {
	status, previous, err := m.known.Check(address, key)
	if err != nil {
		return &runtimeError{Code: "HOST_KEY_STORE_FAILED", Stage: "host_key_verify", Err: err}
	}
	if status == "match" {
		m.setState(ms, "authenticating", "authenticate", "正在验证用户身份", "", "")
		return nil
	}
	kind := status
	challenge := HostKeyChallenge{
		ID: runtimeID("hostkey"), SessionID: ms.snapshot.ID, Kind: kind, Host: ms.cfg.Host, Port: ms.cfg.Port, Address: address,
		Algorithm: key.Type(), Fingerprint: ssh.FingerprintSHA256(key),
	}
	if previous != nil {
		challenge.PreviousFingerprint = previous.Fingerprint
	}
	pending := &pendingChallenge{challenge: challenge, response: make(chan string, 1)}
	m.mu.Lock()
	m.pending[challenge.ID] = pending
	m.mu.Unlock()
	defer func() { m.mu.Lock(); delete(m.pending, challenge.ID); m.mu.Unlock() }()
	if kind == "changed" {
		m.setState(ms, "security_blocked", "host_key_verify", "主机身份已变化，连接已阻止", "HOST_KEY_CHANGED", "")
	} else {
		m.setState(ms, "host_key_pending", "host_key_verify", "等待确认主机身份", "HOST_KEY_UNKNOWN", "")
	}
	m.emitEvent(EventHostKey, challenge)
	select {
	case <-ms.ctx.Done():
		return ms.ctx.Err()
	case action := <-pending.response:
		if kind == "changed" {
			if action != "replace" {
				return &runtimeError{Code: "HOST_KEY_CHANGED", Stage: "host_key_verify", Err: errHostKeyRejected}
			}
			if err := m.known.Trust(address, key); err != nil {
				return err
			}
		} else {
			switch action {
			case "trust_once":
			case "trust_save", "replace":
				if err := m.known.Trust(address, key); err != nil {
					return err
				}
			default:
				return &runtimeError{Code: "HOST_KEY_REJECTED", Stage: "host_key_verify", Err: errHostKeyRejected}
			}
		}
		m.setState(ms, "authenticating", "authenticate", "正在验证用户身份", "", "")
		return nil
	}
}

func (m *Manager) pumpOutput(sessionID string, reader io.Reader) {
	buf := make([]byte, 16*1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			m.emitEvent(EventOutput, OutputEvent{SessionID: sessionID, DataBase64: base64.StdEncoding.EncodeToString(buf[:n])})
		}
		if err != nil {
			return
		}
	}
}

func (m *Manager) session(id string) (*managedSession, error) {
	m.mu.RLock()
	session := m.sessions[id]
	m.mu.RUnlock()
	if session == nil {
		return nil, fmt.Errorf("session %q not found", id)
	}
	return session, nil
}

func (s *managedSession) copySnapshot() SessionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot
}
func (s *managedSession) reconnectDisabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopReconnect
}
func (s *managedSession) clearCredentials() {
	s.mu.Lock()
	s.cfg.Credentials.Password = ""
	s.cfg.Credentials.Passphrase = ""
	s.mu.Unlock()
}

func (m *Manager) setState(ms *managedSession, state, stage, message, code, detail string) {
	ms.mu.Lock()
	ms.snapshot.State, ms.snapshot.Stage, ms.snapshot.Message = state, stage, message
	ms.snapshot.ReasonCode, ms.snapshot.TechnicalDetail = code, detail
	ms.snapshot.NextRetrySeconds = 0
	snapshot := ms.snapshot
	ms.mu.Unlock()
	m.emitEvent(EventSessionState, snapshot)
}

func (m *Manager) setConnected(ms *managedSession) {
	now := time.Now()
	ms.mu.Lock()
	ms.snapshot.State, ms.snapshot.Stage, ms.snapshot.Message = "connected", "connected", "已连接"
	ms.snapshot.ReasonCode, ms.snapshot.TechnicalDetail = "", ""
	ms.snapshot.ConnectedAtUnixMs = now.UnixMilli()
	ms.snapshot.DisconnectedAtUnixMs = 0
	ms.snapshot.NextRetrySeconds = 0
	snapshot := ms.snapshot
	ms.mu.Unlock()
	m.emitEvent(EventSessionState, snapshot)
}

func (m *Manager) setReconnectState(ms *managedSession, attempt, seconds int, err *runtimeError) {
	ms.mu.Lock()
	ms.snapshot.State, ms.snapshot.Stage = "reconnecting", err.Stage
	ms.snapshot.Message = "连接已中断，正在自动重连"
	ms.snapshot.ReasonCode, ms.snapshot.TechnicalDetail = err.Code, err.Error()
	ms.snapshot.ReconnectAttempt, ms.snapshot.NextRetrySeconds = attempt, seconds
	snapshot := ms.snapshot
	ms.mu.Unlock()
	m.emitEvent(EventSessionState, snapshot)
}

func (m *Manager) setFinalState(ms *managedSession, state, message, code, detail string) {
	now := time.Now()
	ms.mu.Lock()
	ms.snapshot.State, ms.snapshot.Message = state, message
	ms.snapshot.ReasonCode, ms.snapshot.TechnicalDetail = code, detail
	ms.snapshot.NextRetrySeconds = 0
	ms.snapshot.DisconnectedAtUnixMs = now.UnixMilli()
	snapshot := ms.snapshot
	ms.mu.Unlock()
	m.emitEvent(EventSessionState, snapshot)
}

func (m *Manager) emitState(ms *managedSession) { m.emitEvent(EventSessionState, ms.copySnapshot()) }
func (m *Manager) emitEvent(event string, payload any) {
	if m.emit != nil {
		m.emit(event, payload)
	}
}

func runtimeID(prefix string) string {
	var raw [8]byte
	_, _ = rand.Read(raw[:])
	return prefix + "_" + hex.EncodeToString(raw[:])
}

func reconnectDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	seconds := 1 << min(attempt-1, 4)
	return time.Duration(seconds) * time.Second
}

func classifyHandshakeError(err error) error {
	var coded *runtimeError
	if errors.As(err, &coded) {
		return coded
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "unable to authenticate") || strings.Contains(text, "no supported methods remain") {
		return &runtimeError{Code: "AUTH_METHOD_REJECTED", Stage: "authenticate", Retryable: false, Err: err}
	}
	return &runtimeError{Code: "SSH_NEGOTIATION_FAILED", Stage: "ssh_handshake", Retryable: false, Err: err}
}

func classifyRuntimeError(err error, connected bool) *runtimeError {
	var coded *runtimeError
	if errors.As(err, &coded) {
		return coded
	}
	if errors.Is(err, context.Canceled) {
		return &runtimeError{Code: "DISCONNECTED", Stage: "connected", Err: err}
	}
	if connected {
		return &runtimeError{Code: "CONNECTION_RESET", Stage: "connected", Retryable: true, Err: err}
	}
	return &runtimeError{Code: "SSH_CONNECTION_FAILED", Stage: "ssh_handshake", Err: err}
}

func networkCode(err error) string {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "NETWORK_TIMEOUT"
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "refused") {
		return "CONNECTION_REFUSED"
	}
	if strings.Contains(text, "no such host") {
		return "DNS_RESOLVE_FAILED"
	}
	if strings.Contains(text, "unreachable") {
		return "NETWORK_UNREACHABLE"
	}
	return "NETWORK_UNREACHABLE"
}

func userMessage(err *runtimeError) string {
	switch err.Code {
	case "NETWORK_TIMEOUT":
		return "连接超时"
	case "CONNECTION_REFUSED":
		return "目标主机拒绝连接"
	case "DNS_RESOLVE_FAILED":
		return "无法解析主机地址"
	case "NETWORK_UNREACHABLE":
		return "无法连接到目标网络"
	case "AUTH_PASSWORD_REQUIRED":
		return "需要输入密码"
	case "AUTH_KEY_PASSPHRASE_REQUIRED":
		return "私钥需要口令"
	case "AUTH_AGENT_UNAVAILABLE":
		return "SSH Agent 不可用"
	case "AUTH_METHOD_REJECTED", "AUTH_KEY_REJECTED":
		return "身份验证失败"
	case "HOST_KEY_CHANGED":
		return "主机身份已变化，连接已阻止"
	case "HOST_KEY_REJECTED":
		return "未信任主机身份"
	case "PTY_REQUEST_FAILED":
		return "远端不支持请求的终端"
	case "SHELL_OPEN_FAILED":
		return "无法启动远端 Shell"
	default:
		return "SSH 连接失败"
	}
}
