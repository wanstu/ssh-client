package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wanstu/ssh-client/internal/model"
)

func TestStoreRoundTrip(t *testing.T) {
	store := NewStoreAt(t.TempDir())
	settings := model.DefaultSettings()
	settings.Groups = append(settings.Groups, model.ConnectionGroup{ID: "group_prod", Name: "生产环境"})
	profile := model.DefaultProfile()
	profile.ID = "profile_web"
	profile.Name = "prod-web-01"
	profile.Host = "10.10.12.18"
	profile.Username = "root"
	profile.GroupID = "group_prod"
	profile.Auth.Mode = "private_key"
	profile.Auth.PrivateKeyPath = "~/.ssh/id_ed25519"
	settings.Profiles = append(settings.Profiles, profile)

	if err := store.Save(settings); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Profiles) != 1 || got.Profiles[0].Name != profile.Name {
		t.Fatalf("unexpected profiles: %#v", got.Profiles)
	}
	data, err := os.ReadFile(filepath.Join(store.Dir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret-value") {
		t.Fatal("settings unexpectedly persisted password material")
	}
}

func TestStoreRejectsUnknownJumpProfile(t *testing.T) {
	store := NewStoreAt(t.TempDir())
	settings := model.DefaultSettings()
	profile := model.DefaultProfile()
	profile.ID = "target"
	profile.Name = "target"
	profile.Host = "example.test"
	profile.Username = "root"
	profile.Network.Mode = "jump_host"
	profile.Network.JumpProfileID = "missing"
	settings.Profiles = []model.ConnectionProfile{profile}
	if err := store.Save(settings); err == nil {
		t.Fatal("expected invalid jump profile error")
	}
}

func TestMigrateLegacyFilesCopiesWithoutOverwrite(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()

	if err := os.WriteFile(filepath.Join(source, "settings.json"), []byte("legacy-settings"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "known_hosts.json"), []byte("legacy-hosts"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "settings.json"), []byte("new-settings"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := migrateLegacyFiles(source, target, []string{"settings.json", "known_hosts.json"}); err != nil {
		t.Fatal(err)
	}

	settings, err := os.ReadFile(filepath.Join(target, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(settings) != "new-settings" {
		t.Fatalf("existing target was overwritten: %q", settings)
	}

	hosts, err := os.ReadFile(filepath.Join(target, "known_hosts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(hosts) != "legacy-hosts" {
		t.Fatalf("legacy known_hosts was not migrated: %q", hosts)
	}
}
