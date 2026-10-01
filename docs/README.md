# 文档总入口

PCAS 的所有文档都从这一页找。新增或移动文档时，同时改这一页。

## 1 现在做到哪了

| 阶段 | 状态 | 入口 |
|---|---|---|
| 第 1 阶段：秘书前台 | 已上线（2026-10-01） | [任务总览](tasks/phase1/README.md) |
| 第 1.5 阶段：稳定化 | 修复已合并；线上实测 11 项和试用一天还没做 | [稳定化计划](tasks/stabilization/README.md) |
| 第 2 阶段：记忆核心 | 方向已定，**未开工**。第一次尝试（2.0）已回滚 | [任务入口](tasks/phase2/README.md) · [方向稿](tasks/phase2/direction.md) |
| 第 3 阶段及以后 | 未开始 | [白皮书 §16 路线图](whitepaper.md) |

还没处理的问题统一记在 [待办清单](tasks/backlog.md)。

## 2 按目的找

**想知道产品要做成什么**

| 文档 | 内容 |
|---|---|
| [白皮书](whitepaper.md) | 定位、团队角色、记忆核心、路线图、黄金路径。上位文档，其他文档与它冲突时以它为准 |
| [界面与交互原则](design/principles.md) | 先做后报、按钮只剩三种、出错要说清楚等界面和后台都要遵守的规则 |
| [记忆架构](memory-architecture.md) | 三层记忆的完整设计 |
| [产品需求](prd.md) | 需求清单 |
| [旧版经验记录](legacy-lessons.md) | 上一版踩过的坑 |

**要开始做任务**

| 文档 | 内容 |
|---|---|
| [任务流程规则](tasks/process.md) | 所有阶段通用：先推演状态、测试和实现分开、真实配置验收、部署后冒烟测试 |
| 第 1 节里对应阶段的入口 | 该阶段的范围、任务包、契约 |
| [待办清单](tasks/backlog.md) | 已知但没处理的问题，排任务时从这里取 |

**要部署、接模型、接数据**

| 文档 | 内容 |
|---|---|
| [部署与模型接入](deployment.md) | 部署方式、模型配置 |
| [Sign in with ChatGPT 套餐授权](chatgpt-plan-auth.md) | ChatGPT 订阅直连通道 |
| [通用资料接入](connectors.md) | 连接器、归档导入、摘要 |
| [统一记忆与工作台服务](memory-service.md) | 后端接口、配置、验证边界 |
| [前端说明](../web/README.md) | 前端开发和浏览器测试 |

线上实例的发布目录和操作说明不在仓库里，在部署主机的 `/root/PCAS-deploy/current/`。

**要查某次验收或评测的证据**：见第 4 节。

## 3 目录约定

| 目录 | 放什么 |
|---|---|
| `docs/` 顶层 | 长期有效的说明：白皮书、架构、需求、部署、接入 |
| `docs/design/` | 界面与交互原则 |
| `docs/tasks/<阶段>/` | 该阶段的任务包和契约。每个阶段有一个 `README.md` 作为入口 |
| `docs/tasks/` 顶层 | 跨阶段的流程规则和待办清单 |
| `docs/evaluations/` | 验收、评测、复盘记录。文件名以日期开头，写完之后不再改写结论 |
| `docs/research/` | 外部方案的研究笔记 |

- 依据的先后：白皮书 → 交互原则、记忆架构、产品需求 → 阶段契约 → 任务包。下游与上游冲突时先改上游。
- 执行者不修改白皮书和记忆架构，除非用户明确要求。
- 每份验收记录都要能从它所属阶段的入口或本页找到。
- 作废的方案不留在 `docs/` 里当作可用文档；需要保留的，打成 git 标签并在阶段入口里写明位置。

## 4 全部文档

### 第 1 阶段

入口：[任务总览](tasks/phase1/README.md) · [接口契约](tasks/phase1/contracts.md) · [上线后修复与线上实测清单](tasks/phase1/postlaunch.md)

| 任务包 | 说明 |
|---|---|
| [A](tasks/phase1/A-fixes.md) | 修三个硬伤 |
| [B1](tasks/phase1/B1-secretary-backend.md) · [B2](tasks/phase1/B2-secretary-frontend.md) | 撤销基础设施和秘书后端；秘书前端 |
| [C1](tasks/phase1/C1-notify.md) · [C2](tasks/phase1/C2-telegram-inbound.md) | 提醒通道；Telegram 双向对话 |
| [D1](tasks/phase1/D1-auto-adopt-backend.md) · [D2](tasks/phase1/D2-buttons-frontend.md) | 副手结果自动采纳；删按钮 |
| [E](tasks/phase1/E-acceptance.md) | 黄金路径验收与收尾 |

验收和评测记录：

| 记录 | 内容 |
|---|---|
| [第 1 阶段黄金路径验收](evaluations/2026-09-30-phase1-acceptance.md) | 任务 E 的验收报告（截图在同名目录） |
| [第 1 阶段上线后问题复盘](evaluations/2026-10-01-phase1-postlaunch.md) | 上线后暴露的问题和原因 |
| [连续记忆验收](evaluations/2026-09-29.md) | 固定场景的真实模型验收（数据在 `2026-09-29-continuity.json`） |
| [记忆与权限边界修复](evaluations/2026-09-30-memory-boundaries.md) | 2026-09-30 的边界修复 |
| [界面回归修复](evaluations/2026-10-01-ux-repairs.md) | PR #1 之后的界面回归 |

### 稳定化

入口：[稳定化计划](tasks/stabilization/README.md)（状态序列表和进入第 2 阶段的条件）· [执行分工](tasks/stabilization/dispatch.md)

| 任务包 | 说明 | 验收记录 |
|---|---|---|
| [F8](tasks/stabilization/F8-timezone-displays.md)（[补验](tasks/stabilization/F8-ci-followup.md)） | 各页面按工作区时区显示 | [时区显示验收](evaluations/2026-10-01-timezone-displays.md) |
| [F9](tasks/stabilization/F9-reminder-repairs.md) | 提醒边界与失败日志 | [提醒修复](evaluations/2026-10-01-reminder-repairs.md) |
| [F10](tasks/stabilization/F10-expired-undo.md) | 撤销有效期 | [撤销有效期评测](evaluations/2026-10-01-expired-undo.md) |
| [F11](tasks/stabilization/F11-secretary-repairs.md) | 对话串行、删除传播、失败说明 | [秘书修复](evaluations/2026-10-01-secretary-repairs.md) |
| [F12](tasks/stabilization/F12-telegram-repairs.md) | Telegram 身份、按钮绑定、语音重试 | [Telegram 修复](evaluations/2026-10-01-telegram-repairs.md) |
| [F13](tasks/stabilization/F13-approved-undo.md) | 逆序撤销规则 | [逆序撤销](evaluations/2026-10-01-approved-undo.md) |
| [F14](tasks/stabilization/F14-same-turn-actions.md) | 一句话新建并继续操作 | [同轮动作](evaluations/2026-10-01-same-turn-actions.md) |
| [F15](tasks/stabilization/F15-conversation-order.md) | 连续输入按接受顺序执行 | [顺序修复](evaluations/2026-10-01-conversation-order-repair.md) |
| [Q1](tasks/stabilization/Q1-conversation-order-audit.md) · [Q2](tasks/stabilization/Q2-conversation-order-acceptance.md) | 连续输入顺序的复现和独立验收 | [顺序独立验收](evaluations/2026-10-01-conversation-order-acceptance.md) |
| [T1](tasks/stabilization/T1-undo-tests.md) | 独立检验撤销与动作序列 | [撤销序列评测](evaluations/2026-10-01-stabilization-undo.md) |
| [T2](tasks/stabilization/T2-time-tests.md) | 独立检验提醒与时间 | [时间测试](evaluations/2026-10-01-stabilization-time.md) |
| [T3](tasks/stabilization/T3-secretary-tests.md) | 独立检验秘书、Telegram、删除传播 | [秘书测试](evaluations/2026-10-01-stabilization-secretary.md) |
| [T4](tasks/stabilization/T4-approved-rules-acceptance.md) | 独立验证四个已确认用例 | [已批准规则验收](evaluations/2026-10-01-approved-rules-acceptance.md) |
| [U1](tasks/stabilization/U1-ux-polish.md) | 首页、事项页、回执的体验 | [体验打磨](evaluations/2026-10-01-ux-polish.md) |
| [U2](tasks/stabilization/U2-settings-things-ux.md) | 设置和事项的日常使用体验 | [设置与事项体验](evaluations/2026-10-01-settings-things-ux.md) |
| [S1](tasks/stabilization/S1-settings-audit.md) | 设置开关实际作用清单 | [设置控件核对](evaluations/2026-10-01-settings-control-audit.md) |
| [A1](tasks/stabilization/A1-acceptance.md) | 集成审查与进入第 2 阶段的判定 | [集成审查与候选验收](evaluations/2026-10-01-stabilization-acceptance.md) |
| [C1](tasks/stabilization/C1-ci-parallel-real-backend.md) | 真实后端浏览器测试并行 | [并行 CI 评测](evaluations/2026-10-01-ci-parallel-real-backend.md) |
| [M0](tasks/stabilization/M0-memory-readiness.md) · [M1](tasks/stabilization/M1-context-path-audit.md) · [M2](tasks/stabilization/M2-phase2-scope-review.md) | 第 2 阶段开工前的三项调查 | M0 的产出是 [记忆核心调查](tasks/phase2/readiness.md)；M1、M2 的产出没有合并，见第 2 阶段入口第 6 节 |

### 第 2 阶段

| 文档 | 内容 |
|---|---|
| [任务入口](tasks/phase2/README.md) | 要交付什么、用户已经定下的事、约束、现在的基线、可参考的归档 |
| [方向稿](tasks/phase2/direction.md) | 分四批怎么做、每批上线后能感受到什么。用户已确认 |
| [2.0 回滚记录](evaluations/2026-10-01-phase2-0-rollback.md) | 第一次尝试做了什么、为什么回滚、回滚后的状态 |
| [记忆核心调查](tasks/phase2/readiness.md) | 2.0 之前的代码现状调查。方案部分未批准 |

2.0 的代码和研究稿不在 `docs/` 里，归档为 git 标签 `archive/phase2-0/*`，位置和用法见任务入口第 6 节。

### 研究

| 文档 | 内容 |
|---|---|
| [研究笔记：Hermes Agent](research/hermes-agent.md) | 外部方案调研 |
