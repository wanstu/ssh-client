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

- 复制后生成新的 Profile。
- ID 必须不同。
- 默认要求确认或修改名称。
- Secret reference 是否复制要由 credential 策略决定，不能复制明文 secret。

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
- 无结果显示空状态。
- 清除筛选恢复全部连接。
- 搜索与筛选组合行为要稳定。

## 4. Batch Mode

- 开启后单击变成选择。
- 双击不能建立 Session。
- 已选数量实时更新。
- 移动 Group 可批量完成。
- Tag 操作可批量完成。
- Favorite 可批量切换。
- 批量删除需要确认。
- 退出批量模式清除选择状态。

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

- 目标 Profile 只引用 jump_profile_id。
- Jump Host 不存在时目标连接不可静默 Direct fallback。
- Jump Host Host Key 先验证。
- Jump Host Auth 先完成。
- 目标 Host Key 与目标 Auth 独立执行。
- UI 能定位错误发生在 Bastion 还是 Target。
- 第一阶段只允许一层 Jump Host。

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
- 光标作为独立 DOM 元素渲染，不覆盖终端字符；支持 bar / outline block / underline，失焦时降低亮度。
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
- Split pane 独立 Shell。
- 第一阶段只左右二分。

## 13. Session Tabs

- 新 Session 新标签。
- 切换不丢终端内容。
- 活动 Session 关闭默认确认。
- Disconnected 历史标签可直接关闭。
- Restore recently closed 恢复 UI 历史，不恢复已死亡的远端 SSH transport。

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
