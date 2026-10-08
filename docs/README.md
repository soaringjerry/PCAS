# PCAS Documentation

Start with the current specification for your work.
Use project status to distinguish a target from an implemented or deployed function.
Historical tasks, research, and evaluations are evidence. They do not assign new work.

## Current Documents

| Document | Purpose |
|---|---|
| [Product Whitepaper](whitepaper.md) | Product purpose, roles, memory, actions, [roadmap](whitepaper.md#10-roadmap), and acceptance. |
| [Memory Architecture](memory-architecture.md) | Memory objects, retrieval, current state, correction, and recovery. |
| [Interface Principles](design/principles.md) | Daily presentation and observation controls. |
| [Development Workflow](tasks/process.md) | Direction, execution, checks, merge, release, and history. |
| [Writing Guide](writing-guide.md) | STE100 rules and project terminology. |
| [Project Status](status.md) | Code support, recorded delivery, targets, and gaps. |
| [Service Reference](memory-service.md) | HTTP interfaces, code paths, and current processing values. |
| [Deployment](deployment.md) | Setup, models, account login, backup, and live checks. |
| [Input Reference](connectors.md) | Record format, archives, polling, folders, and implemented limits. |
| [Open Issues](tasks/backlog.md) | Recorded unresolved and uncertain findings. |
| [Historical Records](history/README.md) | Retired designs and interpretation of past records. |

## Document Authority

1. The user's explicit decisions govern the requested work.
2. The whitepaper gives current product requirements.
3. Memory and interface specifications give their respective requirements.
4. The workflow gives development and release procedures.
5. Implementation references give code information and operating procedures.
6. New batch scopes use these specifications. They cannot silently override them.

If a proposed change conflicts with a current specification, resolve the direction and update that specification before implementation.
Historical precedence statements do not apply to new work.
Product quality must have measured evidence. Deployment records apply to their recorded revision, date, and channel.

## Document Maintenance

Current specifications use English under the [Writing Guide](writing-guide.md).
Keep one authoritative location for a requirement. Link to it from related documents.

Keep completed tasks and past results in historical records. Keep open issues in the backlog.
Update this index when adding, merging, moving, or retiring a document.
Keep historical conclusions and source references.
Keep Phase 2.0 archived.

## Repository Guides

- [Repository entry](../README.md).
- [Frontend development and browser checks](../web/README.md).
- [Memory and workspace code](../internal/).
- [Configuration example](../config/models.example.json).

<details>
<summary>Test and evaluation references outside docs/</summary>

- [Backend acceptance runner](../web/tests/support/README.md).
- [Doing evaluation tool](../cmd/pcas-eval/doing/README.md).
- [Recall evaluation dataset](../testdata/phase2/eval/README.md).
- [Hard recall dataset](../testdata/phase2/eval/hard/README.md).
- [Doing dataset](../testdata/phase2_5/doing/README.md).
- [Independent doing dataset](../testdata/phase2_5/doing-independent/README.md).
- [Recorded result: 2026-10-02-fake-keyword](../testdata/phase2/eval/results/2026-10-02-fake-keyword.md).
- [Recorded result: 2026-10-02-fake-vector](../testdata/phase2/eval/results/2026-10-02-fake-vector.md).
- [Recorded result: 2026-10-02-v2-basic-gold-before](../testdata/phase2/eval/results/2026-10-02-v2-basic-gold-before.md).
- [Recorded result: 2026-10-02-v2-basic-raw-before](../testdata/phase2/eval/results/2026-10-02-v2-basic-raw-before.md).
- [Recorded result: 2026-10-02-v2-hard-gold-before](../testdata/phase2/eval/results/2026-10-02-v2-hard-gold-before.md).
- [Recorded result: 2026-10-02-v2-hard-raw-before](../testdata/phase2/eval/results/2026-10-02-v2-hard-raw-before.md).

</details>

## Complete Document Catalog

Each Markdown document under `docs/` is listed below or in the current-document table.
The catalog keeps recorded historical titles for source identification.
The groups below contain historical material and link-compatibility files, not additional current specifications.

<details>
<summary>Merged document links</summary>

- [ChatGPT Account Instructions Location](chatgpt-plan-auth.md)
- [Product Requirements Location](prd.md)

</details>

<details>
<summary>Evaluations and deployment records</summary>

- [2026-09-29 连续记忆验收](evaluations/2026-09-29.md)
- [Memory and permission boundary repair — 2026-09-30](evaluations/2026-09-30-memory-boundaries.md)
- [第 1 阶段黄金路径验收（任务 E）](evaluations/2026-09-30-phase1-acceptance.md)
- [已批准规则的独立验收](evaluations/2026-10-01-approved-rules-acceptance.md)
- [F13：已批准的同事项逆序撤销](evaluations/2026-10-01-approved-undo.md)
- [Parallel real-backend CI evaluation](evaluations/2026-10-01-ci-parallel-real-backend.md)
- [Q2：连续输入顺序独立验收](evaluations/2026-10-01-conversation-order-acceptance.md)
- [F15：按服务端接受顺序处理秘书轮次](evaluations/2026-10-01-conversation-order-repair.md)
- [F10 撤销有效期修复评测](evaluations/2026-10-01-expired-undo.md)
- [第 1 阶段上线后问题复盘](evaluations/2026-10-01-phase1-postlaunch.md)
- [第二阶段 2.0 回滚记录](evaluations/2026-10-01-phase2-0-rollback.md)
- [第 2 阶段第 1 批独立验收：首交付记录（待集成验收）](evaluations/2026-10-01-phase2-batch1-acceptance.md)
- [F9 提醒边界与失败日志修复（2026-10-01）](evaluations/2026-10-01-reminder-repairs.md)
- [F14：同轮新建并继续操作](evaluations/2026-10-01-same-turn-actions.md)
- [F11：秘书轮次串行、失败说明与删除复核](evaluations/2026-10-01-secretary-repairs.md)
- [设置控件实际行为核对（S1，2026-10-01）](evaluations/2026-10-01-settings-control-audit.md)
- [U2：设置、待确认内容与事项页的日常使用体验](evaluations/2026-10-01-settings-things-ux.md)
- [A1 稳定化集成审查与候选验收](evaluations/2026-10-01-stabilization-acceptance.md)
- [T3：秘书、Telegram 与删除传播独立测试](evaluations/2026-10-01-stabilization-secretary.md)
- [T2 提醒与时间独立测试（2026-10-01）](evaluations/2026-10-01-stabilization-time.md)
- [T1 撤销与动作序列独立评测](evaluations/2026-10-01-stabilization-undo.md)
- [F12：Telegram 身份、回调绑定与语音恢复](evaluations/2026-10-01-telegram-repairs.md)
- [F8：跨页面工作区时区显示验收](evaluations/2026-10-01-timezone-displays.md)
- [U1：首页、事项页与回执的体验打磨](evaluations/2026-10-01-ux-polish.md)
- [UX regression repairs after PR #1](evaluations/2026-10-01-ux-repairs.md)
- [第 2 阶段第 1 批：38a8956 裁定后统一全量验收](evaluations/2026-10-02-phase2-batch1-acceptance.md)
- [第 2 批独立验收：浏览器复核完成](evaluations/2026-10-02-phase2-batch2-acceptance.md)
- [第 3 批独立验收：第 2 批进 main 后的整库结果](evaluations/2026-10-02-phase2-batch3-acceptance.md)
- [第 4 批独立验收：测试预先交付（2026-10-02）](evaluations/2026-10-02-phase2-batch4-acceptance.md)
- [第二阶段任务 V：合成回忆评测与假模型流程验证](evaluations/2026-10-02-phase2-eval.md)
- [M1 image validation](evaluations/2026-10-03-m1-images.md)
- [第 2 批补充验收：R23 秘书原话的对话上文](evaluations/2026-10-03-phase2-batch2-conversation-context-acceptance.md)
- [第 4 批 T4：最终分支的整段协议夹具迁移通过（2026-10-03）](evaluations/2026-10-03-phase2-batch4-acceptance.md)
- [第 4b 批 T2 独立验收（2026-10-03）](evaluations/2026-10-03-phase2-batch4b-acceptance.md)
- [记忆查找办法的对比实验](evaluations/2026-10-04-memory-lookup-experiment.md)
- [记忆查找与使用：第二轮对比实验](evaluations/2026-10-04-memory-lookup-round2.md)
- [V2 虚构实验数字产物](evaluations/2026-10-04-phase2_5-v2-artifacts/README.md)
- [Phase 2.5 doing evaluation](evaluations/2026-10-04-phase2_5-v2-artifacts/result.md)
- [第 2.5 阶段 V2：虚构办事题集与基线](evaluations/2026-10-04-phase2_5-v2-doing.md)
- [V2b 数字产物与重算](evaluations/2026-10-04-phase2_5-v2b-artifacts/README.md)
- [evaluations/2026-10-04-phase2_5-v2b-artifacts/all168-categories.md](evaluations/2026-10-04-phase2_5-v2b-artifacts/all168-categories.md)
- [Phase 2.5 doing evaluation](evaluations/2026-10-04-phase2_5-v2b-artifacts/all168.md)
- [evaluations/2026-10-04-phase2_5-v2b-artifacts/chain-table.md](evaluations/2026-10-04-phase2_5-v2b-artifacts/chain-table.md)
- [evaluations/2026-10-04-phase2_5-v2b-artifacts/disagreements-table.md](evaluations/2026-10-04-phase2_5-v2b-artifacts/disagreements-table.md)
- [evaluations/2026-10-04-phase2_5-v2b-artifacts/new48-categories.md](evaluations/2026-10-04-phase2_5-v2b-artifacts/new48-categories.md)
- [Phase 2.5 doing evaluation](evaluations/2026-10-04-phase2_5-v2b-artifacts/new48.md)
- [evaluations/2026-10-04-phase2_5-v2b-artifacts/noise-table.md](evaluations/2026-10-04-phase2_5-v2b-artifacts/noise-table.md)
- [evaluations/2026-10-04-phase2_5-v2b-artifacts/old120-categories.md](evaluations/2026-10-04-phase2_5-v2b-artifacts/old120-categories.md)
- [Phase 2.5 doing evaluation](evaluations/2026-10-04-phase2_5-v2b-artifacts/old120.md)
- [evaluations/2026-10-04-phase2_5-v2b-artifacts/overall-table.md](evaluations/2026-10-04-phase2_5-v2b-artifacts/overall-table.md)
- [P2 秘书一轮对话的数据库耗时](evaluations/2026-10-05-p2-turn-speed.md)
- [第 2.5 阶段第 1 批：独立验收最终一轮](evaluations/2026-10-05-phase2_5-batch1-acceptance.md)
- [第 2.5 阶段第 2–4 批独立验收：最终一轮及上线并发补验](evaluations/2026-10-05-phase2_5-batch234-acceptance.md)
- [第 2.5 阶段第 4 批：上下文接力、三档与自查](evaluations/2026-10-05-phase2_5-batch4-use.md)
- [第 2.6 阶段上线记录（2026-10-07）](evaluations/2026-10-07-phase2_6-rollout.md)
- [第 3 阶段上线记录（2026-10-08）](evaluations/2026-10-08-phase3-rollout.md)
- [第 3.5 阶段上线记录（2026-10-08）](evaluations/2026-10-08-phase3_5-rollout.md)
- [第 3.6 阶段上线记录（2026-10-08）](evaluations/2026-10-08-phase3_6-rollout.md)

</details>

<details>
<summary>Historical source records</summary>

- [Historical ChatGPT Account Record](history/chatgpt-plan-auth.md)
- [Resolved Issue History](history/resolved-issues.md)

</details>

<details>
<summary>Earlier implementation records</summary>

- [旧版经验记录](legacy-lessons.md)

</details>

<details>
<summary>Research and audits</summary>

- [研究笔记：Hermes Agent](research/hermes-agent.md)
- [研究笔记：看目录、分头细读](research/memory-navigate-and-read.md)
- [讨论稿：记忆下一步怎么做](research/memory-next-direction.md)
- [研究笔记：现状卡、期限表与自查](research/memory-status-cards-and-self-check.md)
- [现状层问题清单（2026-10-06，待审计）](research/memory-status-layer-audit.md)
- [研究笔记：用途标注与事先备料](research/memory-use-phrases-and-packets.md)

</details>

<details>
<summary>Earlier improvement tasks</summary>

- [任务 D1：待办、想法、项目可以删除](tasks/improvements/D1-delete-a-thing.md)
- [E4 记忆保留原对话上下文](tasks/improvements/E4-memory-evidence-context.md)
- [任务 H1：说了「尽快」的事排进首页时间线](tasks/improvements/H1-urgent-in-timeline.md)
- [任务 L1：资料库的「来源」按出处合并](tasks/improvements/L1-library-sources-by-origin.md)
- [M1 image acceptance expectations](tasks/improvements/M1-images-expectations.md)
- [任务 M1：秘书看得见图片](tasks/improvements/M1-images-for-the-secretary.md)
- [任务 P1：资料多了以后，秘书回话不能变慢](tasks/improvements/P1-recall-speed.md)
- [P2 记忆多了以后，秘书回一句话又变慢了](tasks/improvements/P2-turn-speed.md)
- [Q3 历史对话及时进入记忆提取](tasks/improvements/Q3-conversation-dispatch.md)
- [Q4 后台进程回收与索引持续推进](tasks/improvements/Q4-process-cleanup-and-index-lane.md)
- [Q5 后台在用模型时，秘书偶尔「模型没有响应」](tasks/improvements/Q5-secretary-model-error-under-background-load.md)
- [任务 R1：2026-10-03 检查报告里的九个问题](tasks/improvements/R1-review-1003-fixes.md)
- [任务 T1：Telegram 里的回复不再缺斤少两](tasks/improvements/T1-telegram-replies.md)
- [任务 U1：大的归档分片上传](tasks/improvements/U1-archive-upload-in-pieces.md)
- [任务 W1：副手能联网查资料](tasks/improvements/W1-deputy-web-search.md)

</details>

<details>
<summary>Earlier phase1 tasks</summary>

- [任务 A：修三个硬伤](tasks/phase1/A-fixes.md)
- [任务 B1：撤销基础设施 + 秘书后端](tasks/phase1/B1-secretary-backend.md)
- [任务 B2：秘书前端](tasks/phase1/B2-secretary-frontend.md)
- [任务 C1：提醒通道](tasks/phase1/C1-notify.md)
- [任务 C2：Telegram 双向对话](tasks/phase1/C2-telegram-inbound.md)
- [任务 D1：副手结果自动采纳](tasks/phase1/D1-auto-adopt-backend.md)
- [任务 D2：删按钮](tasks/phase1/D2-buttons-frontend.md)
- [任务 E：黄金路径验收与收尾](tasks/phase1/E-acceptance.md)
- [第 1 阶段：秘书前台 · 任务总览](tasks/phase1/README.md)
- [第 1 阶段接口契约](tasks/phase1/contracts.md)
- [第 1 阶段上线后修复](tasks/phase1/postlaunch.md)

</details>

<details>
<summary>Earlier phase2 tasks</summary>

- [第二阶段：任务入口](tasks/phase2/README.md)
- [任务 S0：骨架（迁移、共用字段、接缝）](tasks/phase2/S0-skeleton.md)
- [任务 A：原话供给、依赖校验、依据标记（后端）](tasks/phase2/batch1/A-raw-text.md)
- [任务 B：撤销连带记忆、清理 2.0 遗留](tasks/phase2/batch1/B-undo-memory.md)
- [任务 C：依据卡片里的原话、「依据已更新」标记（前端）](tasks/phase2/batch1/C-frontend.md)
- [任务 C2：从依据卡片点开，直接看到当时那句话（前端）](tasks/phase2/batch1/C2-source-sheet.md)
- [任务 D：旧测试里写死的日期（测试夹具）](tasks/phase2/batch1/D-date-fixtures.md)
- [第 2 阶段第 1 批：原话直达 · 任务总览与契约](tasks/phase2/batch1/README.md)
- [任务 T：独立验收](tasks/phase2/batch1/T-acceptance.md)
- [任务 E1：抽取出人、地点、时间（后端）](tasks/phase2/batch2/E1-extraction.md)
- [任务 E2：后台队列的优先级、重试和旧资料补做（后端）](tasks/phase2/batch2/E2-backfill.md)
- [任务 E3：对秘书说的话，整理时带上这段对话的上文（后端）](tasks/phase2/batch2/E3-conversation-context.md)
- [任务 M：记忆库（列表、撤销只标记、副手补开、删除收尾）](tasks/phase2/batch2/M-memory-store.md)
- [第 2 批：结构化抽取（契约）](tasks/phase2/batch2/README.md)
- [任务 T2：第 2 批独立验收](tasks/phase2/batch2/T2-acceptance.md)
- [任务 U2：记忆卡片上的人、地点、时间；按人按地点翻（前端）](tasks/phase2/batch2/U2-frontend.md)
- [任务 K：时间轴卡片的内容和「后来怎样了」（后端）](tasks/phase2/batch3/K-timeline.md)
- [任务 Q1：把一句问话拆成条件（纯函数）](tasks/phase2/batch3/Q1-planner.md)
- [任务 Q2：按条件找记忆，接进秘书和副手（后端）](tasks/phase2/batch3/Q2-recall.md)
- [第 3 批：查询规划和时间轴（契约）](tasks/phase2/batch3/README.md)
- [任务 T3：第 3 批独立验收](tasks/phase2/batch3/T3-acceptance.md)
- [任务 U3：时间轴卡片的界面（前端）](tasks/phase2/batch3/U3-frontend.md)
- [任务 E4：导入的聊天记录按整段对话整理（后端）](tasks/phase2/batch4/E4-conversation-extraction.md)
- [任务 I：ChatGPT 历史导入（后端）](tasks/phase2/batch4/I-import.md)
- [任务 L：记下每次调用用了哪些记忆、花了多少（后端）](tasks/phase2/batch4/L-usage.md)
- [第 4 批：评测、花费记录和 ChatGPT 历史导入（契约）](tasks/phase2/batch4/README.md)
- [任务 T4：第 4 批独立验收](tasks/phase2/batch4/T4-acceptance.md)
- [任务 U4：导入 ChatGPT 历史的界面（前端）](tasks/phase2/batch4/U4-frontend.md)
- [任务 V：回忆评测集和三种做法的对比](tasks/phase2/batch4/V-eval.md)
- [第二阶段方向稿](tasks/phase2/direction.md)
- [第 2–4 批：并行方案与数据约定](tasks/phase2/parallel.md)
- [M0：记忆核心与二阶段准备（方案待审）](tasks/phase2/readiness.md)

</details>

<details>
<summary>Earlier phase2 5 tasks</summary>

- [第 2.5 阶段：记忆整理（任务入口）](tasks/phase2_5/README.md)
- [任务 V2：办事评测集](tasks/phase2_5/V2-eval-set.md)
- [任务 O1：整理后端](tasks/phase2_5/batch1/O1-organize-backend.md)
- [第 2.5 阶段第 1 批：分组和类型（契约）](tasks/phase2_5/batch1/README.md)
- [任务 T1：第 1 批独立验收](tasks/phase2_5/batch1/T1-acceptance.md)
- [任务 U1：资料库按分组翻记忆（界面）](tasks/phase2_5/batch1/U1-frontend.md)
- [第 2.5 阶段第 2–4 批：并行方案与契约](tasks/phase2_5/parallel.md)

</details>

<details>
<summary>Earlier phase2 6 tasks</summary>

- [第 2.6 阶段：现状层返工与后台可靠性（契约）](tasks/phase2_6/README.md)

</details>

<details>
<summary>Earlier phase3 tasks</summary>

- [P0 第 3 阶段开工前的六项修复](tasks/phase3/P0-fixes.md)
- [第 3 阶段：工作室（入口与契约）](tasks/phase3/README.md)

</details>

<details>
<summary>Earlier phase3 5 tasks</summary>

- [第 3.5 阶段：事项自动产生（入口与契约）](tasks/phase3_5/README.md)
- [第 3.5 阶段交接（2026-10-08，协调者窗口结束时写）](tasks/phase3_5/handover-2026-10-08.md)
- [第 3.5 阶段独立验收](tasks/phase3_5/phase3_5_acceptance.md)

</details>

<details>
<summary>Earlier phase3 6 tasks</summary>

- [第 3.6 阶段：秘书会收拾（入口与规则）](tasks/phase3_6/README.md)

</details>

<details>
<summary>Earlier stabilization tasks</summary>

- [A1：稳定化集成审查与进入二阶段的判定](tasks/stabilization/A1-acceptance.md)
- [C1: parallel real-backend CI](tasks/stabilization/C1-ci-parallel-real-backend.md)
- [F10：撤销有效期与资料删除后的明确拒绝](tasks/stabilization/F10-expired-undo.md)
- [F11：对话串行、删除传播与失败说明](tasks/stabilization/F11-secretary-repairs.md)
- [F12：Telegram 身份隔离、按钮绑定与语音重试](tasks/stabilization/F12-telegram-repairs.md)
- [F13：落实已确认的逆序撤销规则](tasks/stabilization/F13-approved-undo.md)
- [F14：一句话新建任务并继续加步骤](tasks/stabilization/F14-same-turn-actions.md)
- [F15：连续改时间按服务端接受顺序执行](tasks/stabilization/F15-conversation-order.md)
- [F8 补验：完整真实后端浏览器套件](tasks/stabilization/F8-ci-followup.md)
- [F8：各页面按工作区时区显示](tasks/stabilization/F8-timezone-displays.md)
- [F9：按独立发现修复提醒边界与失败日志](tasks/stabilization/F9-reminder-repairs.md)
- [M0：记忆核心现状与二阶段最小交付](tasks/stabilization/M0-memory-readiness.md)
- [M1：独立调查在最新候选上的代码与最小复现](tasks/stabilization/M1-context-path-audit.md)
- [M2：二阶段范围、透明度与验收评审](tasks/stabilization/M2-phase2-scope-review.md)
- [Q1：快速连续修改的顺序复现](tasks/stabilization/Q1-conversation-order-audit.md)
- [Q2：连续输入顺序独立验收](tasks/stabilization/Q2-conversation-order-acceptance.md)
- [稳定化（第 1 阶段之后、第 2 阶段之前）](tasks/stabilization/README.md)
- [S1：设置开关实际作用清单](tasks/stabilization/S1-settings-audit.md)
- [T1：独立检验撤销与动作序列](tasks/stabilization/T1-undo-tests.md)
- [T2：独立检验提醒与时间](tasks/stabilization/T2-time-tests.md)
- [T3：独立检验秘书、Telegram 与删除传播](tasks/stabilization/T3-secretary-tests.md)
- [T4：独立验证四个已确认用例并汇总](tasks/stabilization/T4-approved-rules-acceptance.md)
- [U1：首页、事项页与记忆回执的体验打磨](tasks/stabilization/U1-ux-polish.md)
- [U2：设置和事项的日常使用体验](tasks/stabilization/U2-settings-things-ux.md)
- [稳定化与二阶段准备：执行分工](tasks/stabilization/dispatch.md)

</details>
