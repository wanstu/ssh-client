package sshclient

import (
	"encoding/base64"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wanstu/ssh-client/internal/model"
)

func TestManagerSplitPaneUsesIndependentShellChannel(t *testing.T) {
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
		case EventSessionState, EventOutput, EventPaneClosed:
			events <- payload
		}
	})

	profile := model.DefaultProfile()
	session, err := manager.Connect(ConnectConfig{
		Name:        "split-test",
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
	defer manager.Disconnect(session.ID)

	deadline := time.After(5 * time.Second)
	for {
		select {
		case payload := <-events:
			if state, ok := payload.(SessionSnapshot); ok && state.ID == session.ID && state.State == "connected" {
				goto connected
			}
		case <-deadline:
			t.Fatal("timed out waiting for session connection")
		}
	}

connected:
	pane, err := manager.OpenPane(session.ID, 90, 28)
	if err != nil {
		t.Fatal(err)
	}
	if pane.ID == "" || pane.SessionID != session.ID {
		t.Fatalf("unexpected pane snapshot: %#v", pane)
	}
	if _, err := manager.OpenPane(session.ID, 90, 28); err == nil {
		t.Fatal("expected second split pane to be rejected")
	}
	if err := manager.ResizePane(session.ID, pane.ID, 100, 30); err != nil {
		t.Fatal(err)
	}
	if err := manager.WritePane(session.ID, pane.ID, "split-pane\r"); err != nil {
		t.Fatal(err)
	}

	sawSplitOutput := false
	deadline = time.After(5 * time.Second)
	for !sawSplitOutput {
		select {
		case payload := <-events:
			output, ok := payload.(OutputEvent)
			if !ok || output.SessionID != session.ID || output.PaneID != pane.ID {
				continue
			}
			data, err := base64.StdEncoding.DecodeString(output.DataBase64)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "ready") || strings.Contains(string(data), "split-pane") {
				sawSplitOutput = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for split pane output")
		}
	}

	if err := manager.ClosePane(session.ID, pane.ID); err != nil {
		t.Fatal(err)
	}
	if err := manager.WritePane(session.ID, pane.ID, "closed"); err == nil {
		t.Fatal("expected closed pane write to fail")
	}

	if err := manager.Write(session.ID, "primary-still-live\r"); err != nil {
		t.Fatalf("primary shell should remain writable after split close: %v", err)
	}
	sawPrimaryOutput := false
	deadline = time.After(5 * time.Second)
	for !sawPrimaryOutput {
		select {
		case payload := <-events:
			output, ok := payload.(OutputEvent)
			if !ok || output.SessionID != session.ID || output.PaneID != "" {
				continue
			}
			data, err := base64.StdEncoding.DecodeString(output.DataBase64)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "primary-still-live") {
				sawPrimaryOutput = true
			}
		case <-deadline:
			t.Fatal("timed out waiting for primary output after split close")
		}
	}
}
