# v0.4.0-rc.1 Release Candidate

候选日期：2026-09-28

本候选版是在 v0.3.0 之后的功能收口版本。Phase 1–7 的既定目标已全部进入实现与自动化验证阶段；本 RC 用于真实桌面运行验收与跨平台 Release 验收。

## 主要新增

- 左右分屏终端：同一 SSH transport 下创建独立 Shell Channel / PTY。
- Session Tab：重命名、Pin、Clone、关闭其他、关闭右侧、恢复最近关闭。
- Session 操作：原标签重连、断开保留、清空当前 Pane、导出终端文本。
- Connection Library：复制连接、搜索筛选、批量移动 / 标签 / 收藏 / 删除。
- Command Snippets：本地 CRUD、搜索、Ctrl+K、插入当前 Pane；只插入普通单行文本，不自动执行。
- Jump Host / ProxyJump：单层 Jump Host、独立 Host Key 校验、真实 direct-tcpip 集成测试。
- SOCKS5：NO AUTH SOCKS5 CONNECT、域名交由代理解析、完整 SSH 会话集成测试。
- SSH Config Import：Include 展开、ProxyJump 映射、重导预览、幂等导入与 source identity 收紧。

## 终端兼容性

- 中文 IME 输入。
- CJK 全宽字符光标与退格修复。
- 固定兼容终端配色，避免应用 Theme 干扰 ANSI / 全屏程序。
- 更稳定的窗口高度 resize 行为。
- 明确的终端字体 fallback 与禁用 ligature。

## 发布与版本

Release 使用 Wails Desktop Kit v0.9.0：

- About 显示完整 tag，例如 `v0.4.0-rc.1`。
- Windows 固定文件版本使用纯数字 `0.4.0`。
- Release 资产文件名自动包含 tag。
- 每个发布产物生成对应 `.sha256`。
- Windows / Linux / macOS 使用统一 reusable workflow。

## RC 验收重点

1. Windows 实机验证：About 版本、文件属性版本、退出 / 托盘、中文 IME、分屏、窗口 resize。
2. 真实 SSH：Direct、Password、Private Key、Agent、Jump Host、SOCKS5。
3. Session：Clone、Reconnect、最近关闭、Pin / Rename、批量关闭。
4. Connection Library：筛选、批量模式、SSH Config Include / 重导。
5. Release：确认 Windows EXE、Linux raw / deb、macOS app.zip 与各自 checksum 均存在且非空。

## 已知边界

- SOCKS5 当前仅支持 NO AUTH。
- Jump Host 当前只支持单层；Jump Profile 必须为 Direct。
- 命令片段只支持普通单行文本，避免插入时触发远端控制动作。
- SFTP、端口转发 Dashboard、云同步、团队共享仍属于 Deferred。
