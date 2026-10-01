# F13：落实已确认的逆序撤销规则

执行者 Sol / high。用户于 2026-10-01 确认：同一事项从最后一次修改往回撤销。协调者负责契约与任务文档，不写产品或测试。基线为汇总候选 `59e25daa67df7447f0a09b7f0bd9e5d65edab65e`，工作区 `/root/PCAS-wt/F13`，分支 `stabilization/F13-approved-undo`；PR base 为 `stabilization/acceptance-candidate`。

## 行为与范围

- 对目标动作影响的同一行，有可追溯且尚未撤销的后续动作，返回 HTTP 409 `newer_action`；不按 command / desk / worker 区分。撤销后续动作后可以继续撤销前一个。
- 没有这样的后续动作，但业务内容已经改变，返回 `changed_since`。两个无关事项不能互相挡住撤销。
- 保留 `expired`、`already_undone`、`work_started` 的既有语义及删除传播；禁止为了通过而忽略业务字段或扩大指纹排除项。
- 先在当前候选复现，再做必要的最小修复。核对同轮动作相同事务时间戳下的排序，不能只按时间严格大于判断。

## 文件归属

独占 `internal/postgres/actions_log.go` 及新增 `internal/postgres/approved_undo_test.go`、`docs/evaluations/2026-10-01-approved-undo.md`。静态核查确认基线缺少 newer_action 实现/映射后，协调者追加归属：`internal/workspace/model.go`、`internal/httpapi/server.go`、`internal/telegram/poller.go`、`web/src/store/api.ts` 的错误映射与既定提示；`internal/postgres/actions_log_test.go` 仅调整已记录后续动作对应的旧错误断言；新增 `internal/postgres/migrations/020_action_order.sql` 提供同事务的确定动作顺序。HTTP/Telegram 新回归可用各包独立 `approved_undo_test.go`。不得改 `stabilization_undo_test.go`（归 T4）、秘书产品文件（归 F14）、共享夹具或其他报告。

全包复核后追加：`internal/postgres/auto_adopt_test.go` 中「自动采纳后正常 setNotes」对应的一个旧错误断言改为 ErrNewerAction，业务数据断言保持；新增 `internal/telegram/approved_undo_test.go` 验证回调文案。其他旧断言若冲突逐项报告，不批量替换。

再次复核追加：`internal/postgres/workspace_revision_test.go` 的 TestWorkspaceStaleUndoStillChecksAfterHash 中，公开 setNotes 已产生后续动作，只更新该错误码为 newer_action，原 revision 与业务保护断言保持。

迁移不得用 UUID、ctid 或无序回填假称已知历史先后。历史 created_at、同轮已保存回执顺序是可用证据，确实不可重建的旧并列须保守处理并披露边界；新增事务必须具备精确顺序。先提交兼容设计供协调者审查，再固定迁移细节。既有动作/回执 ID 与幂等语义保持。

兼容设计已审定：新增 nullable action_order，既有行保留 NULL，新行由 sequence 生成，不改 created_at。历史按时间及完整唯一的同轮回执证实顺序；未知旧并列为 changed_since，不把两边都提示成「先撤销另一个」。有可证后续动作时优先 newer_action。补验内容 first→second→first 后仍不能跳撤，保留原候选错误放行的实证。

先读本任务、[正式契约](../phase1/contracts.md)、[执行分工](dispatch.md)；以协调者工作区的最新版为准。测试使用自己的 tmpfs PostgreSQL、假模型/通知；数据库端口由 Docker 分配，HTTP 如需要只用 18152/18153。绝不访问生产、真实模型、秘密或外发通知。只清理自己记录的进程/容器。

交付最小修复、针对性 race 集成证据、make check、独立报告、提交 SHA 与 Draft PR；推自己的分支，不能合 main 或部署。向 T4 提供最终 SHA 后停止写入。T4 将独立验证 U3/U4/U5 和最终同轮 U10，不能把作者自测代替独立验收。
