package sshclient

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"time"

	"github.com/wanstu/wails-desktop-kit/jsonstore"
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
	values *jsonstore.Store[map[string]KnownHostRecord]
}

func NewKnownHostStore(dir string) *KnownHostStore {
	return &KnownHostStore{
		values: jsonstore.New(filepath.Join(dir, "known_hosts.json"), jsonstore.Options[map[string]KnownHostRecord]{
			Default: func() map[string]KnownHostRecord { return map[string]KnownHostRecord{} },
			Normalize: func(records *map[string]KnownHostRecord) {
				if *records == nil {
					*records = map[string]KnownHostRecord{}
				}
			},
		}),
	}
}

func (s *KnownHostStore) Check(address string, key ssh.PublicKey) (string, *KnownHostRecord, error) {
	records, err := s.values.Load()
	if err != nil {
		return "", nil, fmt.Errorf("read known hosts: %w", err)
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
	_, err := s.values.Update(func(records *map[string]KnownHostRecord) error {
		now := time.Now()
		firstSeen := now
		if existing, ok := (*records)[address]; ok {
			firstSeen = existing.FirstSeen
		}
		(*records)[address] = KnownHostRecord{
			Address:     address,
			Algorithm:   key.Type(),
			PublicKey:   base64.StdEncoding.EncodeToString(key.Marshal()),
			Fingerprint: ssh.FingerprintSHA256(key),
			FirstSeen:   firstSeen,
			LastSeen:    now,
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("save known hosts: %w", err)
	}
	return nil
}
