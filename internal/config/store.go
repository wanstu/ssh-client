package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wanstu/ssh-client/internal/model"
	"github.com/wanstu/wails-desktop-kit/jsonstore"
	kitpaths "github.com/wanstu/wails-desktop-kit/paths"
)

type Store struct {
	dir  string
	path string
}

func NewStore() (*Store, error) {
	dir, err := kitpaths.EnsureConfigDir("ssh-client")
	if err != nil {
		return nil, err
	}
	if err := migrateLegacyConfig(dir); err != nil {
		return nil, err
	}
	return NewStoreAt(dir), nil
}

func migrateLegacyConfig(targetDir string) error {
	base, err := os.UserConfigDir()
	if err != nil {
		return nil
	}
	legacyDir := filepath.Join(base, "ssh-client")
	if kitpaths.SamePath(legacyDir, targetDir) {
		return nil
	}
	return migrateLegacyFiles(legacyDir, targetDir, []string{"settings.json", "known_hosts.json"})
}

func migrateLegacyFiles(sourceDir, targetDir string, names []string) error {
	if _, err := kitpaths.MigrateFilesIfMissing(sourceDir, targetDir, names...); err != nil {
		return fmt.Errorf("migrate legacy config: %w", err)
	}
	return nil
}

func NewStoreAt(dir string) *Store {
	return &Store{dir: dir, path: filepath.Join(dir, "settings.json")}
}

func (s *Store) Dir() string { return s.dir }

func (s *Store) values() *jsonstore.Store[model.Settings] {
	return jsonstore.New(s.path, jsonstore.Options[model.Settings]{
		Default: model.DefaultSettings,
		Validate: func(settings model.Settings) error {
			return settings.Validate()
		},
	})
}

func (s *Store) Load() (model.Settings, error) {
	settings, err := s.values().Load()
	if err != nil {
		return model.Settings{}, fmt.Errorf("load settings: %w", err)
	}
	return settings, nil
}

func (s *Store) Save(settings model.Settings) error {
	if err := s.values().Save(settings); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	return nil
}

func (s *Store) Update(fn func(*model.Settings) error) (model.Settings, error) {
	settings, err := s.values().Update(fn)
	if err != nil {
		return model.Settings{}, err
	}
	return settings, nil
}

func (s *Store) NewID(prefix string) string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err)
	}
	prefix = strings.Trim(strings.TrimSpace(prefix), "-_")
	if prefix == "" {
		prefix = "id"
	}
	return prefix + "_" + hex.EncodeToString(raw[:])
}
