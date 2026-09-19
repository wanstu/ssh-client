# 产品决策记录

## D-001 单击不连接

状态：Accepted

连接列表单击只选中；双击或 Enter 才连接。

原因：避免误连生产服务器。

## D-002 断线保留标签

状态：Accepted

网络断开不自动销毁会话标签和终端输出。

## D-003 Host Key Changed 默认阻断

状态：Accepted

不能设计普通“继续连接”主按钮。

## D-004 Quick Connect 默认不保存

状态：Accepted

快速连接是一次性会话入口。

## D-005 SSH Config 只导入，不双向同步

状态：Accepted

第一阶段 Import 之后 Connection Profile 由应用独立管理。

## D-006 私钥只保存路径引用

状态：Accepted

默认不复制 Private Key 内容到应用数据库。

## D-007 收藏与分组独立

状态：Accepted

Favorite 是一个独立维度，不作为 Group。

## D-008 第一阶段单连接只属于一个 Group

状态：Accepted

Tags 用于多维分类。

## D-009 第一阶段 Jump Host 最多一层

状态：Proposed

优先降低状态与错误链路复杂度；多级 ProxyJump 后续扩展。

## D-010 第一阶段只做左右二分终端

状态：Accepted

不先实现任意网格 Pane。

## D-011 命令片段只插入不执行

状态：Accepted

默认不发送 Enter。

## D-012 应用主题与终端主题分离

状态：Accepted


## D-013 配置目录统一使用 Kit paths

状态：Accepted

应用配置统一到 `$XDG_CONFIG_HOME/ssh-client`，未设置时为 `~/.config/ssh-client`。从旧 `os.UserConfigDir()/ssh-client` 迁移时只复制缺失文件，不覆盖新目录、不删除旧目录。

## D-014 Secret 不进入普通配置

状态：Accepted

Password 等敏感数据使用 Desktop Kit Secure Config。Connection Profile 只保存 `credential_ref`；保存动作必须在 SSH 成功连接后发生，系统安全存储失败不得降级为明文。

## D-015 应用主题使用 Kit Theme Pack

状态：Accepted

light / dark / system 由 Desktop Kit theme runtime 管理，可选视觉配色由 Desktop Kit Theme Pack 提供。SSH Client 不再维护 Midnight / Graphite 等完整组件 token 副本，只保留终端区域的产品专属样式。

