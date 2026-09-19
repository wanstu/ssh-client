# 安全设计

## 1. 原则

SSH Client 不应该为了“连接更顺滑”而削弱 SSH 的身份校验。

安全警告必须区分普通失败与安全阻断。

## 2. 未知 Host Key

第一次连接未知主机时：

必须显示：

- Host
- Port
- Key algorithm
- Fingerprint

允许：

- Cancel
- Trust once
- Trust and save

不提供：

- 全局自动信任所有未知 Host
- 静默写入 known_hosts

## 3. Host Key Changed

已保存指纹发生变化时：

默认阻断连接。

必须显示：

- Old fingerprint
- New fingerprint
- Host / Port
- 旧记录时间（若有）
- 连接已阻止

动作：

- Keep blocked
- How to verify
- Confirm and update known host

“Confirm and update”应经过二次确认。

## 4. 凭据

### Password

已使用 Wails Desktop Kit v0.4.0 Secure Config：

- 普通 Profile 只保存 `credential_ref`。
- 密码使用 AES-256-GCM 密文保存在 `~/.config/ssh-client/secure/`。
- 随机主密钥保存在操作系统安全凭据库，不和密文目录放在一起。
- 只有 SSH Session 真正进入 `connected` 后才保存用户勾选“保存密码”的凭据。
- 密码错误、Host Key 阻断或连接失败不会覆盖已保存 Secret。
- 系统安全存储不可用时不得降级成明文保存。

### Private Key

默认只保存路径引用。

禁止默认把私钥内容复制进应用数据库。

### Passphrase

应与普通 Password 分开处理。

### SSH Agent

Agent unavailable 要作为明确认证状态，而不是 fallback 后静默失败。

## 5. SSH Config 导入

导入：

- 不修改原 config
- 不复制 private key
- 不覆盖 known_hosts
- ProxyJump 可以解析为连接链
- Include 需要明确解析来源

## 6. 危险 UI

以下动作需要确认：

- 删除连接
- 批量删除
- 更新 changed host key
- 关闭活动会话（默认）
- 清除保存的凭据

以下动作一般不需要确认：

- 断开已经失效的会话
- 清除筛选
- 关闭历史标签
- 从收藏移除

## 7. 终端内容

终端可能包含敏感数据。

未来实现日志导出时：

- 必须由用户主动操作
- 不默认自动上传
- 不默认云同步
- 导出前可提示内容来自远端终端

## 8. 日志

应用日志不得记录：

- Password
- Private key contents
- Passphrase
- Agent signing payload
- Terminal input 原文（默认）

连接错误可以记录：

- Host
- Port
- stage
- reason code
- timing

是否记录 Username 应在隐私策略中明确。
