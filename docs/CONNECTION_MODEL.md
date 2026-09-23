# 连接模型

## 1. Connection Profile

建议产品层对象：

```text
ConnectionProfile
  id
  name
  host
  port
  username
  group_id?
  tags[]
  favorite
  auth
  network
  terminal
  reconnect
  created_at
  updated_at
```

不要把运行中的 Session 状态塞回 ConnectionProfile。

## 2. Group

```text
ConnectionGroup
  id
  name
  order
```

连接最多属于一个 Group。

收藏与标签独立于 Group。

## 3. Tag

Tag 是多对多。

建议保存标准化文本并保留显示名。

## 4. AuthConfig

产品层建议：

```text
AuthConfig
  mode = password | private_key | ssh_agent | auto
  private_key_path?
  credential_ref?
  agent_identity_hint?
```

credential_ref 只引用安全存储中的凭据，不在普通配置中放 secret。

## 5. NetworkConfig

```text
NetworkConfig
  mode = direct | jump_host | socks5
  connect_timeout
  keepalive_interval
  jump_profile_id?
  socks5_host?
  socks5_port?
```

## 6. Jump Host

优先复用已有 Connection Profile 作为 Jump Host，而不是在目标连接里复制一整份跳板机认证配置。

例如：

```text
target-prod-db
  jump_profile_id = bastion-prod
```

这样：

- Jump Host 密钥变更只改一处
- 可避免凭据重复
- UI 可以清晰展示链路

### 链路展示

建议：

```text
Local
  → bastion-prod
  → prod-db-01
```

第一阶段最多支持一层 Jump Host 可以显著降低实现复杂度。

多级 ProxyJump 后续扩展。

## 7. TerminalConfig

```text
TerminalConfig
  term
  encoding
  font_family
  font_size
  color_scheme
  scrollback_lines
  cursor_style
```

默认值：

```text
term             = xterm-256color
encoding         = UTF-8
font_family      = Cascadia Code
font_size        = 13
color_scheme     = midnight
scrollback_lines = 10000
cursor_style     = bar
```

约束：

- font_size：10 ~ 28。
- scrollback_lines：100 ~ 100000。
- color_scheme：保留 midnight / graphite / daylight 作为配置兼容字段；Desktop 当前固定使用 midnight 兼容配色，不再暴露切换入口。
- cursor_style：bar / block / underline。
- 不合法值由后端 normalize 回默认值，枚举值统一为小写。
- Profile TerminalConfig 应实际作用于终端字体、字号、scrollback、cursor、encoding 和 PTY TERM；color_scheme 当前仅保留数据兼容。
- Terminal 配色与整个应用的 Theme Pack 独立，应用主题不应写进 TerminalConfig。
- 当前字符宽度实现以常见 CJK / combining / 全角字符为目标，不宣称完整实现 Unicode grapheme segmentation 或所有 wcwidth 规则。

## 8. ReconnectConfig

```text
ReconnectConfig
  enabled
  keep_tab_on_disconnect
  retry_policy
```

retry_policy 第一阶段可以只使用应用全局策略，不必给每个连接暴露复杂参数。

## 9. Session

运行时对象建议：

```text
Session
  id
  profile_id?
  temporary_target?
  state
  stage
  started_at
  connected_at?
  disconnected_at?
  panes[]
  reconnect_attempt
  last_error?
```

快速连接可以没有 profile_id。

## 10. Pane

```text
TerminalPane
  id
  session_id
  shell_channel_id
  title?
```

同一标签拆分终端时，Pane 独立 Shell Channel，共享 Session transport / profile 的具体实现方式由技术方案决定。

## 11. Import Source

建议记录连接来源：

```text
source = manual | ssh_config | quick_saved
source_ref?
```

但导入后的连接应成为应用自己的 Profile，不与原 SSH Config 建立隐式双向同步。

否则用户很难理解“到底改哪里才生效”。

## 12. SSH Config 关系

第一阶段：

- Import 是一次性复制
- 不自动双向同步
- 后续可增加 Re-import Preview

这是刻意的产品边界。
