package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"sort"
	"strings"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/wanstu/ssh-client/internal/config"
	"github.com/wanstu/ssh-client/internal/model"
	"github.com/wanstu/ssh-client/internal/sshclient"
	"github.com/wanstu/ssh-client/internal/sshconfig"
	desktopkit "github.com/wanstu/wails-desktop-kit"
	kitautostart "github.com/wanstu/wails-desktop-kit/autostart"
	"github.com/wanstu/wails-desktop-kit/secureconfig"
)

const (
	commandHistorySecureKey = "command-history-v1"
	commandHistoryLimit     = 1000
)

type CommandHistoryEntry struct {
	Command   string `json:"command"`
	SessionID string `json:"session_id,omitempty"`
	Target    string `json:"target,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

type UIState struct {
	Settings               model.Settings              `json:"settings"`
	Sessions               []sshclient.SessionSnapshot `json:"sessions"`
	CommandHistory         []CommandHistoryEntry       `json:"command_history"`
	CommandHistoryError    string                      `json:"command_history_error,omitempty"`
	LaunchAtLoginSupported bool                        `json:"launch_at_login_supported"`
	LaunchAtLogin          bool                        `json:"launch_at_login"`
	DataDir                string                      `json:"data_dir"`
}

type QuickConnectRequest struct {
	Name           string `json:"name"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Username       string `json:"username"`
	AuthMode       string `json:"auth_mode"`
	PrivateKeyPath string `json:"private_key_path"`
	Password       string `json:"password"`
	Passphrase     string `json:"passphrase"`
	SaveProfile    bool   `json:"save_profile"`
}

type pendingCredentialAction struct {
	ProfileID   string
	Password    string
	Remember    bool
	ExistingRef string
}

type App struct {
	store         *config.Store
	secure        *secureconfig.Store
	launchAtLogin *kitautostart.Manager
	sessions      *sshclient.Manager
	mu            sync.RWMutex
	controller    *desktopkit.Controller

	credentialMu      sync.Mutex
	pendingCredential map[string]pendingCredentialAction

	commandHistoryMu sync.Mutex
}

func NewApp() (*App, error) {
	store, err := config.NewStore()
	if err != nil {
		return nil, err
	}
	secure, err := secureconfig.New("ssh-client")
	if err != nil {
		return nil, err
	}
	launchAtLogin, err := kitautostart.New(kitautostart.Config{
		ID: "ssh-client", DisplayName: "SSH Client", Comment: "SSH desktop client", Arguments: []string{"--autostart"},
	})
	if err != nil {
		return nil, err
	}
	app := &App{
		store:             store,
		secure:            secure,
		launchAtLogin:     launchAtLogin,
		pendingCredential: map[string]pendingCredentialAction{},
	}
	app.sessions = sshclient.NewManager(store.Dir(), app.emit)
	return app, nil
}

func (a *App) setController(controller *desktopkit.Controller) {
	a.mu.Lock()
	a.controller = controller
	a.mu.Unlock()
}

func (a *App) shutdown(context.Context) {
	a.sessions.CloseAll()
	a.mu.Lock()
	a.controller = nil
	a.mu.Unlock()
}

func (a *App) emit(event string, payload any) {
	if event == sshclient.EventSessionState {
		if snapshot, ok := payload.(sshclient.SessionSnapshot); ok {
			a.handleCredentialState(snapshot)
		}
	}

	a.mu.RLock()
	controller := a.controller
	a.mu.RUnlock()
	if controller != nil {
		_ = controller.Emit(event, payload)
	}
}

func (a *App) handleCredentialState(snapshot sshclient.SessionSnapshot) {
	if snapshot.State != "connected" && snapshot.State != "failed" &&
		snapshot.State != "security_blocked" && snapshot.State != "disconnected" {
		return
	}

	a.credentialMu.Lock()
	pending, ok := a.pendingCredential[snapshot.ID]
	if ok {
		delete(a.pendingCredential, snapshot.ID)
	}
	a.credentialMu.Unlock()
	if !ok {
		return
	}
	if snapshot.State != "connected" {
		return
	}

	if pending.Remember {
		ref := passwordCredentialRef(pending.ProfileID)
		if err := a.secure.Put(ref, []byte(pending.Password)); err != nil {
			a.emitAppEvent("app:credential-save-error", map[string]string{
				"profile_id": pending.ProfileID,
				"message":    "SSH 已连接，但密码未能保存到系统安全存储：" + err.Error(),
			})
			return
		}
		settings, err := a.store.Update(func(settings *model.Settings) error {
			for i := range settings.Profiles {
				if settings.Profiles[i].ID == pending.ProfileID {
					settings.Profiles[i].Auth.CredentialRef = ref
					return nil
				}
			}
			return fmt.Errorf("连接 %q 不存在", pending.ProfileID)
		})
		if err != nil {
			_ = a.secure.Delete(ref)
			a.emitAppEvent("app:credential-save-error", map[string]string{
				"profile_id": pending.ProfileID,
				"message":    "SSH 已连接，但保存密码引用失败：" + err.Error(),
			})
			return
		}
		a.emitAppEvent("app:settings-changed", settings)
		return
	}

	if pending.ExistingRef != "" {
		_ = a.secure.Delete(pending.ExistingRef)
		settings, err := a.store.Update(func(settings *model.Settings) error {
			for i := range settings.Profiles {
				if settings.Profiles[i].ID == pending.ProfileID {
					settings.Profiles[i].Auth.CredentialRef = ""
					return nil
				}
			}
			return nil
		})
		if err == nil {
			a.emitAppEvent("app:settings-changed", settings)
		}
	}
}

func (a *App) emitAppEvent(event string, payload any) {
	a.mu.RLock()
	controller := a.controller
	a.mu.RUnlock()
	if controller != nil {
		_ = controller.Emit(event, payload)
	}
}

func (a *App) GetState() (UIState, error) {
	settings, err := a.store.Load()
	if err != nil {
		return UIState{}, err
	}
	enabled, err := a.launchAtLogin.Enabled()
	if err != nil {
		return UIState{}, err
	}

	commandHistory, historyErr := a.GetCommandHistory()
	state := UIState{
		Settings:               settings,
		Sessions:               a.sessions.Sessions(),
		CommandHistory:         commandHistory,
		LaunchAtLoginSupported: a.launchAtLogin.Supported(),
		LaunchAtLogin:          enabled,
		DataDir:                a.store.Dir(),
	}
	if historyErr != nil {
		state.CommandHistoryError = historyErr.Error()
	}
	return state, nil
}

func (a *App) GetCommandHistory() ([]CommandHistoryEntry, error) {
	a.commandHistoryMu.Lock()
	defer a.commandHistoryMu.Unlock()
	return a.loadCommandHistoryLocked()
}

func (a *App) RecordCommandHistory(entry CommandHistoryEntry) ([]CommandHistoryEntry, error) {
	entry.Command = strings.TrimSpace(entry.Command)
	entry.SessionID = strings.TrimSpace(entry.SessionID)
	entry.Target = strings.TrimSpace(entry.Target)
	if entry.Command == "" {
		return nil, errors.New("命令不能为空")
	}
	if len(entry.Command) > 64*1024 {
		return nil, errors.New("命令过长，不能写入历史记录")
	}
	if entry.CreatedAt <= 0 {
		entry.CreatedAt = time.Now().UnixMilli()
	}

	a.commandHistoryMu.Lock()
	defer a.commandHistoryMu.Unlock()

	history, err := a.loadCommandHistoryLocked()
	if err != nil {
		return nil, err
	}
	history = append(history, entry)
	sort.SliceStable(history, func(i, j int) bool {
		return history[i].CreatedAt > history[j].CreatedAt
	})
	if len(history) > commandHistoryLimit {
		history = history[:commandHistoryLimit]
	}
	if err := a.secure.SaveJSON(commandHistorySecureKey, history); err != nil {
		return nil, fmt.Errorf("保存加密命令历史失败: %w", err)
	}
	return history, nil
}

func (a *App) ClearCommandHistory() error {
	a.commandHistoryMu.Lock()
	defer a.commandHistoryMu.Unlock()

	err := a.secure.Delete(commandHistorySecureKey)
	if errors.Is(err, secureconfig.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("清空加密命令历史失败: %w", err)
	}
	return nil
}

func (a *App) loadCommandHistoryLocked() ([]CommandHistoryEntry, error) {
	var history []CommandHistoryEntry
	if err := a.secure.LoadJSON(commandHistorySecureKey, &history); err != nil {
		if errors.Is(err, secureconfig.ErrNotFound) {
			return []CommandHistoryEntry{}, nil
		}
		return nil, fmt.Errorf("读取加密命令历史失败: %w", err)
	}

	sort.SliceStable(history, func(i, j int) bool {
		return history[i].CreatedAt > history[j].CreatedAt
	})
	if len(history) > commandHistoryLimit {
		history = history[:commandHistoryLimit]
	}
	return history, nil
}

func (a *App) SetLaunchAtLogin(value bool) (UIState, error) {
	if value && !a.launchAtLogin.Supported() {
		return UIState{}, errors.New("当前平台不支持开机启动")
	}
	if err := a.launchAtLogin.SetEnabled(value); err != nil {
		return UIState{}, err
	}
	return a.GetState()
}

func (a *App) SetTheme(themeSetting model.ThemeSettings) (UIState, error) {
	_, err := a.store.Update(func(settings *model.Settings) error {
		settings.Theme = themeSetting
		return settings.Validate()
	})
	if err != nil {
		return UIState{}, err
	}
	return a.GetState()
}

func (a *App) CreateGroup(name string) (UIState, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return UIState{}, errors.New("分组名称不能为空")
	}
	_, err := a.store.Update(func(settings *model.Settings) error {
		for _, group := range settings.Groups {
			if strings.EqualFold(group.Name, name) {
				return fmt.Errorf("分组 %q 已存在", name)
			}
		}
		settings.Groups = append(settings.Groups, model.ConnectionGroup{ID: a.store.NewID("group"), Name: name, Order: len(settings.Groups) + 1})
		return nil
	})
	if err != nil {
		return UIState{}, err
	}
	return a.GetState()
}

func (a *App) RenameGroup(id, name string) (UIState, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return UIState{}, errors.New("分组名称不能为空")
	}
	_, err := a.store.Update(func(settings *model.Settings) error {
		found := false
		for i := range settings.Groups {
			if settings.Groups[i].ID == id {
				settings.Groups[i].Name = name
				found = true
				continue
			}
			if strings.EqualFold(settings.Groups[i].Name, name) {
				return fmt.Errorf("分组 %q 已存在", name)
			}
		}
		if !found {
			return fmt.Errorf("分组 %q 不存在", id)
		}
		return nil
	})
	if err != nil {
		return UIState{}, err
	}
	return a.GetState()
}

func (a *App) DeleteGroup(id string) (UIState, error) {
	_, err := a.store.Update(func(settings *model.Settings) error {
		found := false
		next := settings.Groups[:0]
		for _, group := range settings.Groups {
			if group.ID == id {
				found = true
				continue
			}
			next = append(next, group)
		}
		if !found {
			return fmt.Errorf("分组 %q 不存在", id)
		}
		settings.Groups = next
		for i := range settings.Profiles {
			if settings.Profiles[i].GroupID == id {
				settings.Profiles[i].GroupID = ""
			}
		}
		return nil
	})
	if err != nil {
		return UIState{}, err
	}
	return a.GetState()
}

func (a *App) CreateProfile(profile model.ConnectionProfile) (UIState, error) {
	profile = normalizeProfile(profile)
	profile.ID = a.store.NewID("profile")
	_, err := a.store.Update(func(settings *model.Settings) error {
		for _, existing := range settings.Profiles {
			if strings.EqualFold(existing.Name, profile.Name) {
				return fmt.Errorf("连接名称 %q 已存在", profile.Name)
			}
		}
		settings.Profiles = append(settings.Profiles, profile)
		return nil
	})
	if err != nil {
		return UIState{}, err
	}
	return a.GetState()
}

func (a *App) UpdateProfile(profile model.ConnectionProfile) (UIState, error) {
	profile = normalizeProfile(profile)
	if profile.ID == "" {
		return UIState{}, errors.New("连接 ID 不能为空")
	}

	settings, err := a.store.Load()
	if err != nil {
		return UIState{}, err
	}
	existingProfile, err := findProfile(settings, profile.ID)
	if err != nil {
		return UIState{}, err
	}
	if profile.Auth.Mode != "password" {
		profile.Auth.CredentialRef = ""
	} else if profile.Auth.CredentialRef == "" {
		profile.Auth.CredentialRef = existingProfile.Auth.CredentialRef
	}

	_, err = a.store.Update(func(settings *model.Settings) error {
		found := false
		for i, existing := range settings.Profiles {
			if existing.ID == profile.ID {
				settings.Profiles[i] = profile
				found = true
				continue
			}
			if strings.EqualFold(existing.Name, profile.Name) {
				return fmt.Errorf("连接名称 %q 已存在", profile.Name)
			}
		}
		if !found {
			return fmt.Errorf("连接 %q 不存在", profile.ID)
		}
		return nil
	})
	if err != nil {
		return UIState{}, err
	}
	if existingProfile.Auth.CredentialRef != "" && profile.Auth.Mode != "password" {
		_ = a.secure.Delete(existingProfile.Auth.CredentialRef)
	}
	return a.GetState()
}

func (a *App) DuplicateProfile(id string) (UIState, error) {
	settings, err := a.store.Load()
	if err != nil {
		return UIState{}, err
	}
	profile, err := findProfile(settings, id)
	if err != nil {
		return UIState{}, err
	}
	profile.ID = ""
	profile.Name += " 副本"
	profile.Auth.CredentialRef = ""
	profile.Source = model.SourceInfo{Kind: "manual"}
	return a.CreateProfile(profile)
}

func (a *App) DeleteProfile(id string) (UIState, error) {
	current, err := a.store.Load()
	if err != nil {
		return UIState{}, err
	}
	profileToDelete, err := findProfile(current, id)
	if err != nil {
		return UIState{}, err
	}

	_, err = a.store.Update(func(settings *model.Settings) error {
		for _, profile := range settings.Profiles {
			if profile.Network.Mode == "jump_host" && profile.Network.JumpProfileID == id {
				return fmt.Errorf("连接 %q 正被 %q 用作 Jump Host", id, profile.Name)
			}
		}
		found := false
		next := settings.Profiles[:0]
		for _, profile := range settings.Profiles {
			if profile.ID == id {
				found = true
				continue
			}
			next = append(next, profile)
		}
		if !found {
			return fmt.Errorf("连接 %q 不存在", id)
		}
		settings.Profiles = next
		return nil
	})
	if err != nil {
		return UIState{}, err
	}
	if profileToDelete.Auth.CredentialRef != "" {
		_ = a.secure.Delete(profileToDelete.Auth.CredentialRef)
	}
	return a.GetState()
}

func (a *App) MoveProfiles(ids []string, groupID string) (UIState, error) {
	wanted := map[string]struct{}{}
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	_, err := a.store.Update(func(settings *model.Settings) error {
		if groupID != "" {
			found := false
			for _, group := range settings.Groups {
				if group.ID == groupID {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("分组 %q 不存在", groupID)
			}
		}
		for i := range settings.Profiles {
			if _, ok := wanted[settings.Profiles[i].ID]; ok {
				settings.Profiles[i].GroupID = groupID
			}
		}
		return nil
	})
	if err != nil {
		return UIState{}, err
	}
	return a.GetState()
}

func (a *App) ConnectProfile(id string, credentials sshclient.Credentials, rememberPassword bool) (sshclient.SessionSnapshot, error) {
	settings, err := a.store.Load()
	if err != nil {
		return sshclient.SessionSnapshot{}, err
	}
	profile, err := findProfile(settings, id)
	if err != nil {
		return sshclient.SessionSnapshot{}, err
	}

	providedPassword := credentials.Password != ""
	if profile.Auth.Mode == "password" && credentials.Password == "" && profile.Auth.CredentialRef != "" {
		saved, err := a.secure.Get(profile.Auth.CredentialRef)
		if err != nil {
			return sshclient.SessionSnapshot{}, fmt.Errorf("读取已保存密码失败，请重新输入：%w", err)
		}
		credentials.Password = string(saved)
	}

	session, err := a.connectProfile(profile, credentials)
	if err != nil {
		return sshclient.SessionSnapshot{}, err
	}
	if profile.Auth.Mode == "password" && providedPassword {
		a.credentialMu.Lock()
		a.pendingCredential[session.ID] = pendingCredentialAction{
			ProfileID:   profile.ID,
			Password:    credentials.Password,
			Remember:    rememberPassword,
			ExistingRef: profile.Auth.CredentialRef,
		}
		a.credentialMu.Unlock()

		for _, current := range a.sessions.Sessions() {
			if current.ID == session.ID && (current.State == "connected" || current.State == "failed" ||
				current.State == "security_blocked" || current.State == "disconnected") {
				a.handleCredentialState(current)
				break
			}
		}
	}
	return session, nil
}

func passwordCredentialRef(profileID string) string {
	return "profile/" + profileID + "/password"
}

func (a *App) ClearSavedCredential(profileID string) (UIState, error) {
	settings, err := a.store.Load()
	if err != nil {
		return UIState{}, err
	}
	profile, err := findProfile(settings, profileID)
	if err != nil {
		return UIState{}, err
	}
	if profile.Auth.CredentialRef != "" {
		if err := a.secure.Delete(profile.Auth.CredentialRef); err != nil && !errors.Is(err, secureconfig.ErrNotFound) {
			return UIState{}, err
		}
	}
	_, err = a.store.Update(func(settings *model.Settings) error {
		for i := range settings.Profiles {
			if settings.Profiles[i].ID == profileID {
				settings.Profiles[i].Auth.CredentialRef = ""
				return nil
			}
		}
		return fmt.Errorf("连接 %q 不存在", profileID)
	})
	if err != nil {
		return UIState{}, err
	}
	return a.GetState()
}

func (a *App) QuickConnect(req QuickConnectRequest) (sshclient.SessionSnapshot, error) {
	profile := model.DefaultProfile()
	profile.ID = ""
	profile.Name = strings.TrimSpace(req.Name)
	profile.Host = strings.TrimSpace(req.Host)
	profile.Port = req.Port
	profile.Username = strings.TrimSpace(req.Username)
	profile.Auth.Mode = strings.TrimSpace(req.AuthMode)
	profile.Auth.PrivateKeyPath = strings.TrimSpace(req.PrivateKeyPath)
	profile.Source = model.SourceInfo{Kind: "quick_saved"}
	profile = normalizeProfile(profile)
	// Quick Connect is session-first. If the user asked to save it, the
	// frontend creates the persistent profile only after the session reaches
	// connected, so failed attempts never leave half-created profiles.
	return a.connectProfile(profile, sshclient.Credentials{Password: req.Password, Passphrase: req.Passphrase})
}

func (a *App) connectProfile(profile model.ConnectionProfile, credentials sshclient.Credentials) (sshclient.SessionSnapshot, error) {
	if profile.Network.Mode != "direct" {
		return sshclient.SessionSnapshot{}, fmt.Errorf("%s 网络模式将在后续阶段接入；当前 Runtime 只允许 Direct", profile.Network.Mode)
	}
	return a.sessions.Connect(sshclient.ConnectConfig{
		ProfileID: profile.ID, Name: profile.Name, Host: profile.Host, Port: profile.Port, Username: profile.Username,
		Auth: profile.Auth, Terminal: profile.Terminal, Reconnect: profile.Reconnect,
		TimeoutSec: profile.Network.TimeoutSec, KeepaliveSec: profile.Network.KeepaliveSec, Credentials: credentials,
	})
}

func (a *App) WriteSession(id, data string) error { return a.sessions.Write(id, data) }
func (a *App) ResizeSession(id string, cols, rows int) error {
	return a.sessions.Resize(id, cols, rows)
}
func (a *App) RetrySession(id string) error           { return a.sessions.RetryNow(id) }
func (a *App) DisconnectSession(id string) error      { return a.sessions.Disconnect(id) }
func (a *App) CloseSession(id string) error           { return a.sessions.CloseSession(id) }
func (a *App) ResolveHostKey(id, action string) error { return a.sessions.ResolveHostKey(id, action) }
func (a *App) DisconnectAll()                         { a.sessions.CloseAll() }

func (a *App) ChoosePrivateKey() (string, error) {
	a.mu.RLock()
	controller := a.controller
	a.mu.RUnlock()
	if controller == nil {
		return "", errors.New("桌面运行时尚未就绪")
	}
	return controller.OpenFileDialog(wailsruntime.OpenDialogOptions{Title: "选择 SSH 私钥", Filters: []wailsruntime.FileFilter{{DisplayName: "SSH private key", Pattern: "*"}, {DisplayName: "All files", Pattern: "*"}}})
}

func (a *App) PreviewSSHConfig(path string) (sshconfig.Preview, error) {
	return sshconfig.PreviewFile(path)
}

func (a *App) ChooseSSHConfig() (string, error) {
	a.mu.RLock()
	controller := a.controller
	a.mu.RUnlock()
	if controller == nil {
		return "", errors.New("桌面运行时尚未就绪")
	}
	return controller.OpenFileDialog(wailsruntime.OpenDialogOptions{
		Title: "选择 SSH Config",
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "OpenSSH config", Pattern: "config;*.conf;*"},
		},
	})
}

func (a *App) ImportSSHConfig(entries []sshconfig.Entry) (UIState, error) {
	if len(entries) == 0 {
		return UIState{}, errors.New("请选择至少一个可导入的 Host")
	}

	_, err := a.store.Update(func(settings *model.Settings) error {
		username := currentUsername()
		profileRefs := make(map[string]string, len(settings.Profiles)+len(entries))
		names := make(map[string]struct{}, len(settings.Profiles)+len(entries))

		for _, profile := range settings.Profiles {
			names[strings.ToLower(profile.Name)] = struct{}{}
			profileRefs[strings.ToLower(profile.Name)] = profile.ID
			if profile.Source.Kind == "ssh_config" {
				if alias := sourceAlias(profile.Source.Ref); alias != "" {
					profileRefs[strings.ToLower(alias)] = profile.ID
				}
			}
		}

		newProfiles := make([]model.ConnectionProfile, 0, len(entries))
		aliasToID := make(map[string]string, len(entries))
		for _, entry := range entries {
			if !entry.Supported {
				return fmt.Errorf("Host %q 不能安全导入: %s", entry.Alias, entry.Warning)
			}
			name := strings.TrimSpace(entry.Alias)
			if name == "" {
				return errors.New("SSH Config Host alias 不能为空")
			}
			key := strings.ToLower(name)
			if _, exists := names[key]; exists {
				return fmt.Errorf("连接名称 %q 已存在；导入不会静默覆盖", name)
			}
			names[key] = struct{}{}
			id := a.store.NewID("profile")
			aliasToID[key] = id
			profileRefs[key] = id

			profile := model.DefaultProfile()
			profile.ID = id
			profile.Name = name
			profile.Host = strings.TrimSpace(entry.HostName)
			if profile.Host == "" {
				profile.Host = name
			}
			profile.Port = entry.Port
			if profile.Port == 0 {
				profile.Port = 22
			}
			profile.Username = strings.TrimSpace(entry.User)
			if profile.Username == "" {
				profile.Username = username
			}
			if strings.TrimSpace(entry.IdentityFile) != "" {
				profile.Auth.Mode = "private_key"
				profile.Auth.PrivateKeyPath = strings.TrimSpace(entry.IdentityFile)
			}
			profile.Source = model.SourceInfo{
				Kind: "ssh_config",
				Ref:  entry.SourcePath + "#" + name,
			}
			newProfiles = append(newProfiles, profile)
		}

		for i, entry := range entries {
			jump := proxyJumpAlias(entry.ProxyJump)
			if jump == "" {
				continue
			}
			jumpID, ok := profileRefs[strings.ToLower(jump)]
			if !ok {
				return fmt.Errorf("Host %q 引用了 ProxyJump %q，但该 Jump Host 未导入且连接库中不存在", entry.Alias, jump)
			}
			if jumpID == newProfiles[i].ID {
				return fmt.Errorf("Host %q 不能把自己作为 Jump Host", entry.Alias)
			}
			newProfiles[i].Network.Mode = "jump_host"
			newProfiles[i].Network.JumpProfileID = jumpID
		}

		settings.Profiles = append(settings.Profiles, newProfiles...)
		return nil
	})
	if err != nil {
		return UIState{}, err
	}
	return a.GetState()
}

func currentUsername() string {
	for _, key := range []string{"USERNAME", "USER"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	if current, err := user.Current(); err == nil {
		if value := strings.TrimSpace(current.Username); value != "" {
			if index := strings.LastIndexAny(value, `\\/`); index >= 0 && index+1 < len(value) {
				return value[index+1:]
			}
			return value
		}
	}
	return "user"
}

func sourceAlias(ref string) string {
	if index := strings.LastIndex(ref, "#"); index >= 0 && index+1 < len(ref) {
		return strings.TrimSpace(ref[index+1:])
	}
	return ""
}

func proxyJumpAlias(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "none") {
		return ""
	}
	if strings.Contains(value, ",") {
		return value
	}
	if at := strings.LastIndex(value, "@"); at >= 0 && at+1 < len(value) {
		value = value[at+1:]
	}
	if strings.HasPrefix(value, "[") {
		if end := strings.Index(value, "]"); end > 0 {
			return value[1:end]
		}
	}
	if colon := strings.LastIndex(value, ":"); colon > 0 && strings.Count(value, ":") == 1 {
		value = value[:colon]
	}
	return strings.TrimSpace(value)
}

func normalizeProfile(profile model.ConnectionProfile) model.ConnectionProfile {
	defaults := model.DefaultProfile()
	profile.Name = strings.TrimSpace(profile.Name)
	profile.Host = strings.TrimSpace(profile.Host)
	profile.Username = strings.TrimSpace(profile.Username)
	if profile.Port == 0 {
		profile.Port = defaults.Port
	}
	if profile.Auth.Mode == "" {
		profile.Auth.Mode = defaults.Auth.Mode
	}
	if profile.Network.Mode == "" {
		profile.Network.Mode = defaults.Network.Mode
	}
	if profile.Network.TimeoutSec <= 0 {
		profile.Network.TimeoutSec = defaults.Network.TimeoutSec
	}
	if profile.Network.KeepaliveSec < 0 {
		profile.Network.KeepaliveSec = 0
	}
	if profile.Terminal.Term == "" {
		profile.Terminal.Term = defaults.Terminal.Term
	}
	if profile.Terminal.Encoding == "" {
		profile.Terminal.Encoding = defaults.Terminal.Encoding
	}
	if profile.Terminal.FontFamily == "" {
		profile.Terminal.FontFamily = defaults.Terminal.FontFamily
	}
	if profile.Terminal.FontSize < 10 || profile.Terminal.FontSize > 28 {
		profile.Terminal.FontSize = defaults.Terminal.FontSize
	}
	switch strings.ToLower(strings.TrimSpace(profile.Terminal.ColorScheme)) {
	case "midnight", "graphite", "daylight":
		profile.Terminal.ColorScheme = strings.ToLower(strings.TrimSpace(profile.Terminal.ColorScheme))
	default:
		profile.Terminal.ColorScheme = defaults.Terminal.ColorScheme
	}
	if profile.Terminal.ScrollbackLines < 100 || profile.Terminal.ScrollbackLines > 100000 {
		profile.Terminal.ScrollbackLines = defaults.Terminal.ScrollbackLines
	}
	switch strings.ToLower(strings.TrimSpace(profile.Terminal.CursorStyle)) {
	case "bar", "block", "underline":
		profile.Terminal.CursorStyle = strings.ToLower(strings.TrimSpace(profile.Terminal.CursorStyle))
	default:
		profile.Terminal.CursorStyle = defaults.Terminal.CursorStyle
	}
	if profile.Source.Kind == "" {
		profile.Source.Kind = "manual"
	}
	seen := map[string]struct{}{}
	tags := make([]string, 0, len(profile.Tags))
	for _, tag := range profile.Tags {
		tag = strings.TrimSpace(tag)
		key := strings.ToLower(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		tags = append(tags, tag)
	}
	profile.Tags = tags
	return profile
}

func findProfile(settings model.Settings, id string) (model.ConnectionProfile, error) {
	for _, profile := range settings.Profiles {
		if profile.ID == id {
			return profile, nil
		}
	}
	return model.ConnectionProfile{}, fmt.Errorf("连接 %q 不存在", id)
}
