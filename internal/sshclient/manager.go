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
	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/transform"
)

var errHostKeyRejected = errors.New("host key was not trusted")

type pendingChallenge struct {
	challenge HostKeyChallenge
	response  chan string
}

type managedPane struct {
	id    string
	shell *ssh.Session
	stdin io.WriteCloser
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
	panes         map[string]*managedPane
	retryNow      chan struct{}
	stopReconnect bool
	closed        bool
}

type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*managedSession
	pending  map[string]*pendingChallenge
	known    *KnownHostStore
	emit     EmitFunc
}

func normalizeJumpConfig(cfg *ConnectConfig) error {
	if cfg.Jump == nil {
		return nil
	}
	jump := cfg.Jump
	jump.Name = strings.TrimSpace(jump.Name)
	jump.Host = strings.TrimSpace(jump.Host)
	jump.Username = strings.TrimSpace(jump.Username)
	if jump.Host == "" || jump.Username == "" {
		return errors.New("jump host and username are required")
	}
	if jump.Port < 1 || jump.Port > 65535 {
		return errors.New("jump host port must be between 1 and 65535")
	}
	if jump.TimeoutSec <= 0 {
		jump.TimeoutSec = cfg.TimeoutSec
	}
	if jump.TimeoutSec <= 0 {
		jump.TimeoutSec = 10
	}
	if jump.KeepaliveSec < 0 {
		jump.KeepaliveSec = 0
	}
	if jump.Name == "" {
		jump.Name = jump.Host
	}
	return nil
}

func cloneConnectConfig(cfg ConnectConfig) ConnectConfig {
	cloned := cfg
	if cfg.Jump != nil {
		jumpCopy := *cfg.Jump
		cloned.Jump = &jumpCopy
	}
	return cloned
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
	if err := normalizeJumpConfig(&cfg); err != nil {
		return SessionSnapshot{}, err
	}
	if cfg.TimeoutSec <= 0 {
		cfg.TimeoutSec = 10
	}
	if cfg.Terminal.Term == "" {
		cfg.Terminal.Term = "xterm-256color"
	}
	if strings.TrimSpace(cfg.Terminal.Encoding) == "" {
		cfg.Terminal.Encoding = "UTF-8"
	}
	if cfg.Name == "" {
		cfg.Name = cfg.Host
	}
	ctx, cancel := context.WithCancel(context.Background())
	now := time.Now()
	ms := &managedSession{
		cfg: cfg, ctx: ctx, cancel: cancel, panes: map[string]*managedPane{}, retryNow: make(chan struct{}, 1),
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
	encodingName := ms.cfg.Terminal.Encoding
	ms.mu.Unlock()
	if stdin == nil {
		return errors.New("session shell is not connected")
	}
	payload, err := encodeTerminalInput(data, encodingName)
	if err != nil {
		return err
	}
	_, err = stdin.Write(payload)
	return err
}

func encodeTerminalInput(data, encodingName string) ([]byte, error) {
	encodingName = strings.TrimSpace(encodingName)
	if encodingName == "" || strings.EqualFold(encodingName, "UTF-8") || strings.EqualFold(encodingName, "UTF8") {
		return []byte(data), nil
	}
	enc, err := htmlindex.Get(encodingName)
	if err != nil || enc == nil {
		return nil, fmt.Errorf("unsupported terminal encoding %q", encodingName)
	}
	payload, _, err := transform.Bytes(enc.NewEncoder(), []byte(data))
	if err != nil {
		return nil, fmt.Errorf("encode terminal input as %s: %w", encodingName, err)
	}
	return payload, nil
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

func (m *Manager) OpenPane(sessionID string, cols, rows int) (TerminalPaneSnapshot, error) {
	if cols < 1 || rows < 1 {
		return TerminalPaneSnapshot{}, errors.New("terminal size must be positive")
	}
	ms, err := m.session(sessionID)
	if err != nil {
		return TerminalPaneSnapshot{}, err
	}

	ms.mu.Lock()
	if ms.snapshot.State != "connected" || ms.client == nil {
		ms.mu.Unlock()
		return TerminalPaneSnapshot{}, errors.New("session is not connected")
	}
	if len(ms.panes) >= 1 {
		ms.mu.Unlock()
		return TerminalPaneSnapshot{}, errors.New("only one split pane is supported")
	}
	client := ms.client
	term := ms.cfg.Terminal.Term
	if term == "" {
		term = "xterm-256color"
	}
	ms.mu.Unlock()

	shell, err := client.NewSession()
	if err != nil {
		return TerminalPaneSnapshot{}, fmt.Errorf("open split shell: %w", err)
	}
	closeShell := true
	defer func() {
		if closeShell {
			_ = shell.Close()
		}
	}()

	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}
	if err := shell.RequestPty(term, rows, cols, modes); err != nil {
		return TerminalPaneSnapshot{}, fmt.Errorf("request split pty: %w", err)
	}
	stdin, err := shell.StdinPipe()
	if err != nil {
		return TerminalPaneSnapshot{}, fmt.Errorf("open split stdin: %w", err)
	}
	stdout, err := shell.StdoutPipe()
	if err != nil {
		return TerminalPaneSnapshot{}, fmt.Errorf("open split stdout: %w", err)
	}
	stderr, err := shell.StderrPipe()
	if err != nil {
		return TerminalPaneSnapshot{}, fmt.Errorf("open split stderr: %w", err)
	}
	if err := shell.Shell(); err != nil {
		return TerminalPaneSnapshot{}, fmt.Errorf("start split shell: %w", err)
	}

	paneID := runtimeID("pane")
	pane := &managedPane{id: paneID, shell: shell, stdin: stdin}
	ms.mu.Lock()
	if ms.client != client || ms.snapshot.State != "connected" {
		ms.mu.Unlock()
		return TerminalPaneSnapshot{}, errors.New("session disconnected while opening split pane")
	}
	if len(ms.panes) >= 1 {
		ms.mu.Unlock()
		return TerminalPaneSnapshot{}, errors.New("only one split pane is supported")
	}
	ms.panes[paneID] = pane
	ms.mu.Unlock()
	closeShell = false

	go m.pumpPaneOutput(sessionID, paneID, stdout)
	go m.pumpPaneOutput(sessionID, paneID, stderr)
	go m.waitPane(ms, paneID, shell)

	return TerminalPaneSnapshot{ID: paneID, SessionID: sessionID, Title: "shell · 2"}, nil
}

func (m *Manager) WritePane(sessionID, paneID, data string) error {
	ms, err := m.session(sessionID)
	if err != nil {
		return err
	}
	ms.mu.Lock()
	pane := ms.panes[paneID]
	encodingName := ms.cfg.Terminal.Encoding
	ms.mu.Unlock()
	if pane == nil || pane.stdin == nil {
		return fmt.Errorf("pane %q not found", paneID)
	}
	payload, err := encodeTerminalInput(data, encodingName)
	if err != nil {
		return err
	}
	_, err = pane.stdin.Write(payload)
	return err
}

func (m *Manager) ResizePane(sessionID, paneID string, cols, rows int) error {
	if cols < 1 || rows < 1 {
		return errors.New("terminal size must be positive")
	}
	ms, err := m.session(sessionID)
	if err != nil {
		return err
	}
	ms.mu.Lock()
	pane := ms.panes[paneID]
	ms.mu.Unlock()
	if pane == nil || pane.shell == nil {
		return fmt.Errorf("pane %q not found", paneID)
	}
	return pane.shell.WindowChange(rows, cols)
}

func (m *Manager) ClosePane(sessionID, paneID string) error {
	ms, err := m.session(sessionID)
	if err != nil {
		return err
	}
	ms.mu.Lock()
	pane := ms.panes[paneID]
	if pane != nil {
		delete(ms.panes, paneID)
	}
	ms.mu.Unlock()
	if pane == nil {
		return nil
	}
	_ = pane.shell.Close()
	m.emitEvent(EventPaneClosed, PaneEvent{SessionID: sessionID, PaneID: paneID})
	return nil
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
	m.closeAllPanes(ms)
	if shell != nil {
		_ = shell.Close()
	}
	if client != nil {
		_ = client.Close()
	}
	return nil
}

func (m *Manager) ReconnectSession(sessionID string, cfg ConnectConfig) (SessionSnapshot, error) {
	old, err := m.session(sessionID)
	if err != nil {
		return SessionSnapshot{}, err
	}

	cfg.Name = strings.TrimSpace(cfg.Name)
	cfg.Host = strings.TrimSpace(cfg.Host)
	cfg.Username = strings.TrimSpace(cfg.Username)
	if cfg.Host == "" || cfg.Username == "" {
		return SessionSnapshot{}, errors.New("host and username are required")
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return SessionSnapshot{}, errors.New("port must be between 1 and 65535")
	}
	if err := normalizeJumpConfig(&cfg); err != nil {
		return SessionSnapshot{}, err
	}
	if cfg.TimeoutSec <= 0 {
		cfg.TimeoutSec = 10
	}
	if cfg.Terminal.Term == "" {
		cfg.Terminal.Term = "xterm-256color"
	}
	if strings.TrimSpace(cfg.Terminal.Encoding) == "" {
		cfg.Terminal.Encoding = "UTF-8"
	}
	if cfg.Name == "" {
		cfg.Name = cfg.Host
	}

	old.mu.Lock()
	state := old.snapshot.State
	if old.closed {
		old.mu.Unlock()
		return SessionSnapshot{}, fmt.Errorf("session %q is closed and cannot reconnect", sessionID)
	}
	switch state {
	case "disconnected", "failed", "security_blocked":
	default:
		old.mu.Unlock()
		return SessionSnapshot{}, fmt.Errorf("session %q is %s and cannot reconnect", sessionID, state)
	}
	old.closed = true
	cancel := old.cancel
	shell, client := old.shell, old.client
	old.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	m.closeAllPanes(old)
	if shell != nil {
		_ = shell.Close()
	}
	if client != nil {
		_ = client.Close()
	}

	ctx, nextCancel := context.WithCancel(context.Background())
	now := time.Now()
	replacement := &managedSession{
		cfg: cfg, ctx: ctx, cancel: nextCancel, panes: map[string]*managedPane{}, retryNow: make(chan struct{}, 1),
		snapshot: SessionSnapshot{
			ID: sessionID, ProfileID: cfg.ProfileID, Name: cfg.Name,
			Target: net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port)),
			State:  "connecting", Stage: "tcp_connect", Message: "正在重新建立网络连接", StartedAtUnixMs: now.UnixMilli(),
		},
	}

	m.mu.Lock()
	if current := m.sessions[sessionID]; current != old {
		m.mu.Unlock()
		nextCancel()
		return SessionSnapshot{}, fmt.Errorf("session %q changed while reconnecting", sessionID)
	}
	m.sessions[sessionID] = replacement
	m.mu.Unlock()

	m.emitState(replacement)
	go m.run(replacement)
	return replacement.copySnapshot(), nil
}

func (m *Manager) CloneSession(sessionID string) (SessionSnapshot, error) {
	ms, err := m.session(sessionID)
	if err != nil {
		return SessionSnapshot{}, err
	}

	ms.mu.Lock()
	cfg := cloneConnectConfig(ms.cfg)
	state := ms.snapshot.State
	closed := ms.closed
	ms.mu.Unlock()

	if closed {
		return SessionSnapshot{}, fmt.Errorf("session %q is closed and cannot be cloned", sessionID)
	}
	switch state {
	case "connecting", "authenticating", "host_key_pending", "connected", "reconnecting":
	default:
		return SessionSnapshot{}, fmt.Errorf("session %q is not active and cannot be cloned", sessionID)
	}

	return m.Connect(cfg)
}

func (m *Manager) CloseSession(sessionID string) error {
	ms, err := m.session(sessionID)
	if err != nil {
		return err
	}
	ms.mu.Lock()
	if ms.closed {
		ms.mu.Unlock()
		return nil
	}
	ms.closed = true
	ms.mu.Unlock()

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

func dialTCPContext(ctx context.Context, host string, port, timeoutSec, keepaliveSec int) (net.Conn, error) {
	address := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	dialer := net.Dialer{Timeout: time.Duration(timeoutSec) * time.Second}
	if keepaliveSec > 0 {
		dialer.KeepAlive = time.Duration(keepaliveSec) * time.Second
	}
	return dialer.DialContext(ctx, "tcp", address)
}

func closeAuthClosers(closers []io.Closer) {
	for _, closer := range closers {
		_ = closer.Close()
	}
}

func remapJumpAuthError(err error) error {
	var coded *runtimeError
	if !errors.As(err, &coded) {
		return &runtimeError{Code: "JUMP_HOST_AUTH_FAILED", Stage: "jump_host_authenticate", Err: err}
	}
	switch coded.Code {
	case "AUTH_PASSWORD_REQUIRED":
		return &runtimeError{Code: "JUMP_HOST_PASSWORD_REQUIRED", Stage: "jump_host_authenticate", Err: coded.Err}
	case "AUTH_KEY_PASSPHRASE_REQUIRED":
		return &runtimeError{Code: "JUMP_HOST_KEY_PASSPHRASE_REQUIRED", Stage: "jump_host_authenticate", Err: coded.Err}
	case "AUTH_AGENT_UNAVAILABLE":
		return &runtimeError{Code: "JUMP_HOST_AGENT_UNAVAILABLE", Stage: "jump_host_authenticate", Err: coded.Err}
	case "HOST_KEY_CHANGED", "HOST_KEY_REJECTED", "HOST_KEY_STORE_FAILED":
		return coded
	default:
		return &runtimeError{Code: "JUMP_HOST_AUTH_FAILED", Stage: "jump_host_authenticate", Err: coded.Err}
	}
}

func classifyJumpHandshakeError(err error) error {
	var coded *runtimeError
	if errors.As(err, &coded) {
		if strings.HasPrefix(coded.Code, "HOST_KEY_") {
			return coded
		}
		return remapJumpAuthError(coded)
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "unable to authenticate") || strings.Contains(text, "no supported methods remain") {
		return &runtimeError{Code: "JUMP_HOST_AUTH_FAILED", Stage: "jump_host_authenticate", Err: err}
	}
	return &runtimeError{Code: "JUMP_HOST_NEGOTIATION_FAILED", Stage: "jump_host_handshake", Err: err}
}

type dialResult struct {
	conn net.Conn
	err  error
}

func dialThroughJump(ctx context.Context, client *ssh.Client, address string, timeout time.Duration) (net.Conn, error) {
	result := make(chan dialResult, 1)
	go func() {
		conn, err := client.Dial("tcp", address)
		result <- dialResult{conn: conn, err: err}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		_ = client.Close()
		return nil, ctx.Err()
	case <-timer.C:
		_ = client.Close()
		return nil, &runtimeError{Code: "JUMP_HOST_TARGET_TIMEOUT", Stage: "jump_host_dial", Retryable: true, Err: errors.New("timed out opening target connection through jump host")}
	case value := <-result:
		if value.err != nil {
			return nil, &runtimeError{Code: "JUMP_HOST_TARGET_DIAL_FAILED", Stage: "jump_host_dial", Retryable: true, Err: value.err}
		}
		return value.conn, nil
	}
}

func (m *Manager) openTargetConn(ms *managedSession, cfg ConnectConfig, targetAddress string) (net.Conn, func(), error) {
	if cfg.Jump == nil {
		m.setState(ms, "connecting", "tcp_connect", "正在连接 "+targetAddress, "", "")
		raw, err := dialTCPContext(ms.ctx, cfg.Host, cfg.Port, cfg.TimeoutSec, cfg.KeepaliveSec)
		if err != nil {
			return nil, func() {}, &runtimeError{Code: networkCode(err), Stage: "tcp_connect", Retryable: true, Err: err}
		}
		return raw, func() {}, nil
	}

	jump := *cfg.Jump
	jumpAddress := net.JoinHostPort(jump.Host, fmt.Sprintf("%d", jump.Port))
	m.setState(ms, "connecting", "jump_host_connect", "正在连接 Jump Host "+jumpAddress, "", "")
	rawJump, err := dialTCPContext(ms.ctx, jump.Host, jump.Port, jump.TimeoutSec, jump.KeepaliveSec)
	if err != nil {
		return nil, func() {}, &runtimeError{Code: "JUMP_HOST_CONNECT_FAILED", Stage: "jump_host_connect", Retryable: true, Err: err}
	}

	methods, closers, err := buildAuthMethods(jump.Auth, jump.Credentials)
	if err != nil {
		_ = rawJump.Close()
		closeAuthClosers(closers)
		return nil, func() {}, remapJumpAuthError(err)
	}
	defer closeAuthClosers(closers)

	jumpClientConfig := &ssh.ClientConfig{
		User: jump.Username, Auth: methods, Timeout: time.Duration(jump.TimeoutSec) * time.Second,
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return m.verifyHostKeyFor(ms, "jump", jump.Name, jump.Host, jump.Port, jumpAddress, key)
		},
	}
	m.setState(ms, "connecting", "jump_host_handshake", "正在协商 Jump Host SSH 连接", "", "")
	conn, chans, reqs, err := ssh.NewClientConn(rawJump, jumpAddress, jumpClientConfig)
	if err != nil {
		_ = rawJump.Close()
		return nil, func() {}, classifyJumpHandshakeError(err)
	}
	jumpClient := ssh.NewClient(conn, chans, reqs)

	m.setState(ms, "connecting", "jump_host_dial", "正在通过 Jump Host 连接目标 "+targetAddress, "", "")
	targetRaw, err := dialThroughJump(ms.ctx, jumpClient, targetAddress, time.Duration(cfg.TimeoutSec)*time.Second)
	if err != nil {
		_ = jumpClient.Close()
		return nil, func() {}, err
	}
	cleanup := func() {
		_ = targetRaw.Close()
		_ = jumpClient.Close()
	}
	return targetRaw, cleanup, nil
}

func (m *Manager) runOnce(ms *managedSession) (bool, error) {
	cfg := ms.cfg
	address := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
	raw, cleanupTransport, err := m.openTargetConn(ms, cfg, address)
	if err != nil {
		return false, err
	}
	defer cleanupTransport()

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
	defer func() {
		m.closeAllPanes(ms)
		_ = client.Close()
		ms.mu.Lock()
		ms.client = nil
		ms.mu.Unlock()
	}()

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
	return m.verifyHostKeyFor(ms, "target", ms.cfg.Name, ms.cfg.Host, ms.cfg.Port, address, key)
}

func (m *Manager) verifyHostKeyFor(ms *managedSession, scope, name, host string, port int, address string, key ssh.PublicKey) error {
	stage := "host_key_verify"
	authStage := "authenticate"
	pendingMessage := "等待确认主机身份"
	changedMessage := "主机身份已变化，连接已阻止"
	authMessage := "正在验证用户身份"
	if scope == "jump" {
		stage = "jump_host_verify"
		authStage = "jump_host_authenticate"
		pendingMessage = "等待确认 Jump Host 身份"
		changedMessage = "Jump Host 身份已变化，连接已阻止"
		authMessage = "正在验证 Jump Host 身份"
	}

	status, previous, err := m.known.Check(address, key)
	if err != nil {
		return &runtimeError{Code: "HOST_KEY_STORE_FAILED", Stage: stage, Err: err}
	}
	if status == "match" {
		if scope == "target" {
			m.setState(ms, "authenticating", authStage, authMessage, "", "")
		} else {
			m.setState(ms, "connecting", authStage, authMessage, "", "")
		}
		return nil
	}

	kind := status
	challenge := HostKeyChallenge{
		ID: runtimeID("hostkey"), SessionID: ms.snapshot.ID, Kind: kind, Scope: scope, Name: name,
		Host: host, Port: port, Address: address, Algorithm: key.Type(), Fingerprint: ssh.FingerprintSHA256(key),
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
		m.setState(ms, "security_blocked", stage, changedMessage, "HOST_KEY_CHANGED", "")
	} else {
		m.setState(ms, "host_key_pending", stage, pendingMessage, "HOST_KEY_UNKNOWN", "")
	}
	m.emitEvent(EventHostKey, challenge)

	select {
	case <-ms.ctx.Done():
		return ms.ctx.Err()
	case action := <-pending.response:
		if kind == "changed" {
			if action != "replace" {
				return &runtimeError{Code: "HOST_KEY_CHANGED", Stage: stage, Err: errHostKeyRejected}
			}
			if err := m.known.Trust(address, key); err != nil {
				return &runtimeError{Code: "HOST_KEY_STORE_FAILED", Stage: stage, Err: err}
			}
		} else {
			switch action {
			case "trust_once":
			case "trust_save", "replace":
				if err := m.known.Trust(address, key); err != nil {
					return &runtimeError{Code: "HOST_KEY_STORE_FAILED", Stage: stage, Err: err}
				}
			default:
				return &runtimeError{Code: "HOST_KEY_REJECTED", Stage: stage, Err: errHostKeyRejected}
			}
		}

		if scope == "target" {
			m.setState(ms, "authenticating", authStage, authMessage, "", "")
		} else {
			m.setState(ms, "connecting", authStage, authMessage, "", "")
		}
		return nil
	}
}

func (m *Manager) pumpPaneOutput(sessionID, paneID string, reader io.Reader) {
	buf := make([]byte, 16*1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			m.emitEvent(EventOutput, OutputEvent{SessionID: sessionID, PaneID: paneID, DataBase64: base64.StdEncoding.EncodeToString(buf[:n])})
		}
		if err != nil {
			return
		}
	}
}

func (m *Manager) waitPane(ms *managedSession, paneID string, shell *ssh.Session) {
	_ = shell.Wait()
	ms.mu.Lock()
	pane := ms.panes[paneID]
	if pane == nil || pane.shell != shell {
		ms.mu.Unlock()
		return
	}
	delete(ms.panes, paneID)
	sessionID := ms.snapshot.ID
	ms.mu.Unlock()
	m.emitEvent(EventPaneClosed, PaneEvent{SessionID: sessionID, PaneID: paneID})
}

func (m *Manager) closeAllPanes(ms *managedSession) {
	ms.mu.Lock()
	if len(ms.panes) == 0 {
		ms.mu.Unlock()
		return
	}
	panes := make([]*managedPane, 0, len(ms.panes))
	sessionID := ms.snapshot.ID
	for _, pane := range ms.panes {
		panes = append(panes, pane)
	}
	ms.panes = map[string]*managedPane{}
	ms.mu.Unlock()

	for _, pane := range panes {
		if pane.shell != nil {
			_ = pane.shell.Close()
		}
		m.emitEvent(EventPaneClosed, PaneEvent{SessionID: sessionID, PaneID: pane.id})
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
	if s.cfg.Jump != nil {
		s.cfg.Jump.Credentials.Password = ""
		s.cfg.Jump.Credentials.Passphrase = ""
	}
	s.mu.Unlock()
}

func (m *Manager) setState(ms *managedSession, state, stage, message, code, detail string) {
	ms.mu.Lock()
	if ms.closed {
		ms.mu.Unlock()
		return
	}
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
	if ms.closed {
		ms.mu.Unlock()
		return
	}
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
	if ms.closed {
		ms.mu.Unlock()
		return
	}
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
	if ms.closed {
		ms.mu.Unlock()
		return
	}
	ms.snapshot.State, ms.snapshot.Message = state, message
	ms.snapshot.ReasonCode, ms.snapshot.TechnicalDetail = code, detail
	ms.snapshot.NextRetrySeconds = 0
	ms.snapshot.DisconnectedAtUnixMs = now.UnixMilli()
	snapshot := ms.snapshot
	ms.mu.Unlock()
	m.emitEvent(EventSessionState, snapshot)
}

func (m *Manager) emitState(ms *managedSession) {
	ms.mu.Lock()
	if ms.closed {
		ms.mu.Unlock()
		return
	}
	snapshot := ms.snapshot
	ms.mu.Unlock()
	m.emitEvent(EventSessionState, snapshot)
}
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
	case "JUMP_HOST_CONNECT_FAILED":
		return "无法连接 Jump Host"
	case "JUMP_HOST_PASSWORD_REQUIRED":
		return "Jump Host 需要已保存密码"
	case "JUMP_HOST_KEY_PASSPHRASE_REQUIRED":
		return "Jump Host 私钥需要单独口令"
	case "JUMP_HOST_AGENT_UNAVAILABLE":
		return "Jump Host 的 SSH Agent 不可用"
	case "JUMP_HOST_AUTH_FAILED":
		return "Jump Host 身份验证失败"
	case "JUMP_HOST_NEGOTIATION_FAILED":
		return "Jump Host SSH 协商失败"
	case "JUMP_HOST_TARGET_TIMEOUT":
		return "通过 Jump Host 连接目标超时"
	case "JUMP_HOST_TARGET_DIAL_FAILED":
		return "Jump Host 无法连接目标主机"
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
