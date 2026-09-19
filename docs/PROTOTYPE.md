# HTML 原型说明

## 1. 文件

主原型：

```text
../index.html
```

预览图：

```text
../prototype-v4.png
```

旧预览仅作为历史留档。

## 2. 原型目的

原型用于确认：

- 信息架构
- 状态层级
- 主题
- 交互
- 错误表达
- 安全提示
- 菜单组织

不用于验证：

- SSH library
- PTY
- terminal emulator
- key storage
- networking
- process model

## 3. Prototype 场景入口

底部：

```text
Prototype v4 · 场景
```

此入口只用于设计评审，正式产品删除。

当前场景：

- 正常会话
- 首次使用
- Connecting
- Reconnecting
- Auth failed
- TCP timeout
- Unknown Host Key
- Host Key Changed

## 4. 假数据

原型中的：

- IP
- Fingerprint
- Latency
- Username
- Cipher
- Linux 输出

全部是假数据。

不能复制为测试凭据或默认配置。

## 5. 原型交互

当前 JavaScript 仅模拟：

- Modal / Popover
- Theme switch
- Density switch
- Search / status filter
- Group management
- Batch selection
- Profile duplicate / move / delete
- Direct / Jump Host / SOCKS5 configuration
- Tabs / session menus
- Split pane
- Connection state
- Host Key confirmation
- Import preview

未来实现时不要把原型 JS 当业务实现迁移。
