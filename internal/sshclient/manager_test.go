package sshclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wanstu/ssh-client/internal/model"
	"golang.org/x/crypto/ssh"
)

func TestCloneConnectConfigCopiesJumpCredentials(t *testing.T) {
	original := ConnectConfig{
		Credentials: Credentials{Password: "target"},
		Jump:        &JumpConfig{ProfileID: "jump", Credentials: Credentials{Password: "jump"}},
		SOCKS5:      &SOCKS5Config{Host: "127.0.0.1", Port: 1080},
	}
	cloned := cloneConnectConfig(original)
	if cloned.Jump == original.Jump {
		t.Fatal("jump config pointer was shared")
	}
	if cloned.SOCKS5 == original.SOCKS5 {
		t.Fatal("SOCKS5 config pointer was shared")
	}
	cloned.Jump.Credentials.Password = ""
	cloned.SOCKS5.Host = "changed"
	if original.Jump.Credentials.Password != "jump" {
		t.Fatal("clearing cloned jump credentials mutated original config")
	}
	if original.SOCKS5.Host != "127.0.0.1" {
		t.Fatal("changing cloned SOCKS5 config mutated original config")
	}
}

func TestNormalizeSOCKS5RejectsJumpCombination(t *testing.T) {
	cfg := ConnectConfig{
		Jump:   &JumpConfig{Host: "jump", Port: 22, Username: "root"},
		SOCKS5: &SOCKS5Config{Host: "127.0.0.1", Port: 1080},
	}
	if err := normalizeSOCKS5Config(&cfg); err == nil {
		t.Fatal("expected jump host and SOCKS5 combination to be rejected")
	}
}

func TestManagerDirectPasswordSession(t *testing.T) {
	addr, stopServer := startTestSSHServer(t)
	defer stopServer()

	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}

	var manager *Manager
	events := make(chan any, 64)
	manager = NewManager(t.TempDir(), func(event string, payload any) {
		switch event {
		case EventHostKey:
			challenge := payload.(HostKeyChallenge)
			if err := manager.ResolveHostKey(challenge.ID, "trust_once"); err != nil {
				t.Errorf("resolve host key: %v", err)
			}
		case EventSessionState, EventOutput:
			events <- payload
		}
	})

	profile := model.DefaultProfile()
	session, err := manager.Connect(ConnectConfig{
		Name:         "test",
		Host:         host,
		Port:         port,
		Username:     "tester",
		Auth:         model.AuthConfig{Mode: "password"},
		Terminal:     profile.Terminal,
		Reconnect:    model.ReconnectConfig{Enabled: false, KeepTabOnDisconnect: true},
		TimeoutSec:   3,
		KeepaliveSec: 0,
		Credentials:  Credentials{Password: "secret"},
	})
	if err != nil {
		t.Fatal(err)
	}

	connected := false
	ready := false
	deadline := time.After(5 * time.Second)
	for !(connected && ready) {
		select {
		case payload := <-events:
			switch value := payload.(type) {
			case SessionSnapshot:
				if value.ID == session.ID && value.State == "connected" {
					connected = true
					if err := manager.Write(session.ID, "hello\r"); err != nil {
						t.Fatal(err)
					}
				}
			case OutputEvent:
				if value.SessionID != session.ID {
					continue
				}
				data, err := base64.StdEncoding.DecodeString(value.DataBase64)
				if err != nil {
					t.Fatal(err)
				}
				text := string(data)
				if strings.Contains(text, "ready") || strings.Contains(text, "hello") {
					ready = true
				}
			}
		case <-deadline:
			t.Fatalf("timed out waiting for connected/output: connected=%v ready=%v", connected, ready)
		}
	}

	if err := manager.Disconnect(session.ID); err != nil {
		t.Fatal(err)
	}
}

func TestSOCKS5AddressPreservesDomainName(t *testing.T) {
	request, err := socks5Address("internal.example", 22)
	if err != nil {
		t.Fatal(err)
	}
	if len(request) < 7 {
		t.Fatalf("SOCKS5 request too short: %x", request)
	}
	if request[0] != 0x05 || request[1] != 0x01 || request[2] != 0x00 || request[3] != 0x03 {
		t.Fatalf("unexpected SOCKS5 request header: %x", request[:4])
	}
	nameLen := int(request[4])
	if got := string(request[5 : 5+nameLen]); got != "internal.example" {
		t.Fatalf("domain = %q", got)
	}
	if request[len(request)-2] != 0 || request[len(request)-1] != 22 {
		t.Fatalf("port bytes = %x", request[len(request)-2:])
	}
}

func TestManagerSOCKS5PasswordSession(t *testing.T) {
	targetAddr, stopTarget := startTestSSHServer(t)
	defer stopTarget()
	proxyAddr, stopProxy := startTestSOCKS5Server(t)
	defer stopProxy()

	targetHost, targetPortText, err := net.SplitHostPort(targetAddr)
	if err != nil {
		t.Fatal(err)
	}
	targetPort, err := strconv.Atoi(targetPortText)
	if err != nil {
		t.Fatal(err)
	}
	proxyHost, proxyPortText, err := net.SplitHostPort(proxyAddr)
	if err != nil {
		t.Fatal(err)
	}
	proxyPort, err := strconv.Atoi(proxyPortText)
	if err != nil {
		t.Fatal(err)
	}

	var manager *Manager
	events := make(chan any, 128)
	scopes := make(chan string, 4)
	manager = NewManager(t.TempDir(), func(event string, payload any) {
		switch event {
		case EventHostKey:
			challenge := payload.(HostKeyChallenge)
			scopes <- challenge.Scope
			if err := manager.ResolveHostKey(challenge.ID, "trust_once"); err != nil {
				t.Errorf("resolve host key: %v", err)
			}
		case EventSessionState, EventOutput:
			events <- payload
		}
	})

	profile := model.DefaultProfile()
	session, err := manager.Connect(ConnectConfig{
		Name:        "socks-target",
		Host:        targetHost,
		Port:        targetPort,
		Username:    "tester",
		Auth:        model.AuthConfig{Mode: "password"},
		Terminal:    profile.Terminal,
		Reconnect:   model.ReconnectConfig{Enabled: false, KeepTabOnDisconnect: true},
		TimeoutSec:  3,
		Credentials: Credentials{Password: "secret"},
		SOCKS5:      &SOCKS5Config{Host: proxyHost, Port: proxyPort},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Disconnect(session.ID)

	connected := false
	echoed := false
	sawProxyStage := false
	deadline := time.After(5 * time.Second)
	for !(connected && echoed && sawProxyStage) {
		select {
		case payload := <-events:
			switch value := payload.(type) {
			case SessionSnapshot:
				if value.ID != session.ID {
					continue
				}
				if value.Stage == "socks5_connect" || value.Stage == "socks5_handshake" {
					sawProxyStage = true
				}
				if value.State == "connected" && !connected {
					connected = true
					if err := manager.Write(session.ID, "socks-ok\r"); err != nil {
						t.Fatal(err)
					}
				}
			case OutputEvent:
				if value.SessionID != session.ID {
					continue
				}
				data, err := base64.StdEncoding.DecodeString(value.DataBase64)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(data), "socks-ok") {
					echoed = true
				}
			}
		case <-deadline:
			t.Fatalf("timed out waiting for SOCKS5 session: connected=%v echoed=%v proxyStage=%v", connected, echoed, sawProxyStage)
		}
	}

	select {
	case scope := <-scopes:
		if scope != "target" {
			t.Fatalf("host key scope = %q, want target", scope)
		}
	default:
		t.Fatal("expected target host key challenge")
	}
}

func TestManagerJumpHostPasswordSession(t *testing.T) {
	targetAddr, stopTarget := startTestSSHServer(t)
	defer stopTarget()
	jumpAddr, stopJump := startTestSSHServer(t)
	defer stopJump()

	targetHost, targetPortText, err := net.SplitHostPort(targetAddr)
	if err != nil {
		t.Fatal(err)
	}
	targetPort, err := strconv.Atoi(targetPortText)
	if err != nil {
		t.Fatal(err)
	}
	jumpHost, jumpPortText, err := net.SplitHostPort(jumpAddr)
	if err != nil {
		t.Fatal(err)
	}
	jumpPort, err := strconv.Atoi(jumpPortText)
	if err != nil {
		t.Fatal(err)
	}

	var manager *Manager
	events := make(chan any, 128)
	scopes := make(chan string, 4)
	manager = NewManager(t.TempDir(), func(event string, payload any) {
		switch event {
		case EventHostKey:
			challenge := payload.(HostKeyChallenge)
			scopes <- challenge.Scope
			if err := manager.ResolveHostKey(challenge.ID, "trust_once"); err != nil {
				t.Errorf("resolve host key: %v", err)
			}
		case EventSessionState, EventOutput:
			events <- payload
		}
	})

	profile := model.DefaultProfile()
	session, err := manager.Connect(ConnectConfig{
		Name:        "jump-target",
		Host:        targetHost,
		Port:        targetPort,
		Username:    "tester",
		Auth:        model.AuthConfig{Mode: "password"},
		Terminal:    profile.Terminal,
		Reconnect:   model.ReconnectConfig{Enabled: false, KeepTabOnDisconnect: true},
		TimeoutSec:  3,
		Credentials: Credentials{Password: "secret"},
		Jump: &JumpConfig{
			ProfileID: "jump_profile",
			Name:      "jump-test", Host: jumpHost, Port: jumpPort, Username: "tester",
			Auth: model.AuthConfig{Mode: "password"}, TimeoutSec: 3,
			Credentials: Credentials{Password: "secret"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Disconnect(session.ID)

	connected := false
	echoed := false
	deadline := time.After(5 * time.Second)
	for !(connected && echoed) {
		select {
		case payload := <-events:
			switch value := payload.(type) {
			case SessionSnapshot:
				if value.ID == session.ID && value.State == "connected" {
					connected = true
					if err := manager.Write(session.ID, "jump-ok\r"); err != nil {
						t.Fatal(err)
					}
				}
			case OutputEvent:
				if value.SessionID != session.ID {
					continue
				}
				data, err := base64.StdEncoding.DecodeString(value.DataBase64)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(data), "jump-ok") {
					echoed = true
				}
			}
		case <-deadline:
			t.Fatalf("timed out waiting for jump session: connected=%v echoed=%v", connected, echoed)
		}
	}

	gotScopes := []string{}
	for len(scopes) > 0 {
		gotScopes = append(gotScopes, <-scopes)
	}
	if len(gotScopes) < 2 || gotScopes[0] != "jump" || gotScopes[1] != "target" {
		t.Fatalf("host key scopes = %#v, want jump then target", gotScopes)
	}
}

func TestManagerCloneSessionCreatesIndependentConnection(t *testing.T) {
	addr, stopServer := startTestSSHServer(t)
	defer stopServer()

	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}

	var manager *Manager
	events := make(chan any, 128)
	manager = NewManager(t.TempDir(), func(event string, payload any) {
		switch event {
		case EventHostKey:
			challenge := payload.(HostKeyChallenge)
			if err := manager.ResolveHostKey(challenge.ID, "trust_once"); err != nil {
				t.Errorf("resolve host key: %v", err)
			}
		case EventSessionState, EventOutput:
			events <- payload
		}
	})

	profile := model.DefaultProfile()
	original, err := manager.Connect(ConnectConfig{
		ProfileID:   "profile_clone",
		Name:        "clone-test",
		Host:        host,
		Port:        port,
		Username:    "tester",
		Auth:        model.AuthConfig{Mode: "password"},
		Terminal:    profile.Terminal,
		Reconnect:   model.ReconnectConfig{Enabled: false, KeepTabOnDisconnect: true},
		TimeoutSec:  3,
		Credentials: Credentials{Password: "secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Disconnect(original.ID)

	waitConnected := func(sessionID string) {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case payload := <-events:
				if state, ok := payload.(SessionSnapshot); ok && state.ID == sessionID && state.State == "connected" {
					return
				}
			case <-deadline:
				t.Fatalf("timed out waiting for session %s", sessionID)
			}
		}
	}
	waitConnected(original.ID)

	cloned, err := manager.CloneSession(original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cloned.ID == "" || cloned.ID == original.ID {
		t.Fatalf("clone ID = %q, original ID = %q", cloned.ID, original.ID)
	}
	if cloned.ProfileID != original.ProfileID || cloned.Name != original.Name || cloned.Target != original.Target {
		t.Fatalf("clone snapshot mismatch: original=%#v clone=%#v", original, cloned)
	}
	waitConnected(cloned.ID)

	if err := manager.CloseSession(cloned.ID); err != nil {
		t.Fatal(err)
	}
	if err := manager.Write(original.ID, "still-alive\r"); err != nil {
		t.Fatalf("original session stopped after clone close: %v", err)
	}

	deadline := time.After(5 * time.Second)
	for {
		select {
		case payload := <-events:
			output, ok := payload.(OutputEvent)
			if !ok || output.SessionID != original.ID {
				continue
			}
			data, err := base64.StdEncoding.DecodeString(output.DataBase64)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "still-alive") {
				return
			}
		case <-deadline:
			t.Fatal("original session did not produce output after clone was closed")
		}
	}
}

func TestManagerReconnectSessionReusesSessionID(t *testing.T) {
	manager := NewManager(t.TempDir(), func(string, any) {})
	ctx, cancel := context.WithCancel(context.Background())
	old := &managedSession{
		cfg: ConnectConfig{
			ProfileID:  "profile_reconnect",
			Name:       "old",
			Host:       "127.0.0.1",
			Port:       22,
			Username:   "tester",
			Auth:       model.AuthConfig{Mode: "password"},
			Terminal:   model.DefaultProfile().Terminal,
			TimeoutSec: 1,
		},
		ctx:      ctx,
		cancel:   cancel,
		panes:    map[string]*managedPane{},
		retryNow: make(chan struct{}, 1),
		snapshot: SessionSnapshot{
			ID:    "session_reconnect",
			State: "disconnected",
		},
	}
	manager.sessions[old.snapshot.ID] = old

	cfg := old.cfg
	cfg.Name = "updated"
	cfg.Credentials = Credentials{Password: "secret"}
	next, err := manager.ReconnectSession(old.snapshot.ID, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Disconnect(next.ID)

	if next.ID != old.snapshot.ID {
		t.Fatalf("reconnect ID = %q, want %q", next.ID, old.snapshot.ID)
	}
	if next.State != "connecting" || next.Name != "updated" {
		t.Fatalf("unexpected reconnect snapshot: %#v", next)
	}
	if !old.closed {
		t.Fatal("old managed session was not closed before replacement")
	}
	current, err := manager.session(next.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current == old {
		t.Fatal("reconnect did not replace the managed session instance")
	}
}

func TestClosedSessionSuppressesLateStateEvents(t *testing.T) {
	emitted := 0
	manager := NewManager(t.TempDir(), func(event string, payload any) {
		if event == EventSessionState {
			emitted++
		}
	})
	session := &managedSession{
		closed: true,
		snapshot: SessionSnapshot{
			ID:    "session_closed",
			State: "connected",
		},
	}

	manager.setState(session, "connecting", "tcp_connect", "ignored", "", "")
	manager.setConnected(session)
	manager.setReconnectState(session, 1, 1, &runtimeError{Code: "CONNECTION_RESET", Stage: "connected", Err: errors.New("reset")})
	manager.setFinalState(session, "disconnected", "ignored", "", "")
	manager.emitState(session)

	if emitted != 0 {
		t.Fatalf("closed session emitted %d late state events", emitted)
	}
	if session.snapshot.State != "connected" {
		t.Fatalf("closed session state mutated to %q", session.snapshot.State)
	}
}

func TestManagedSessionClearCredentialsClearsJumpCredentials(t *testing.T) {
	session := &managedSession{cfg: ConnectConfig{
		Credentials: Credentials{Password: "target-password", Passphrase: "target-passphrase"},
		Jump:        &JumpConfig{Credentials: Credentials{Password: "jump-password", Passphrase: "jump-passphrase"}},
	}}
	session.clearCredentials()
	if session.cfg.Credentials.Password != "" || session.cfg.Credentials.Passphrase != "" {
		t.Fatalf("target credentials were not cleared: %#v", session.cfg.Credentials)
	}
	if session.cfg.Jump.Credentials.Password != "" || session.cfg.Jump.Credentials.Passphrase != "" {
		t.Fatalf("jump credentials were not cleared: %#v", session.cfg.Jump.Credentials)
	}
}

func TestEncodeTerminalInput(t *testing.T) {
	utf8Payload, err := encodeTerminalInput("中文", "UTF-8")
	if err != nil {
		t.Fatal(err)
	}
	if string(utf8Payload) != "中文" {
		t.Fatalf("UTF-8 input changed: %x", utf8Payload)
	}

	gbkPayload, err := encodeTerminalInput("中文", "GBK")
	if err != nil {
		t.Fatal(err)
	}
	wantGBK := []byte{0xD6, 0xD0, 0xCE, 0xC4}
	if string(gbkPayload) != string(wantGBK) {
		t.Fatalf("GBK input = %x, want %x", gbkPayload, wantGBK)
	}

	if _, err := encodeTerminalInput("test", "not-a-real-encoding"); err == nil {
		t.Fatal("expected unsupported encoding error")
	}
}

func startTestSOCKS5Server(t *testing.T) (string, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	done := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-done:
					return
				default:
					return
				}
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				handleTestSOCKS5Conn(conn)
			}()
		}
	}()

	stop := func() {
		close(done)
		_ = listener.Close()
		wg.Wait()
	}
	return listener.Addr().String(), stop
}

func handleTestSOCKS5Conn(conn net.Conn) {
	defer conn.Close()

	greeting := make([]byte, 3)
	if _, err := io.ReadFull(conn, greeting); err != nil {
		return
	}
	if greeting[0] != 0x05 || greeting[1] != 0x01 || greeting[2] != 0x00 {
		_, _ = conn.Write([]byte{0x05, 0xff})
		return
	}
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return
	}

	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return
	}
	if header[0] != 0x05 || header[1] != 0x01 {
		return
	}

	var host string
	switch header[3] {
	case 0x01:
		raw := make([]byte, 4)
		if _, err := io.ReadFull(conn, raw); err != nil {
			return
		}
		host = net.IP(raw).String()
	case 0x04:
		raw := make([]byte, 16)
		if _, err := io.ReadFull(conn, raw); err != nil {
			return
		}
		host = net.IP(raw).String()
	case 0x03:
		length := []byte{0}
		if _, err := io.ReadFull(conn, length); err != nil {
			return
		}
		raw := make([]byte, int(length[0]))
		if _, err := io.ReadFull(conn, raw); err != nil {
			return
		}
		host = string(raw)
	default:
		_, _ = conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}

	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBytes); err != nil {
		return
	}
	port := int(portBytes[0])<<8 | int(portBytes[1])
	upstream, err := net.Dial("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		_, _ = conn.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer upstream.Close()

	if _, err := conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 127, 0, 0, 1, 0, 0}); err != nil {
		return
	}

	copyDone := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(conn, upstream); copyDone <- struct{}{} }()
	go func() { _, _ = io.Copy(upstream, conn); copyDone <- struct{}{} }()
	<-copyDone
}

func startTestSSHServer(t *testing.T) (string, func()) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}

	serverConfig := &ssh.ServerConfig{
		PasswordCallback: func(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if meta.User() == "tester" && string(password) == "secret" {
				return nil, nil
			}
			return nil, errors.New("denied")
		},
	}
	serverConfig.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	done := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-done:
					return
				default:
					return
				}
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				handleTestSSHConn(conn, serverConfig)
			}()
		}
	}()

	stop := func() {
		close(done)
		_ = listener.Close()
		wg.Wait()
	}
	return listener.Addr().String(), stop
}

func handleTestSSHConn(conn net.Conn, config *ssh.ServerConfig) {
	defer conn.Close()
	_, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(requests)
	for request := range channels {
		if request.ChannelType() == "direct-tcpip" {
			var target struct {
				Host       string
				Port       uint32
				OriginHost string
				OriginPort uint32
			}
			if err := ssh.Unmarshal(request.ExtraData(), &target); err != nil {
				_ = request.Reject(ssh.ConnectionFailed, "invalid direct-tcpip payload")
				continue
			}
			upstream, err := net.Dial("tcp", net.JoinHostPort(target.Host, strconv.Itoa(int(target.Port))))
			if err != nil {
				_ = request.Reject(ssh.ConnectionFailed, err.Error())
				continue
			}
			channel, reqs, err := request.Accept()
			if err != nil {
				_ = upstream.Close()
				continue
			}
			go ssh.DiscardRequests(reqs)
			go func(ch ssh.Channel, upstream net.Conn) {
				defer ch.Close()
				defer upstream.Close()
				done := make(chan struct{}, 2)
				go func() { _, _ = io.Copy(ch, upstream); done <- struct{}{} }()
				go func() { _, _ = io.Copy(upstream, ch); done <- struct{}{} }()
				<-done
			}(channel, upstream)
			continue
		}
		if request.ChannelType() != "session" {
			_ = request.Reject(ssh.UnknownChannelType, "unsupported")
			continue
		}
		channel, reqs, err := request.Accept()
		if err != nil {
			continue
		}
		go func(ch ssh.Channel, reqs <-chan *ssh.Request) {
			defer ch.Close()
			for req := range reqs {
				switch req.Type {
				case "pty-req":
					_ = req.Reply(true, nil)
				case "shell":
					_ = req.Reply(true, nil)
					_, _ = ch.Write([]byte("ready\r\n"))
					go func() {
						buf := make([]byte, 1024)
						for {
							n, err := ch.Read(buf)
							if n > 0 {
								_, _ = ch.Write(buf[:n])
							}
							if err != nil {
								return
							}
						}
					}()
				default:
					_ = req.Reply(false, nil)
				}
			}
		}(channel, reqs)
	}
}
