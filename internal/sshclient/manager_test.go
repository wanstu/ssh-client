package sshclient

import (
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
