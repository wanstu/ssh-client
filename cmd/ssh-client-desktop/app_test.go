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
	kitautostart "github.com/wanstu/wails-desktop-kit/autostart"
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

	launchAtLogin, err := kitautostart.New(kitautostart.Config{
		ID: "ssh-client-test", DisplayName: "SSH Client Test", Arguments: []string{"--autostart"},
	})
	if err != nil {
		t.Fatal(err)
	}
	app := &App{
		store:             store,
		secure:            secure,
		launchAtLogin:     launchAtLogin,
		pendingCredential: map[string]pendingCredentialAction{},
	}
	app.sessions = sshclient.NewManager(dir, app.emit)
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

func TestDuplicateProfileNameAvoidsCollisions(t *testing.T) {
	profiles := []model.ConnectionProfile{
		{Name: "prod"},
		{Name: "prod 副本"},
		{Name: "PROD 副本 2"},
	}
	if got := duplicateProfileName(profiles, "prod"); got != "prod 副本 3" {
		t.Fatalf("duplicateProfileName() = %q, want %q", got, "prod 副本 3")
	}
	if got := duplicateProfileName(profiles, "staging"); got != "staging 副本" {
		t.Fatalf("duplicateProfileName() = %q, want %q", got, "staging 副本")
	}
}

func TestStoredProfileConnectConfigResolvesJumpHostCredentials(t *testing.T) {
	app, target := newCredentialTestApp(t)

	jump := model.DefaultProfile()
	jump.ID = "profile_jump"
	jump.Name = "jump"
	jump.Host = "10.0.0.10"
	jump.Username = "jump-user"
	jump.Auth.Mode = "password"
	jump.Auth.CredentialRef = passwordCredentialRef(jump.ID)

	target.Network.Mode = "jump_host"
	target.Network.JumpProfileID = jump.ID

	settings := model.DefaultSettings()
	settings.Profiles = []model.ConnectionProfile{jump, target}
	if err := app.store.Save(settings); err != nil {
		t.Fatal(err)
	}
	if err := app.secure.Put(jump.Auth.CredentialRef, []byte("jump-secret")); err != nil {
		t.Fatal(err)
	}

	cfg, err := app.storedProfileConnectConfig(settings, target, sshclient.Credentials{Password: "target-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Jump == nil {
		t.Fatal("jump config was not resolved")
	}
	if cfg.Jump.ProfileID != jump.ID || cfg.Jump.Host != jump.Host || cfg.Jump.Username != jump.Username {
		t.Fatalf("unexpected jump config: %#v", cfg.Jump)
	}
	if cfg.Jump.Credentials.Password != "jump-secret" {
		t.Fatalf("jump password was not loaded from secure storage")
	}
	if cfg.Credentials.Password != "target-secret" {
		t.Fatalf("target credentials changed: %#v", cfg.Credentials)
	}

	jump.Auth.CredentialRef = ""
	settings.Profiles[0] = jump
	if _, err := app.storedProfileConnectConfig(settings, target, sshclient.Credentials{}); err == nil {
		t.Fatal("expected missing saved jump password error")
	}

	jump.Network.Mode = "jump_host"
	jump.Network.JumpProfileID = target.ID
	settings.Profiles[0] = jump
	if _, err := app.storedProfileConnectConfig(settings, target, sshclient.Credentials{}); err == nil {
		t.Fatal("expected nested jump host rejection")
	}
	if err := settings.Validate(); err == nil {
		t.Fatal("expected settings validation to reject nested jump host")
	}
}

func TestBatchProfileOperations(t *testing.T) {
	app, profileA := newCredentialTestApp(t)
	profileA.Tags = []string{"ops"}
	ref := passwordCredentialRef(profileA.ID)
	profileA.Auth.CredentialRef = ref
	if err := app.secure.Put(ref, []byte("saved-password")); err != nil {
		t.Fatal(err)
	}

	profileB := model.DefaultProfile()
	profileB.ID = "profile_b"
	profileB.Name = "profile-b"
	profileB.Host = "10.0.0.2"
	profileB.Username = "root"
	profileB.Tags = []string{"Blue", "remove"}

	profileC := model.DefaultProfile()
	profileC.ID = "profile_c"
	profileC.Name = "profile-c"
	profileC.Host = "10.0.0.3"
	profileC.Username = "root"
	profileC.Network.Mode = "jump_host"
	profileC.Network.JumpProfileID = profileA.ID

	settings := model.DefaultSettings()
	settings.Groups = []model.ConnectionGroup{{ID: "group_ops", Name: "Ops", Order: 1}}
	settings.Profiles = []model.ConnectionProfile{profileA, profileB, profileC}
	if err := app.store.Save(settings); err != nil {
		t.Fatal(err)
	}

	moved, err := app.MoveProfiles([]string{profileA.ID, profileB.ID}, "group_ops")
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range moved.Settings.Profiles[:2] {
		if profile.GroupID != "group_ops" {
			t.Fatalf("profile %q group = %q", profile.ID, profile.GroupID)
		}
	}

	favorited, err := app.SetProfilesFavorite([]string{profileA.ID, profileB.ID}, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range favorited.Settings.Profiles[:2] {
		if !profile.Favorite {
			t.Fatalf("profile %q was not favorited", profile.ID)
		}
	}

	tagged, err := app.UpdateProfilesTags(
		[]string{profileA.ID, profileB.ID},
		[]string{" Prod ", "prod"},
		[]string{"OPS", "remove"},
	)
	if err != nil {
		t.Fatal(err)
	}
	gotTags := map[string][]string{}
	for _, profile := range tagged.Settings.Profiles {
		gotTags[profile.ID] = profile.Tags
	}
	if len(gotTags[profileA.ID]) != 1 || gotTags[profileA.ID][0] != "Prod" {
		t.Fatalf("profile A tags = %#v", gotTags[profileA.ID])
	}
	if len(gotTags[profileB.ID]) != 2 || gotTags[profileB.ID][0] != "Blue" || gotTags[profileB.ID][1] != "Prod" {
		t.Fatalf("profile B tags = %#v", gotTags[profileB.ID])
	}

	if _, err := app.DeleteProfiles([]string{profileA.ID, profileB.ID}); err == nil {
		t.Fatal("expected delete to fail while an unselected profile depends on selected jump host")
	}
	if _, err := app.secure.Get(ref); err != nil {
		t.Fatalf("credential was removed after rejected delete: %v", err)
	}

	deleted, err := app.DeleteProfiles([]string{profileA.ID, profileB.ID, profileC.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted.Settings.Profiles) != 0 {
		t.Fatalf("profiles remain after batch delete: %#v", deleted.Settings.Profiles)
	}
	if _, err := app.secure.Get(ref); !errors.Is(err, secureconfig.ErrNotFound) {
		t.Fatalf("credential still exists after batch delete: %v", err)
	}
}

func TestCommandSnippetCRUD(t *testing.T) {
	app, _ := newCredentialTestApp(t)
	originalCommand := "  journalctl -u app --since today | tail -n 50  "

	created, err := app.CreateSnippet(model.CommandSnippet{
		Name:    "  日志查看  ",
		Command: originalCommand,
		Tags:    []string{" ops ", "OPS", " prod "},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Settings.Snippets) != 1 {
		t.Fatalf("snippet count = %d", len(created.Settings.Snippets))
	}
	snippet := created.Settings.Snippets[0]
	if snippet.ID == "" || snippet.Name != "日志查看" {
		t.Fatalf("unexpected snippet identity: %#v", snippet)
	}
	if snippet.Command != originalCommand {
		t.Fatalf("command was modified: %q", snippet.Command)
	}
	if len(snippet.Tags) != 2 || snippet.Tags[0] != "ops" || snippet.Tags[1] != "prod" {
		t.Fatalf("tags were not normalized: %#v", snippet.Tags)
	}

	if _, err := app.CreateSnippet(model.CommandSnippet{Name: "日志查看", Command: "pwd"}); err == nil {
		t.Fatal("expected duplicate snippet name error")
	}

	snippet.Name = "日志尾部"
	snippet.Command = "tail -f /var/log/app.log"
	updated, err := app.UpdateSnippet(snippet)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Settings.Snippets) != 1 || updated.Settings.Snippets[0].Name != "日志尾部" {
		t.Fatalf("snippet update failed: %#v", updated.Settings.Snippets)
	}

	deleted, err := app.DeleteSnippet(snippet.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted.Settings.Snippets) != 0 {
		t.Fatalf("snippet delete failed: %#v", deleted.Settings.Snippets)
	}
}

func TestCommandSnippetValidation(t *testing.T) {
	settings := model.DefaultSettings()
	settings.Snippets = []model.CommandSnippet{{ID: "snippet_1", Name: "pwd", Command: "pwd"}}
	if err := settings.Validate(); err != nil {
		t.Fatalf("valid snippet rejected: %v", err)
	}

	settings.Snippets = append(settings.Snippets, model.CommandSnippet{ID: "snippet_2", Name: "PWD", Command: "pwd"})
	if err := settings.Validate(); err == nil {
		t.Fatal("expected duplicate snippet name validation error")
	}

	settings = model.DefaultSettings()
	settings.Snippets = []model.CommandSnippet{{ID: "snippet_multi", Name: "multi", Command: "echo one\necho two"}}
	if err := settings.Validate(); err == nil {
		t.Fatal("expected multiline snippet validation error")
	}
}

func TestTerminalExportFilename(t *testing.T) {
	cases := map[string]string{
		"":                    "ssh-terminal.txt",
		"prod":                "prod.txt",
		"prod.txt":            "prod.txt",
		"prod:root/console*1": "prod_root_console_1.txt",
		"  .  ":               "ssh-terminal.txt",
	}
	for input, want := range cases {
		if got := terminalExportFilename(input); got != want {
			t.Fatalf("terminalExportFilename(%q) = %q, want %q", input, got, want)
		}
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
