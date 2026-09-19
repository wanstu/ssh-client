package sshclient

import "github.com/wanstu/ssh-client/internal/model"

const (
	EventSessionState  = "ssh:session-state"
	EventOutput        = "ssh:output"
	EventHostKey       = "ssh:host-key"
	EventSessionClosed = "ssh:session-closed"
)

type Credentials struct {
	Password   string `json:"password,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
}

type JumpConfig struct {
	ProfileID    string           `json:"profile_id,omitempty"`
	Name         string           `json:"name"`
	Host         string           `json:"host"`
	Port         int              `json:"port"`
	Username     string           `json:"username"`
	Auth         model.AuthConfig `json:"auth"`
	TimeoutSec   int              `json:"timeout_sec"`
	KeepaliveSec int              `json:"keepalive_sec"`
	Credentials  Credentials      `json:"credentials"`
}

type ConnectConfig struct {
	ProfileID    string                `json:"profile_id,omitempty"`
	Name         string                `json:"name"`
	Host         string                `json:"host"`
	Port         int                   `json:"port"`
	Username     string                `json:"username"`
	Auth         model.AuthConfig      `json:"auth"`
	Terminal     model.TerminalConfig  `json:"terminal"`
	Reconnect    model.ReconnectConfig `json:"reconnect"`
	TimeoutSec   int                   `json:"timeout_sec"`
	KeepaliveSec int                   `json:"keepalive_sec"`
	Credentials  Credentials           `json:"credentials"`
	Jump         *JumpConfig           `json:"jump,omitempty"`
}

type SessionSnapshot struct {
	ID               string `json:"id"`
	ProfileID        string `json:"profile_id,omitempty"`
	Name             string `json:"name"`
	Target           string `json:"target"`
	State            string `json:"state"`
	Stage            string `json:"stage,omitempty"`
	Message          string `json:"message,omitempty"`
	ReasonCode       string `json:"reason_code,omitempty"`
	TechnicalDetail  string `json:"technical_detail,omitempty"`
	ReconnectAttempt int    `json:"reconnect_attempt"`
	NextRetrySeconds int    `json:"next_retry_seconds,omitempty"`
	StartedAtUnixMs  int64  `json:"started_at_unix_ms"`
	ConnectedAtUnixMs int64 `json:"connected_at_unix_ms,omitempty"`
	DisconnectedAtUnixMs int64 `json:"disconnected_at_unix_ms,omitempty"`
}

type OutputEvent struct {
	SessionID  string `json:"session_id"`
	DataBase64 string `json:"data_base64"`
}

type HostKeyChallenge struct {
	ID                  string `json:"id"`
	SessionID           string `json:"session_id"`
	Kind                string `json:"kind"`
	Scope               string `json:"scope"`
	Name                string `json:"name"`
	Host                string `json:"host"`
	Port                int    `json:"port"`
	Address             string `json:"address"`
	Algorithm           string `json:"algorithm"`
	Fingerprint         string `json:"fingerprint"`
	PreviousFingerprint string `json:"previous_fingerprint,omitempty"`
}

type EmitFunc func(event string, payload any)
