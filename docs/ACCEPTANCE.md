# 开发验收清单

> 本文用于未来每个实现阶段的功能验收。不是自动化测试代码，但测试用例应覆盖这些行为。

## 1. Connection Library

### Create

- 新建连接后出现在正确 Group。
- Favorite 与 Group 独立。
- Tags 可多值。
- Host / Port / Username 非法输入有明确错误。
- 保存失败不关闭编辑器。

### Edit

- 编辑 Profile 不影响已经建立的 Session 的历史输出。
- 修改 Host / Auth 后，下次新建 Session 使用新配置。
- 已连接 Session 是否热更新配置：第一阶段明确不做。

### Duplicate

- 复制后生成新的 Profile，ID 必须不同。
- 默认名称自动使用“原名 副本”；重名时依次使用“副本 2 / 副本 3 ...”，复制操作不能因为名称冲突直接失败。
- 保存的 Password credential reference 不复制；不得复制任何明文 secret。
- 右键连接菜单和 Ctrl+K 均可触发复制；复制成功后选中新 Profile，方便继续编辑。

### Delete

- 删除 Profile 需要确认。
- 删除 Profile 不强制销毁已经存在的活动 Session。
- 删除 Jump Host Profile 时，如果有目标连接引用它，应阻止删除或明确处理引用关系。

## 2. Group

- 新建 / 重命名可用。
- 删除 Group 不删除其中 Profile。
- 删除后连接进入“未分组”。
- 批量移动更新所有选中 Profile。
- Favorite 列表不受 Group 移动影响。

## 3. Search / Filter

- Name 可搜索。
- Host 可搜索。
- Username 可搜索。
- Group 可搜索。
- Tag 可搜索。
- 无结果显示空状态，并明确区分“没有连接”和“搜索或筛选没有结果”。
- 连接筛选支持状态 / Group / Tag / Auth method 四个维度，可同时组合；Group / Tag 选项随当前 Settings 动态更新。
- 状态筛选至少区分 connected / connecting / reconnecting / disconnected / failed（含 security_blocked）/ offline（当前运行期无 Session）。
- 清除筛选恢复全部连接；筛选仅为前端运行时展示状态，不写入 Profile Settings。
- 搜索与筛选取交集，组合行为要稳定；筛选按钮显示当前启用条件数量。

## 4. Batch Mode

- 开启后单击连接切换选择，连接行显示明确勾选状态；右键不再打开单连接菜单。
- 双击不能建立 Session，连续双击最多改变选择状态，不得触发 SSH Connect。
- 已选数量实时更新，并显示当前搜索 / 筛选结果数量；“全选当前”只加入当前可见 Profile，“清空”清除全部选择。
- 移动 Group 可批量完成，空 Group ID 表示移到未分组。
- Tag 添加 / 移除可批量完成，输入支持中英文逗号，大小写不敏感去重。
- Favorite / Unfavorite 可批量切换。
- 批量删除只确认一次；如果选中的 Jump Host 仍被未选中 Profile 引用，整批拒绝，不得留下悬空 JumpProfileID。
- 批量删除成功后清理已删除 Profile 的安全凭据引用，但不强制关闭已经存在的活动 Session。
- 点击“完成”或离开连接导航必须退出批量模式并清除选择状态。

## 5. Quick Connect

- user@host 可解析。
- user@host:port 可解析。
- 默认不创建 Profile。
- 临时 Session 可以正常进入 Session 列表。
- 开启“连接成功后保存”后，成功后才创建 Profile。
- 连接失败时不创建半成品 Profile。

## 6. SSH Config Import

- 先 Preview。
- 用户可选择条目。
- 原文件不被修改。
- Private Key 内容不复制。
- known_hosts 不覆盖。
- ProxyJump 可识别。
- 非交互 Host 可跳过。
- 重复导入必须有冲突策略，不静默覆盖。

## 7. Direct SSH

- DNS / resolve 错误可定位。
- TCP timeout 可定位。
- Connection refused 可定位。
- SSH negotiation failure 可定位。
- Unknown Host Key 进入确认。
- Changed Host Key 进入 security_blocked。
- Auth failure 不进入网络自动重连循环。
- Shell open failure 能保留连接错误信息。

## 8. Host Key

### First Seen

- 展示 Host / Port。
- 展示 Algorithm。
- 展示 Fingerprint。
- Cancel 不保存。
- Trust once 不持久化。
- Trust and save 持久化。

### Changed

- 默认阻止。
- 展示旧、新指纹。
- 不自动重试。
- 更新记录前二次确认。
- 用户取消后仍保持阻止。

## 9. Authentication

### Password

- 错误密码明确提示 Auth failure。
- 不记录到普通日志。
- 保存时进入安全存储。

### Private Key

- 配置只保存路径。
- Key 不存在有明确错误。
- Passphrase required 是独立状态。
- Key rejected 是独立状态。

### Agent

- Agent unavailable 明确提示。
- Agent 拒绝签名有明确 Auth error。

## 10. Jump Host

- 目标 Profile 只引用 `jump_profile_id`；Desktop Profile 编辑页提供 Direct / Jump Host 网络模式、Jump Profile、Timeout、Keepalive 配置，不要求手改 Settings。
- Jump Host 不存在时目标连接不可静默 Direct fallback；引用的 Jump Profile 必须存在且使用 Direct 网络模式。
- Jump Host Host Key 先验证，Host Key challenge 的 `scope=jump`；目标 Host Key 使用 `scope=target`，两者分别保存/信任。
- Jump Host Auth 先完成，再通过 SSH `direct-tcpip` Channel 连接目标地址；目标 Host Key 与目标 Auth 独立执行。
- Jump Host Password 只从 Jump Profile 自己的安全 credential_ref 读取，不得复用目标 Profile 密码；没有已保存密码时在创建 Session 前明确报错。
- Jump Host 支持 Password（已保存）、SSH Agent、无口令 Private Key 与 Auto。加密 Jump 私钥需要单独 Passphrase 时必须明确报错，当前批次不猜测或复用目标 Passphrase。
- UI Host Key 对话框与 Session state/message 能定位错误发生在 Jump Host 还是 Target；Jump 连接、握手、认证、目标转发使用独立 stage / reason message。
- Session 最终结束时同时清除目标 Credentials 与 Jump Credentials 的运行时副本。
- 第一阶段只允许一层 Jump Host；Settings 校验禁止 Jump Profile 自身再使用 Jump Host。
- Runtime 必须有真实集成测试覆盖 `Local → Jump SSH → direct-tcpip → Target SSH → PTY/Shell`，而不只是配置对象测试。

## 11. Reconnect

- 网络中断保留输出。
- 标签状态变 Reconnecting。
- 显示 attempt。
- 用户可 Retry now。
- 用户可 Stop。
- Stop 后进入 Disconnected。
- Auth failure 不自动重连。
- Host Key Changed 不自动重连。

## 12. Terminal

- resize 使用实际字体字符宽度、line-height 与 viewport padding 计算 PTY 行列，并合并高频 resize。
- UTF-8 / CJK 宽字符正常；combining character 不额外占列。复杂 ZWJ emoji、旗帜 emoji和 East Asian Ambiguous Width 仍属于近似兼容范围。
- ANSI SGR 支持基础/bright 16 色、256 色、True Color，以及 bold / dim / italic / underline / inverse / hidden / strike。
- 光标使用终端内容流中的真实 DOM 锚点定位，并固定渲染为 Windows 风格 1px 单竖线；不得用 `col × 估算字符宽度` 直接决定可见光标位置，失焦时降低亮度。
- scrollback 有上限配置，并保留屏幕滚动时的 SGR 样式。
- alternate screen 支持 DEC 47 / 1047 / 1048 / 1049，退出后恢复 main screen、cursor 与相关状态。
- autowrap 使用 wrap-pending 语义；关闭 DEC ?7 后右边界不会错误滚屏。
- 支持 IRM、DECOM、DECSTBM，并在 Origin Mode 下把光标限制在 scroll region。
- 支持 application cursor / application keypad、F1-F12、Home/End/Insert/Delete/Page、方向键组合修饰键与 Shift+Tab。
- 支持 bracketed paste、focus reporting、mouse 1000 / 1002 / 1003 / 1006；按住 Shift 时保留本地选择和右键菜单。
- 支持 DEC Special Graphics、G0/G1/G2/G3、SO/SI，TUI 边框不应退化为 l/q/x 等字符。
- Tab Stop 支持默认 8 列、HTS、TBC、CHT、CBT。
- CSI 2 J 只清屏，不擅自移动光标；CSI 3 J 同时清 scrollback。
- 支持常见 DSR / DA / DECID / DECRQM 与 OSC 10/11/12 query response；OSC 缓冲有长度上限。
- copy / paste 正常。
- Ctrl+L 只清显示，不代表服务器执行清理。
- Split pane 独立 Shell：同一标签复用 SSH transport，但第二 Pane 必须创建独立 Shell Channel / PTY，不能只是同一输出的两个视图。
- 第一阶段只左右二分，每个 Session 最多 2 个 Pane；工具栏“左右分屏”和 `Alt+\\` 均可进入/切换分屏。
- 两个 Pane 分别处理输入、中文 IME、ANSI 状态、scrollback、鼠标协议和 PTY resize；关闭第二 Pane 不得断开主 Session。
- 主 Session 断开或重连时，附属 Pane 必须关闭并清理本地终端缓冲，不残留幽灵 Pane。
- 命令片段持久化在本地 Settings，至少包含 `id / name / command / tags`；旧 Settings 没有 `snippets` 字段时必须兼容为空列表，不升级 settings version。
- 命令片段支持新建、编辑、删除，并按名称、命令正文和 Tags 搜索；名称大小写不敏感唯一。
- 命令片段正文只允许普通单行文本，禁止 C0 / DEL 控制字符（包括 CR/LF、Tab、ESC、Ctrl+C 等）；单条正文上限 16 KiB。该限制用于保证“插入”本身不会夹带终端控制动作。
- 左侧“插入”、双击片段和 Ctrl+K 片段结果都只向当前聚焦 Pane 写入正文原文，不附加 Enter / CR / LF；必须由用户随后明确按 Enter 执行。
- 没有 `connected` Session 时片段插入不可用；分屏存在时必须写入当前聚焦 Pane，并沿用该 Session 的终端编码。

## 13. Session Tabs

- 新 Session 新标签。
- 切换不丢终端内容。
- 活动 Session 关闭默认确认。
- Disconnected 历史标签可直接关闭。
- 标签右键菜单提供“重命名标签 / 固定或取消固定 / 克隆会话 / 关闭 / 关闭其他 / 关闭右侧”；没有右侧标签时“关闭右侧”禁用。
- Rename 只改变当前 Session 的 UI 标签名称，不修改 Connection Profile，也不跨应用重启持久化；标签别名应同步显示在标签栏、当前会话标题、会话侧栏和 Ctrl+K 搜索中。
- Pin 只作用于当前运行时；固定标签排列在左侧并保持固定组内原相对顺序，取消固定后回到普通标签组。`关闭右侧` 必须以 Pin 后的视觉顺序为准。
- 关闭重命名过的真实 Session 时，“最近关闭”记录保留关闭时的标签别名；关闭后清理该 Session 的 Rename / Pin 运行时状态。
- Session 右键菜单提供“重新连接 / 断开并保留标签 / 清空当前终端显示 / 导出当前终端文本”。活动 Session 的重新连接禁用，结束态 Profile Session 才允许原地 Reconnect；自动重连态则触发 RetryNow。
- Profile Session 原地 Reconnect 必须复用相同 Session ID，旧 managedSession 先标记关闭并抑制晚到状态，新实例使用当前 Profile 配置重新建立 transport/PTY；Rename、Pin 和主 Pane scrollback 不得丢失。
- Quick Connect 断开后不得复用已清除的 Password / Passphrase；无 Profile 的结束态 Session 不允许无凭据原地重连。
- “清空当前终端显示”只清本地当前 Pane screen，不发送任何远端输入，不清 scrollback，不重置终端 mode/cursor 状态。
- “导出当前终端文本”导出当前聚焦 Pane 的 visible + scrollback 纯文本，并通过系统 Save Dialog 由用户选择路径；取消保存不报错，单次导出上限 32 MiB。
- 克隆活动 Session 必须创建新的 SSH Session/transport，使用原 Session 的运行时连接配置；新旧 Session ID 不同，关闭克隆 Session 不得影响原 Session。
- `history_only` 或已经结束的 Session 不允许直接 Clone Runtime Session。
- “关闭其他 / 关闭右侧”涉及多个活动 Session 时只确认一次；每个真实 Session 仍分别进入最近关闭历史。
- `CloseSession` 后不得再发送晚到的 Session State 事件，避免已关闭标签被 `disconnected` 状态重新加入 UI。
- 关闭真实 Session 后进入“最近关闭”；关闭 `history_only` 标签不得再次写入最近关闭，避免循环历史。
- `Ctrl+Shift+T` 与会话侧栏均恢复最新记录，并在触发后消费该记录，不能重复恢复同一条。
- 原 Profile 仍存在时，恢复操作使用当前 Profile 配置重新建立新的 SSH Session；绝不尝试复活已死亡的旧 transport。
- 原 Profile 已删除或原会话没有 `profile_id` 时，恢复为只读 `history_only` 标签。
- 跨应用重启只持久化最近关闭的会话元数据，不持久化终端正文；同一次运行内可使用内存中的截断终端文本展示历史。

## 14. Security Logging

日志中不得出现：

- Password。
- Key contents。
- Passphrase。
- Agent signing payload。
- 默认不得记录完整终端输入。

## 15. Desktop UX

- Terminal 使用固定兼容配色，不由应用主题重写 ANSI / OSC 基础色。
- Windows WebView2 下终端应支持中文 IME composition，普通 shell 与 vim/tmux 内均可输入中文；连续中文使用 Backspace 时每次只删除一个字符，不得因双宽字符列移动而误删前一个字符。
- 调整窗口高度时普通 Shell 内容不得产生大段空白或重复 prompt；缩小时优先裁剪光标以下区域，而不是无条件把顶部内容滚入 scrollback。
- 终端字体由 Desktop 控制；英文使用配置的等宽字体，中文使用稳定的 CJK fallback，并关闭代码字体连字以保证单元格布局。CJK/全角字符的 DOM 渲染宽度必须固定为两个 terminal cells，避免 fallback 字体实际像素宽度导致光标累计漂移。
- Comfortable / Compact 不造成文字截断。
- Ctrl+K 可键盘操作，命令面板必须使用完整主题样式而非浏览器默认控件样式。
- 所有 Modal 可 Escape 关闭，但高风险确认状态的关闭语义必须等于 Cancel / Keep blocked。

## 16. Release Gate

进入可发布版本前至少完成：

- Windows 基础验证。
- Linux 基础验证。
- macOS 如纳入当前版本则完成对应验证。
- Host Key 安全测试。
- Credential storage 测试。
- 网络断开 / 重连测试。
- 多 Session 稳定性测试。
- 长时间终端 scrollback 测试。
