# 产品开发路线

## Phase 0 — Design Baseline

当前阶段。

完成：

- HTML 主原型
- 正常会话
- 错误状态
- Host Key 安全状态
- 主题
- 快速连接
- SSH Config 导入概念
- 核心产品文档

退出条件：

- 产品边界稳定
- Connection / Session 模型稳定
- 安全规则稳定

## Phase 1 — Desktop Shell

目标：

- 桌面窗口
- Theme
- Settings
- Static navigation
- Local persistence skeleton

不接 SSH。

## Phase 2 — Connection Library

目标：

- Profile CRUD
- Group
- Tag
- Favorite
- Search
- Filter
- Batch operations
- Import preview

## Phase 3 — SSH Runtime

目标：

- Direct connection
- Host Key
- Password / Private Key / Agent
- Structured state machine
- Structured errors

先不做多 Session。

## Phase 4 — Terminal Session

目标：

- Terminal emulator
- One live shell
- resize
- clipboard
- scrollback

## Phase 5 — Multi Session

目标：

- Tabs
- Session list
- Disconnect
- Reconnect
- Restore recently closed UI state

## Phase 6 — Network Features

目标：

- Jump Host
- SOCKS5
- Keepalive
- reconnect policy

## Phase 7 — Productivity

目标：

- Split pane
- Command snippets
- Ctrl+K
- Import refinement
- Export terminal text

## Deferred

- SFTP
- Port forwarding dashboard
- cloud sync
- team sharing
- monitoring
- automation
- AI operations
