# 开发实施指南

## 1. 当前实现基线

当前已锁定：

- Desktop：Go + Wails v2.15；Go Runtime 依赖 Wails Desktop Kit v0.5.1，CI / Release reusable workflow 固定 v0.5.2（该版本修复 `.deb` control metadata），并启用 Linux amd64 `.deb` 打包。
- Theme：Desktop Kit Runtime Theme；不再编译依赖独立 Theme Module。mode 与 Theme Pack 正交，完整主题集可运行时刷新，Kit 内置 4 个离线 fallback。
- SSH：`golang.org/x/crypto/ssh`。
- Connection/Profile：本地 JSON。
- 配置目录：Kit `paths`，默认 `~/.config/ssh-client`。
- Credential：Kit `secureconfig`，Profile 只保存 `credential_ref`。
- Host Key：应用独立 `known_hosts.json` 记录并保持显式确认流程。
- Terminal：当前为应用内轻量终端缓冲实现，后续仍可替换成熟 emulator 而不改变 Session 契约。

## 2. 推荐模块边界

### app-shell

负责：

- desktop window
- theme
- settings
- global command palette

### connection-store

负责：

- profiles
- groups
- tags
- import / export

不负责真实 SSH 网络连接。

### credential-store

负责：

- OS secure storage abstraction
- password/passphrase reference

普通 connection store 只保存 credential_ref。

### ssh-runtime

负责：

- resolve
- TCP
- handshake
- Host Key
- auth
- reconnect
- session lifecycle

### terminal-runtime

负责：

- terminal emulator binding
- shell channel
- pane lifecycle
- resize
- clipboard

### session-ui

负责：

- tabs
- status
- banners
- error states
- details drawer

## 3. 不要混合的概念

### Connection Profile != Session

Profile 是配置。

Session 是运行状态。

### Session != Terminal Pane

Session 是一次 SSH 会话上下文。

Pane 是该会话里的一个 Shell 视图。

### App Theme != Terminal Theme

两者独立。

### Import != Sync

第一阶段 SSH Config 只导入，不双向同步。

## 4. 开发顺序建议

1. App shell + static connection store
2. Connection CRUD / groups / tags
3. SSH runtime state machine without full UI polish
4. Host Key verification
5. Authentication
6. Single terminal session
7. Session tabs
8. Disconnect / reconnect
9. Error states
10. Split pane
11. SSH Config import
12. Settings / themes / command palette

不要先做 SFTP 或高级运维功能。

## 5. 状态驱动 UI

UI 不应根据错误字符串猜状态。

Runtime 应返回结构化状态：

```text
state
stage
reason_code
message
technical_detail
available_actions
```

## 6. 验收原则

每个功能不仅验 happy path，还需要至少验证：

- cancel
- timeout
- invalid input
- retry
- app reopen
- session close
- network loss
- credential failure

Host Key 相关还要验证：

- first seen
- accepted once
- accepted persistently
- changed
- rejected

## 7. 原型与实现差异

开发时应尽量保持产品结构，但允许：

- 调整尺寸
- 修正控件行为
- 替换图标
- 因平台差异调整快捷键

不允许未经产品决策直接改变：

- 安全规则
- 状态含义
- Profile / Session 边界
- Quick Connect 一次性语义
