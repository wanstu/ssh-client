package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wanstu/ssh-client/internal/model"
	kitpaths "github.com/wanstu/wails-desktop-kit/paths"
)

type Store struct {
	mu   sync.Mutex
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
	if samePath(legacyDir, targetDir) {
		return nil
	}
	return migrateLegacyFiles(legacyDir, targetDir, []string{"settings.json", "known_hosts.json"})
}

func migrateLegacyFiles(sourceDir, targetDir string, names []string) error {
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		return fmt.Errorf("create target config dir: %w", err)
	}
	for _, name := range names {
		source := filepath.Join(sourceDir, name)
		target := filepath.Join(targetDir, name)
		if _, err := os.Stat(target); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect target config %s: %w", name, err)
		}
		info, err := os.Stat(source)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect legacy config %s: %w", name, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if err := copyFileAtomic(source, target); err != nil {
			return fmt.Errorf("migrate legacy config %s: %w", name, err)
		}
	}
	return nil
}

func copyFileAtomic(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()

	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".migrate-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, target)
}

func samePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return strings.EqualFold(filepath.Clean(aa), filepath.Clean(bb))
}

func NewStoreAt(dir string) *Store {
	return &Store{dir: dir, path: filepath.Join(dir, "settings.json")}
}
func (s *Store) Dir() string { return s.dir }

func (s *Store) Load() (model.Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *Store) Save(settings model.Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(settings)
}

func (s *Store) Update(fn func(*model.Settings) error) (model.Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	settings, err := s.loadLocked()
	if err != nil {
		return model.Settings{}, err
	}
	if err := fn(&settings); err != nil {
		return model.Settings{}, err
	}
	if err := s.saveLocked(settings); err != nil {
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

func (s *Store) loadLocked() (model.Settings, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return model.DefaultSettings(), nil
	}
	if err != nil {
		return model.Settings{}, fmt.Errorf("read settings: %w", err)
	}
	var settings model.Settings
	if err := json.Unmarshal(data, &settings); err != nil {
		return model.Settings{}, fmt.Errorf("decode settings: %w", err)
	}
	if err := settings.Validate(); err != nil {
		return model.Settings{}, fmt.Errorf("validate settings: %w", err)
	}
	return settings, nil
}

func (s *Store) saveLocked(settings model.Settings) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(s.dir, "settings-*.tmp")
	if err != nil {
		return fmt.Errorf("create settings temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("protect settings temp file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write settings temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync settings temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close settings temp file: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("replace settings: %w", err)
	}
	return nil
}
