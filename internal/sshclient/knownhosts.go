package sshclient

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type KnownHostRecord struct {
	Address     string    `json:"address"`
	Algorithm   string    `json:"algorithm"`
	PublicKey   string    `json:"public_key"`
	Fingerprint string    `json:"fingerprint"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
}

type KnownHostStore struct {
	mu   sync.Mutex
	path string
}

func NewKnownHostStore(dir string) *KnownHostStore {
	return &KnownHostStore{path: filepath.Join(dir, "known_hosts.json")}
}

func (s *KnownHostStore) Check(address string, key ssh.PublicKey) (string, *KnownHostRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.loadLocked()
	if err != nil {
		return "", nil, err
	}
	record, ok := records[address]
	if !ok {
		return "unknown", nil, nil
	}
	encoded := base64.StdEncoding.EncodeToString(key.Marshal())
	if record.Algorithm == key.Type() && record.PublicKey == encoded {
		copy := record
		return "match", &copy, nil
	}
	copy := record
	return "changed", &copy, nil
}

func (s *KnownHostStore) Trust(address string, key ssh.PublicKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.loadLocked()
	if err != nil {
		return err
	}
	now := time.Now()
	firstSeen := now
	if existing, ok := records[address]; ok {
		firstSeen = existing.FirstSeen
	}
	records[address] = KnownHostRecord{
		Address: address, Algorithm: key.Type(),
		PublicKey:   base64.StdEncoding.EncodeToString(key.Marshal()),
		Fingerprint: ssh.FingerprintSHA256(key), FirstSeen: firstSeen, LastSeen: now,
	}
	return s.saveLocked(records)
}

func (s *KnownHostStore) loadLocked() (map[string]KnownHostRecord, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]KnownHostRecord{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read known hosts: %w", err)
	}
	records := map[string]KnownHostRecord{}
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("decode known hosts: %w", err)
	}
	return records, nil
}

func (s *KnownHostStore) saveLocked(records map[string]KnownHostRecord) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create known hosts dir: %w", err)
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("encode known hosts: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, "known-hosts-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
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
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("replace known hosts: %w", err)
	}
	return nil
}
