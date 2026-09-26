package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/wanstu/ssh-client/internal/config"
	"github.com/wanstu/ssh-client/internal/model"
	"github.com/wanstu/ssh-client/internal/sshclient"
	"github.com/wanstu/wails-desktop-kit/secureconfig"
)

type testCredentialBackend struct {
	mu    sync.Mutex
	items map[string]string
}

func newTestCredentialBackend() *testCredentialBackend {
	return &testCredentialBackend{items: map[string]string{}}
}

func (b *testCredentialBackend) key(service, user string) string {
	return service + "\x00" + user
}

func (b *testCredentialBackend) Get(service, user string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	value, ok := b.items[b.key(service, user)]
	if !ok {
		return "", secureconfig.ErrNotFound
	}
	return value, nil
}

func (b *testCredentialBackend) Set(service, user, password string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.items[b.key(service, user)] = password
	return nil
}

func (b *testCredentialBackend) Delete(service, user string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := b.key(service, user)
	if _, ok := b.items[key]; !ok {
		return secureconfig.ErrNotFound
	}
	delete(b.items, key)
	return nil
}

func newCredentialTestApp(t *testing.T) (*App, model.ConnectionProfile) {
	t.Helper()
	dir := t.TempDir()
	store := config.NewStoreAt(dir)
	secure, err := secureconfig.NewAt("ssh-client", dir, newTestCredentialBackend())
	if err != nil {
		t.Fatal(err)
	}

	profile := model.DefaultProfile()
	profile.ID = "profile_test"
	profile.Name = "test"
	profile.Host = "127.0.0.1"
	profile.Username = "root"
	profile.Auth.Mode = "password"

	settings := model.DefaultSettings()
	settings.Profiles = []model.ConnectionProfile{profile}
	if err := store.Save(settings); err != nil {
		t.Fatal(err)
	}

	app := &App{
		store:             store,
		secure:            secure,
		pendingCredential: map[string]pendingCredentialAction{},
	}
	return app, profile
}

func TestCredentialSavedOnlyAfterConnected(t *testing.T) {
	app, profile := newCredentialTestApp(t)
	sessionID := "session_1"
	app.pendingCredential[sessionID] = pendingCredentialAction{
		ProfileID: profile.ID,
		Password:  "correct-password",
		Remember:  true,
	}

	app.handleCredentialState(sshclient.SessionSnapshot{ID: sessionID, State: "failed"})
	if _, err := app.secure.Get(passwordCredentialRef(profile.ID)); !errors.Is(err, secureconfig.ErrNotFound) {
		t.Fatalf("failed connection unexpectedly saved password: %v", err)
	}

	app.pendingCredential[sessionID] = pendingCredentialAction{
		ProfileID: profile.ID,
		Password:  "correct-password",
		Remember:  true,
	}
	app.handleCredentialState(sshclient.SessionSnapshot{ID: sessionID, State: "connected"})

	password, err := app.secure.Get(passwordCredentialRef(profile.ID))
	if err != nil {
		t.Fatal(err)
	}
	if string(password) != "correct-password" {
		t.Fatalf("saved password = %q", password)
	}

	settings, err := app.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Profiles[0].Auth.CredentialRef != passwordCredentialRef(profile.ID) {
		t.Fatalf("credential ref not persisted: %#v", settings.Profiles[0].Auth)
	}
}

func TestCommandHistoryEncryptedAndCapped(t *testing.T) {
	app, _ := newCredentialTestApp(t)

	seed := make([]CommandHistoryEntry, commandHistoryLimit)
	for i := range seed {
		seed[i] = CommandHistoryEntry{
			Command:   "sensitive-command",
			SessionID: "session_seed",
			Target:    "root@example:22",
			CreatedAt: int64(i + 1),
		}
	}
	if err := app.secure.SaveJSON(commandHistorySecureKey, seed); err != nil {
		t.Fatal(err)
	}

	history, err := app.RecordCommandHistory(CommandHistoryEntry{
		Command:   "top-secret-command",
		SessionID: "session_new",
		Target:    "root@example:22",
		CreatedAt: 2000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != commandHistoryLimit {
		t.Fatalf("history length = %d, want %d", len(history), commandHistoryLimit)
	}
	if history[0].Command != "top-secret-command" {
		t.Fatalf("latest command = %q", history[0].Command)
	}

	loaded, err := app.GetCommandHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != commandHistoryLimit || loaded[0].Command != "top-secret-command" {
		t.Fatalf("persisted history mismatch: len=%d first=%q", len(loaded), loaded[0].Command)
	}

	files, err := os.ReadDir(app.secure.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("encrypted command history file was not created")
	}
	for _, entry := range files {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(app.secure.Dir(), entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("top-secret-command")) || bytes.Contains(data, []byte("sensitive-command")) {
			t.Fatalf("encrypted payload %q contains plaintext command history", entry.Name())
		}
	}
}

func TestCommandHistoryClearIsIdempotent(t *testing.T) {
	app, _ := newCredentialTestApp(t)
	if err := app.ClearCommandHistory(); err != nil {
		t.Fatal(err)
	}
	if _, err := app.RecordCommandHistory(CommandHistoryEntry{Command: "pwd"}); err != nil {
		t.Fatal(err)
	}
	if err := app.ClearCommandHistory(); err != nil {
		t.Fatal(err)
	}
	if err := app.ClearCommandHistory(); err != nil {
		t.Fatal(err)
	}
	history, err := app.GetCommandHistory()
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 0 {
		t.Fatalf("history length after clear = %d", len(history))
	}
}

func TestCredentialClearedAfterSuccessfulOptOut(t *testing.T) {
	app, profile := newCredentialTestApp(t)
	ref := passwordCredentialRef(profile.ID)
	if err := app.secure.Put(ref, []byte("old-password")); err != nil {
		t.Fatal(err)
	}
	_, err := app.store.Update(func(settings *model.Settings) error {
		settings.Profiles[0].Auth.CredentialRef = ref
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	sessionID := "session_2"
	app.pendingCredential[sessionID] = pendingCredentialAction{
		ProfileID:   profile.ID,
		Password:    "new-password",
		Remember:    false,
		ExistingRef: ref,
	}
	app.handleCredentialState(sshclient.SessionSnapshot{ID: sessionID, State: "connected"})

	if _, err := app.secure.Get(ref); !errors.Is(err, secureconfig.ErrNotFound) {
		t.Fatalf("saved credential still exists: %v", err)
	}
	settings, err := app.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Profiles[0].Auth.CredentialRef != "" {
		t.Fatalf("credential ref was not cleared: %#v", settings.Profiles[0].Auth)
	}
}

func TestNormalizeProfileTerminalSettings(t *testing.T) {
	profile := model.DefaultProfile()
	profile.Terminal.FontSize = 48
	profile.Terminal.ScrollbackLines = 10
	profile.Terminal.ColorScheme = "unknown"
	profile.Terminal.CursorStyle = "box"

	normalized := normalizeProfile(profile)
	defaults := model.DefaultProfile()
	if normalized.Terminal.FontSize != defaults.Terminal.FontSize {
		t.Fatalf("font size = %d, want %d", normalized.Terminal.FontSize, defaults.Terminal.FontSize)
	}
	if normalized.Terminal.ScrollbackLines != defaults.Terminal.ScrollbackLines {
		t.Fatalf("scrollback = %d, want %d", normalized.Terminal.ScrollbackLines, defaults.Terminal.ScrollbackLines)
	}
	if normalized.Terminal.ColorScheme != "midnight" {
		t.Fatalf("color scheme = %q", normalized.Terminal.ColorScheme)
	}
	if normalized.Terminal.CursorStyle != "bar" {
		t.Fatalf("cursor style = %q", normalized.Terminal.CursorStyle)
	}

	profile.Terminal.FontSize = 10
	profile.Terminal.ScrollbackLines = 100000
	profile.Terminal.ColorScheme = "DAYLIGHT"
	profile.Terminal.CursorStyle = "UNDERLINE"
	normalized = normalizeProfile(profile)
	if normalized.Terminal.FontSize != 10 || normalized.Terminal.ScrollbackLines != 100000 {
		t.Fatalf("valid terminal numeric settings changed: %#v", normalized.Terminal)
	}
	if normalized.Terminal.ColorScheme != "daylight" || normalized.Terminal.CursorStyle != "underline" {
		t.Fatalf("valid terminal enum settings changed: %#v", normalized.Terminal)
	}
}
