package sshclient

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wanstu/ssh-client/internal/model"
	"golang.org/x/crypto/ssh"
)

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
