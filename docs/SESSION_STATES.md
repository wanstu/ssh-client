# SSH 会话状态机

## 1. 顶层状态

建议会话状态：

```text
idle
connecting
host_key_pending
authenticating
connected
reconnecting
disconnected
failed
security_blocked
closing
closed
```

UI 可以使用更友好的中文文案，但内部状态应保持稳定。

## 2. 建立连接步骤

建议把一次连接拆成可观察阶段：

```text
resolve_host
tcp_connect
ssh_handshake
host_key_verify
authenticate
open_session
open_shell
connected
```

错误应该携带 stage。

例如：

```text
NETWORK_TIMEOUT
stage=tcp_connect
```

而不是只返回：

```text
connection failed
```

## 3. Connecting

UI：

- 保留当前标签
- 展示连接目标
- 展示当前步骤
- 可以取消

不需要长期显示每一个成功步骤；连接快速完成时可以只短暂显示。

## 4. Connected

UI：

- 绿色状态
- 显示 username@host:port
- 延迟
- SSH version / cipher 可放状态栏或详情
- 终端获得主要焦点

## 5. Reconnecting

触发：

- 已建立会话后网络短暂断开
- keepalive 判断连接不可用

UI：

- 原终端内容继续可见
- 顶部非遮挡式 banner
- 显示重试次数
- 显示下一次重试
- 提供立即重试
- 提供停止自动重连

停止后进入 disconnected。

## 6. Disconnected

不是错误状态。

常见原因：

- 用户主动断开
- 用户停止重连
- 服务器主动关闭连接

终端输出仍保留。

## 7. Failed

用于不可恢复或当前连接尝试已结束的普通失败。

示例：

- DNS failure
- TCP timeout
- Connection refused
- Auth rejected
- Shell open failed

Failed 必须带：

- stage
- reason_code
- user_message
- technical_detail
- recommended_actions

## 8. Security Blocked

Host Key Changed 属于安全阻断，不等同 Failed。

在用户确认之前：

- 不自动重试
- 不自动更新 known host
- 不进入认证阶段

## 9. 自动重连

建议使用退避，不要高频重试。

产品原型不锁定具体算法，但建议实现允许：

- 初始短间隔
- 逐步增长
- 最大间隔
- 可取消

如果认证失败，不应该进入自动网络重连循环。

## 10. 错误码建议

### Network

- DNS_RESOLVE_FAILED
- NETWORK_TIMEOUT
- CONNECTION_REFUSED
- NETWORK_UNREACHABLE
- CONNECTION_RESET

### SSH

- SSH_NEGOTIATION_FAILED
- HOST_KEY_UNKNOWN
- HOST_KEY_CHANGED
- HOST_KEY_REJECTED

### Auth

- AUTH_METHOD_REJECTED
- AUTH_PASSWORD_REJECTED
- AUTH_KEY_REJECTED
- AUTH_AGENT_UNAVAILABLE
- AUTH_KEY_PASSPHRASE_REQUIRED

### Session

- SESSION_OPEN_FAILED
- SHELL_OPEN_FAILED
- PTY_REQUEST_FAILED

这些名称是产品 / 实现契约建议，开发时可细化，但不要退化成单一 generic error。
