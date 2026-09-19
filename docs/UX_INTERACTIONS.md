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
- 查看详情
- 收藏 / 取消收藏
- 复制连接
- 编辑
- 移动到分组
- 删除

删除必须确认。

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
- Pin
- Clone session
- Close
- Close others
- Close tabs to the right
- Restore last closed

## 7. 会话菜单

- Rename tab
- Clone session
- Export visible / scrollback text
- Clear terminal display
- Reconnect
- Disconnect and keep tab

“清空终端显示”只影响显示缓冲，不代表清除服务器历史。

## 8. 拆分终端

第一阶段只提供左右二分。

同一标签内多个 Pane 默认共享同一 Connection Profile，但分别创建 Shell Channel。

不先做任意网格拆分。

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

默认不发送 Enter。

禁止把命令片段系统设计成无确认的远程批量命令执行器。

## 11. Ctrl+K

全局命令面板负责：

- 搜索连接
- 搜索当前与最近会话
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
