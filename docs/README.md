# SSH Client 文档入口

> 本目录是 SSH Client 的产品与开发契约。项目已从 Product Prototype 进入首个真实实现阶段：具备 Wails 桌面壳、Connection Profile、本地持久化、真实 Direct SSH、Host Key 校验、Session、终端交互、SSH Config 导入、Kit Runtime Theme 与 Secure Config。

## 文档地图

| 文档 | 用途 |
| --- | --- |
| [PRODUCT.md](./PRODUCT.md) | 产品定位、范围、信息架构与阶段边界 |
| [UX_INTERACTIONS.md](./UX_INTERACTIONS.md) | 主机列表、会话、标签、快捷键、菜单与交互规则 |
| [SESSION_STATES.md](./SESSION_STATES.md) | SSH 会话状态机、连接步骤、重连与错误状态 |
| [SECURITY.md](./SECURITY.md) | Host Key、凭据、危险操作与安全交互规则 |
| [CONNECTION_MODEL.md](./CONNECTION_MODEL.md) | 持久连接、临时连接、认证、代理、Jump Host 的产品数据模型 |
| [PROTOTYPE.md](./PROTOTYPE.md) | HTML 原型入口、评审场景与原型专用功能 |
| [IMPLEMENTATION_GUIDE.md](./IMPLEMENTATION_GUIDE.md) | 未来开发时的模块边界、优先级和验收原则 |
| [ACCEPTANCE.md](./ACCEPTANCE.md) | 可直接复用的功能验收清单 |
| [DECISIONS.md](./DECISIONS.md) | 已定产品决策，避免后续反复讨论 |
| [ROADMAP.md](./ROADMAP.md) | 从原型到可用版本的推荐阶段划分 |

## 当前原则

开发开始前，应先阅读 PRODUCT、SESSION_STATES、SECURITY、CONNECTION_MODEL。

任何实现如果与这些文档冲突，应先更新文档并记录到 DECISIONS，再修改代码。

## 当前原型

主原型：../index.html

当前原型只模拟界面与状态，不得把原型中的假数据、假指纹、假错误码或模拟终端输出当成实现约束。

## 文档状态

- 当前版本：v0.2.0
- 日期：2026-09-19
- 状态：首个可用版本；产品原型继续作为 UX 参考，不代表所有后续功能已经实现
