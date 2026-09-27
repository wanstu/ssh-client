# 交互规范

## 1. 连接列表

### 单击

只选中连接，不建立 SSH 会话。

### 双击

打开新 SSH 会话。

### Enter

当焦点位于连接条目时，打开当前选中连接。

### 更多菜单

建议包含：

- 打开新会话
- 编辑
- 复制连接
- 收藏 / 取消收藏
- 删除

当前 Desktop 右键菜单直接提供以上常用动作；删除必须确认。复制连接自动生成不冲突的副本名称，并且不继承保存的 Password credential reference。

## 2. 分组

分组用于信息组织，不参与 SSH 解析逻辑。

支持：

- 新建
- 重命名
- 删除
- 拖入连接
- 批量移动
- 折叠 / 展开

删除分组时默认只删除分组，不删除其中连接；连接回到“未分组”。

收藏是独立维度，不是分组。一个连接既可以属于“生产环境”，也可以同时出现在“收藏”。

## 3. 标签

标签用于跨分组检索，例如：

- Linux
- DB
- Web
- Production
- Customer-A

标签可多选。

## 4. 搜索与筛选

搜索字段至少匹配：

- 连接名称
- Host
- Username
- Group
- Tag

筛选维度：

- 连接状态
- Group
- Tag
- Auth method

搜索无结果时必须出现明确空状态。

## 5. 批量选择

进入批量模式后：

- 单击连接切换勾选
- 顶部显示已选数量
- 支持移动分组
- 添加 / 移除标签
- 收藏 / 取消收藏
- 删除

批量模式下双击连接不应直接建立 SSH 会话，避免语义冲突。

## 6. 会话标签

状态：

- Connecting
- Connected
- Reconnecting
- Disconnected
- Failed

关闭活动会话默认确认。

关闭已断开的纯历史标签可以直接关闭。

标签菜单建议包含：

- Rename
- Pin / Unpin
- Clone session
- Close
- Close others
- Close tabs to the right
- Restore last closed

当前 Desktop 在标签右键菜单实现 Rename / Pin / Clone / Close / Close others / Close tabs to the right。Rename 只修改当前运行时标签名称，不修改 Connection Profile；Pin 只在本次运行中生效，固定标签排列在左侧并保持相对顺序。批量关闭只进行一次活动会话确认，Close tabs to the right 按用户实际看到的标签顺序判断。Clone 创建独立 SSH Session，而不是共享同一个 Shell Channel。

“恢复最近关闭”使用 `Ctrl+Shift+T` 或会话侧栏触发。若原 Connection Profile 仍存在，则以当前 Profile 配置新建 SSH Session；否则只恢复为只读历史标签。恢复不会尝试复活旧 transport，也不会跨应用重启持久化终端正文。

## 7. 会话菜单

- Rename tab
- Clone session
- Export visible / scrollback text
- Clear terminal display
- Reconnect
- Disconnect and keep tab

当前 Desktop 已在 Session Tab 右键菜单提供以上操作。导出针对当前聚焦 Pane，必须由用户通过保存对话框选择目标路径；不会自动写固定目录。清空终端只清当前 Pane 的本地 screen cells，保留 scrollback、终端模式和远端状态，不发送 Ctrl+L 或其他远端输入。

Reconnect 对 Profile Session 复用原 Session ID，只替换 SSH transport / PTY，因此标签 Rename、Pin 和主 Pane scrollback 保留；使用当前 Profile 配置和正常凭据流程重新认证。临时 Quick Connect 在断开后不复用已经清除的 Password / Passphrase，需重新使用快速连接。自动重连中的 Session 使用“立即重试”而不是另起连接。

## 8. 拆分终端

第一阶段只提供左右二分。

同一标签内多个 Pane 默认共享同一 Connection Profile 和 SSH transport，但分别创建 Shell Channel / PTY。工具栏提供“左右分屏”，快捷键 `Alt+\\` 在两个 Pane 之间切换；第二 Pane 标题栏可单独关闭。

不先做任意网格拆分，也不持久化运行时 Pane；应用重启后仍从单 Pane 开始。

## 9. 快速连接

快速连接是一次性会话入口，不等同于“新建连接”。

默认：

- 输入 user@host:port
- 不保存到连接库
- 认证优先 Auto / Agent / known key
- 可临时选择 Password / Private Key

用户主动打开“连接成功后保存为连接”后，才进入持久化流程。

## 10. 命令片段

命令片段的动作是“插入文本”，不是“自动执行”。

当前 Desktop 将命令片段保存在本地 Settings，字段为名称、单行命令和 Tags；支持新建、编辑、删除以及按名称 / 命令正文 / Tags 搜索。左侧片段列表的“插入”、双击片段以及 Ctrl+K 中的片段结果，都只把命令文本写入当前聚焦 Pane。

默认不发送 Enter，也不允许片段正文包含 CR/LF、Tab、ESC、Ctrl+C 等控制字符。限制为普通单行文本是为了保证“插入”本身不会夹带终端控制动作；用户仍需在终端中明确按 Enter 才会执行。

没有已连接 SSH Session 时“插入”不可用。禁止把命令片段系统设计成无确认的一键或批量远程命令执行器。

## 11. Ctrl+K

全局命令面板负责：

- 搜索连接
- 搜索当前与最近会话
- 搜索命令片段并插入当前 Pane
- 复制当前选中连接
- 新建连接
- 快速连接
- 拆分终端
- 切换主题
- 打开设置

## 12. 推荐快捷键

| 操作 | 快捷键 |
| --- | --- |
| 命令面板 | Ctrl+K |
| 新建连接 | Ctrl+N |
| 新标签 / 快速连接 | Ctrl+T |
| 关闭标签 | Ctrl+W |
| 恢复最近关闭 | Ctrl+Shift+T |
| 拆分终端 | Alt+\ |
| 搜索连接 | Ctrl+F |
| 清空终端显示 | Ctrl+L |

最终快捷键应以 Windows/Linux/macOS 冲突测试为准。
