package model

import (
	"errors"
	"fmt"
	"strings"

	kittheme "github.com/wanstu/wails-desktop-kit/theme"
)

const CurrentSettingsVersion = 1

type ThemeSettings struct {
	Mode    string `json:"mode"`
	Variant string `json:"variant"`
}

type ConnectionGroup struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Order int    `json:"order"`
}

type CommandSnippet struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Tags    []string `json:"tags,omitempty"`
}

type AuthConfig struct {
	Mode           string `json:"mode"`
	PrivateKeyPath string `json:"private_key_path,omitempty"`
	CredentialRef  string `json:"credential_ref,omitempty"`
}

type NetworkConfig struct {
	Mode          string `json:"mode"`
	JumpProfileID string `json:"jump_profile_id,omitempty"`
	SOCKS5Host    string `json:"socks5_host,omitempty"`
	SOCKS5Port    int    `json:"socks5_port,omitempty"`
	TimeoutSec    int    `json:"timeout_sec"`
	KeepaliveSec  int    `json:"keepalive_sec"`
}

type TerminalConfig struct {
	Term            string `json:"term"`
	Encoding        string `json:"encoding"`
	FontFamily      string `json:"font_family"`
	FontSize        int    `json:"font_size"`
	ColorScheme     string `json:"color_scheme"`
	ScrollbackLines int    `json:"scrollback_lines"`
	CursorStyle     string `json:"cursor_style"`
}

type ReconnectConfig struct {
	Enabled             bool `json:"enabled"`
	KeepTabOnDisconnect bool `json:"keep_tab_on_disconnect"`
}

type SourceInfo struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref,omitempty"`
}

type ConnectionProfile struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Host      string          `json:"host"`
	Port      int             `json:"port"`
	Username  string          `json:"username"`
	GroupID   string          `json:"group_id,omitempty"`
	Tags      []string        `json:"tags,omitempty"`
	Favorite  bool            `json:"favorite"`
	Auth      AuthConfig      `json:"auth"`
	Network   NetworkConfig   `json:"network"`
	Terminal  TerminalConfig  `json:"terminal"`
	Reconnect ReconnectConfig `json:"reconnect"`
	Source    SourceInfo      `json:"source"`
}

type Settings struct {
	Version  int                 `json:"version"`
	Theme    ThemeSettings       `json:"theme"`
	Groups   []ConnectionGroup   `json:"groups"`
	Profiles []ConnectionProfile `json:"profiles"`
	Snippets []CommandSnippet    `json:"snippets,omitempty"`
}

func DefaultSettings() Settings {
	return Settings{
		Version: CurrentSettingsVersion,
		Theme:   ThemeSettings{Mode: "dark", Variant: "aurora"},
	}
}

func DefaultProfile() ConnectionProfile {
	return ConnectionProfile{
		Port:    22,
		Auth:    AuthConfig{Mode: "auto"},
		Network: NetworkConfig{Mode: "direct", TimeoutSec: 10, KeepaliveSec: 30},
		Terminal: TerminalConfig{
			Term: "xterm-256color", Encoding: "UTF-8", FontFamily: "Cascadia Code", FontSize: 13,
			ColorScheme: "midnight", ScrollbackLines: 10000, CursorStyle: "bar",
		},
		Reconnect: ReconnectConfig{Enabled: true, KeepTabOnDisconnect: true},
		Source:    SourceInfo{Kind: "manual"},
	}
}

func (s Settings) Validate() error {
	if s.Version != CurrentSettingsVersion {
		return fmt.Errorf("unsupported settings version %d", s.Version)
	}
	if err := kittheme.ValidateMode(kittheme.Mode(s.Theme.Mode)); err != nil {
		return fmt.Errorf("invalid theme mode %q: %w", s.Theme.Mode, err)
	}
	if err := kittheme.ValidatePackName(s.Theme.Variant); err != nil {
		return fmt.Errorf("invalid theme pack %q: %w", s.Theme.Variant, err)
	}
	groupIDs := make(map[string]struct{}, len(s.Groups))
	for _, group := range s.Groups {
		if strings.TrimSpace(group.ID) == "" || strings.TrimSpace(group.Name) == "" {
			return errors.New("group id and name are required")
		}
		if _, ok := groupIDs[group.ID]; ok {
			return fmt.Errorf("duplicate group id %q", group.ID)
		}
		groupIDs[group.ID] = struct{}{}
	}
	profileIDs := make(map[string]struct{}, len(s.Profiles))
	profilesByID := make(map[string]ConnectionProfile, len(s.Profiles))
	for _, profile := range s.Profiles {
		if err := profile.Validate(); err != nil {
			return fmt.Errorf("profile %q: %w", profile.Name, err)
		}
		if _, ok := profileIDs[profile.ID]; ok {
			return fmt.Errorf("duplicate profile id %q", profile.ID)
		}
		profileIDs[profile.ID] = struct{}{}
		profilesByID[profile.ID] = profile
		if profile.GroupID != "" {
			if _, ok := groupIDs[profile.GroupID]; !ok {
				return fmt.Errorf("profile %q references unknown group %q", profile.Name, profile.GroupID)
			}
		}
	}
	for _, profile := range s.Profiles {
		if profile.Network.Mode == "jump_host" {
			if profile.Network.JumpProfileID == profile.ID {
				return fmt.Errorf("profile %q cannot jump through itself", profile.Name)
			}
			jump, ok := profilesByID[profile.Network.JumpProfileID]
			if !ok {
				return fmt.Errorf("profile %q references unknown jump profile %q", profile.Name, profile.Network.JumpProfileID)
			}
			if jump.Network.Mode != "direct" {
				return fmt.Errorf("profile %q jump host %q must use direct network mode", profile.Name, jump.Name)
			}
		}
	}

	snippetIDs := make(map[string]struct{}, len(s.Snippets))
	snippetNames := make(map[string]struct{}, len(s.Snippets))
	for _, snippet := range s.Snippets {
		if err := snippet.Validate(); err != nil {
			return fmt.Errorf("snippet %q: %w", snippet.Name, err)
		}
		if _, ok := snippetIDs[snippet.ID]; ok {
			return fmt.Errorf("duplicate snippet id %q", snippet.ID)
		}
		snippetIDs[snippet.ID] = struct{}{}
		nameKey := strings.ToLower(strings.TrimSpace(snippet.Name))
		if _, ok := snippetNames[nameKey]; ok {
			return fmt.Errorf("duplicate snippet name %q", snippet.Name)
		}
		snippetNames[nameKey] = struct{}{}
	}
	return nil
}

func (s CommandSnippet) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return errors.New("id is required")
	}
	name := strings.TrimSpace(s.Name)
	if name == "" {
		return errors.New("name is required")
	}
	if len([]rune(name)) > 80 {
		return errors.New("name is too long")
	}
	if strings.TrimSpace(s.Command) == "" {
		return errors.New("command is required")
	}
	for _, r := range s.Command {
		if r < 0x20 || r == 0x7f {
			return errors.New("command cannot contain control characters")
		}
	}
	if len(s.Command) > 16*1024 {
		return errors.New("command is too long")
	}
	if len(s.Tags) > 20 {
		return errors.New("too many tags")
	}
	for _, tag := range s.Tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			return errors.New("tag cannot be empty")
		}
		if len([]rune(tag)) > 40 {
			return fmt.Errorf("tag %q is too long", tag)
		}
	}
	return nil
}

func (p ConnectionProfile) Validate() error {
	if strings.TrimSpace(p.ID) == "" {
		return errors.New("id is required")
	}
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("name is required")
	}
	if strings.TrimSpace(p.Host) == "" {
		return errors.New("host is required")
	}
	if p.Port < 1 || p.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	if strings.TrimSpace(p.Username) == "" {
		return errors.New("username is required")
	}
	switch p.Auth.Mode {
	case "auto", "password", "private_key", "ssh_agent":
	default:
		return fmt.Errorf("invalid auth mode %q", p.Auth.Mode)
	}
	switch p.Network.Mode {
	case "direct", "jump_host", "socks5":
	default:
		return fmt.Errorf("invalid network mode %q", p.Network.Mode)
	}
	if p.Network.Mode == "jump_host" && strings.TrimSpace(p.Network.JumpProfileID) == "" {
		return errors.New("jump host profile is required")
	}
	if p.Network.Mode == "socks5" {
		if strings.TrimSpace(p.Network.SOCKS5Host) == "" {
			return errors.New("SOCKS5 host is required")
		}
		if p.Network.SOCKS5Port < 1 || p.Network.SOCKS5Port > 65535 {
			return errors.New("SOCKS5 port must be between 1 and 65535")
		}
	}
	return nil
}
