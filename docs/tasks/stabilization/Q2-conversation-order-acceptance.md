# Q2：连续输入顺序独立验收

Sol / high，由发现 Q1 问题且未写实现的执行者承担。独立 `/root/PCAS-wt/Q2` / `test/Q2-conversation-order`，初始基线 `210144a`。F15 独占产品，T4 独占 A1 集成/完整验收，U2 独占页面；本任务只新增 `internal/postgres/conversation_order_acceptance_test.go` 和 `docs/evaluations/2026-10-01-conversation-order-acceptance.md`，不改共享 helper 或任何产品/旧测试。不要把旧 Q1 诊断分支合入新分支，以免无关 opt-in skip 混进默认验收。

先按 [正式契约](../phase1/contracts.md#211-服务端内部调用) 与 [F15任务](F15-conversation-order.md) 写独立预期。可读类型、现有 fixture 和 migration 中可观察的接受票据，不照抄 F15 新自测。产品实现可为独立审查读取，不能从算法反推通过条件。等待作者最终 SHA 后 root 指定集成到此 worktree；产品冲突归作者，不擅改。

最少独立验证：

- 真实 DeskTurn + loopback 假 HTTP 模型，第一轮被屏障阻塞，观察后续票据已提交，三轮模型/保存历史按明确接受顺序，同一事项最终五点。两 Store 共库，规范化 conversation；不靠 goroutine 启动或几次没倒置证明顺序。
- 同 requestId 同正文并发只执行一次、原 conversation 不变，重复等待者取消不终止 creator，正文冲突无动作；不要只统计请求还需查业务状态。
- 排队取消/失效后后继可继续；终结旧请求再发只保存原话、capture回执明确未完成，不调用模型、不重执行旧动作。AutoAccept=true 时没有故障补存来源的后台作业可重新自动创建事项，原文可读取/导出，正常capture仍派生作业。
- 故障补存来源删除后，历史/响应/同键重放均不复活原文。按用户可见结果查，不仅查 ticket 状态。
- 实例/连接丢失的 fence 进行独立实现审查，针对确有未覆盖风险加有限测试；模拟旧deadline要明确为注入历史状态，不宣称等了真实两分钟或杀了进程。不能靠先改掉预期才能通过。

必要时独立检查作者已有 14 等待者 / 异会话进展 / owner 隔离自测是否真覆盖资源上界；不用重复写等价几十个用例。任何确定失败保留证据，先交作者修，再复验，不改产品或弱化断言。

独立 tmpfs PG 可用 33269（旧Q1已清理），httptest loopback随机端口；只合成服务/资料，禁止生产/真实模型/账户/秘密。仅清自己的资源。运行自己的专项 race、相关契约专项及编译检查即可，完整Go/PG由T4最终组合运行。交精确SHA、命令、通过/失败/skip、证据/边界，提交独立测试与报告后停止，交T4指定集成；不合main、不部署、不提前推候选。
