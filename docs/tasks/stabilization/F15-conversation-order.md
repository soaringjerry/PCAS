# F15：连续改时间按服务端接受顺序执行

> **Historical task record.** This text records an earlier phase or investigation.
> Use the [current documentation](../../README.md) and [project status](../../status.md) for new work.
> Its old assignments, limits, and precedence statements do not govern current development.

执行者 Sol / high。用户要求完成阶段一打磨，并授权无冲突并行；Q1 已实际复现三轮改时间倒置，root 安排修复。root 不写产品或测试。本任务不是开始第二阶段。

基线后端组合 `210144a93753bed736d2d5561a454f8d07185eb0`，工作区 `/root/PCAS-wt/F15`，分支 `stabilization/F15-conversation-order`。先读 Q1 独立证据 `/root/PCAS-wt/Q1/docs/evaluations/2026-10-01-conversation-order-audit.md`（98b9a01）、正式契约、F11 与其真实并发测试。Q1 已停，不与其共写；U2 独占页面，T4 独占集成候选与独立 UX 测试。

## 行为要求

同 owner / 规范化 conversation 的三个已接受请求依次是「三点开会」「改四点」「最后改五点」，模型调用和保存轮次必须按这一顺序，最终同一事项为五点。互斥不足以代替顺序。共同接受点必须由服务端给出且可验证；不能用客户端 goroutine 启动、HTTP 原始抵达或 UUID 字典序声称跨实例全序。无共同先后证据的同时到达请求不承诺网络全序。

保留 F11 的跨 Store / 多实例串行、不同 conversation 可继续、无连接等待、最多 9 个生成事务为 10 连接 pool 留嵌套 Recall / budget 余量。不得只换随机退避、仅用进程内 map mutex，或用长期持有 owner 锁阻塞其他会话。若调整资源预算，先提供证据和完整取舍。

请求重放和冲突保持既有契约：同 owner / requestId / 内容只能执行一次，正文冲突拒绝；相同请求重试不生成第二个排队位置或抢到更前位置。保持上下文撤回、原话保存、动作原子性、取消/超时/模型错误的现有契约，不用排队故障掩盖业务失败。排队前/中/正在执行的取消需区分，未完成不能伪称成功；头部失败、连接断开或实例死亡不能永久阻塞后续。不得增加含原话的第二份持久队列而遗漏删除传播。

## 先交设计，再实现

先在自己的报告写简短设计并发给 root：接受点与排序、同 requestId 的并发/重试、取消/错误/重启、连接/slot 上界、数据保留和删除传播；列出所需文件/迁移。对比最小阻塞锁方案与数据库票据方案，明确哪些保证有证据、哪些需实测。root 明确固定内部契约后再写产品，不向用户询问数据库细节。优先最小足够实现，不添加通用任务调度平台或新的用户设置。

## 文件归属

2026-10-01 root 已审查设计并固定最小实现：数据库仅元数据票据、commit 接受点、队首进入既有 slots 且持执行锁后二次核查；终结同键重试只补存原话并给明确未完成的 capture 回执，不迟到重执行。Q1 独立代码复核确认可同事务撤掉此次新建来源尚未发布的 queued chunk 作业，保留原文与 unknown 候选，避免自动提取旧指令；不要用 blocked 伪装配置故障再给隐含重新执行的“重试”。必须检查 ingest Duplicate 与精确作业范围。正式边界见 [2.1.1 契约](../phase1/contracts.md#211-服务端内部调用)。已授权完整产品实施，无需再次等待用户或 root 确认。

预留 `internal/postgres/desk_turn.go`、新 `internal/postgres/desk_turn_order.go`、自测 `internal/postgres/desk_turn_order_test.go`，必要时 `internal/postgres/database.go` 与迁移 021（路径/编号先按仓库核查），以及 `docs/evaluations/2026-10-01-conversation-order-repair.md`。共享类型/接口、其他测试或生产文件需先报告 root 统一归属。原 Q1 诊断测试和既有 F11 测试不改；可读取作为独立证据，不能降低预期。不得修改前端、Telegram、撤销逻辑、任务文档或 A1 工作区。

故障补存使用专属 `desk-incomplete` connector，隔离普通 capture 的来源 identity，异常 Duplicate 且无已保存 turn 时真实冲突，不抑制别人的已发布作业。已授权 `internal/postgres/editing.go` 原 `('desk','capture')` 白名单加入该 connector 及紧邻注释，确保删除原话传播到 turn/response/回执；必须专项验证重放不复活。除这个单点外不调整删除机制。

## 验证与交付

自有 tmpfs PostgreSQL，端口可用 33270，loopback 假模型可 httptest 随机端口，HTTP 固定需要时 18160/18161（Q1 已释放）。全为合成数据，不用生产/真实账号/真实模型/通知，只清理自己的进程容器。

基于真实 DeskTurn + 假 HTTP 模型覆盖三轮明确接受顺序、两个 Store 共库、同请求重放/冲突、超过 9 个同会话等待者下另一个会话仍能前进、不同 owner 隔离、队头/队中取消、模型错误、连接丢失和重启语义。有限屏障，不靠一次没倒置就声称 FIFO；证明最终业务状态/保存历史，不只看锁函数调用。已有并发/删除/取消回归必须保持，运行 make check、相关数据库 race 测试并报告精确结果；最后 T4 对组合候选运行必要全量验收。

交独立提交和报告，不自行合入候选/main，不部署。只在 root 指定后开 Draft PR（base acceptance-candidate）。完成后停止写入，交回所有权。root 将另请非实现者独立验收。
