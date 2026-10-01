# 十二叶矩阵：harness 与 compile 记录

仅准备，尚无本矩阵动态结果。初始冻结 905403bf2dbeb8bdc612ea422eda7519ec2b6acd；本次文档含协调者随后批准的 recipient-specific revoke 与先 prepare/完成后变原 route 因果补充，并对齐契约 ba93afc / 2bdf253。原最小复现红报告 4b8957e、原输入/manifest/退出与原 test body 均保留。

编写工作区 `/tmp/pcas-phase2-action-lineage-repro`，产品源码仍精确 `fb1c3eae593527640de156fff806d8789ab5e282`，尚无 025；本次 compile 只证明新测试 Go 可编译，不能证明未来 SQL/行为通过。最后编译的独占测试文件 SHA256 为 `3e6119ea67881a82cd82469f6f1d1addb7b40b4a24166c8bb2f9356d35a1f0c8`。

编译命令 `go test ./internal/postgres -run '^$' -count=1`。编辑基础 harness、真实 studio API/undo 断言、批准的 carried-origin/recipient-specific 因果和最后 target-own-Task 正控后共执行四次 compile gate；四次均 exit 0、`[no tests to run]`。原日志按编写顺序保存在 matrix-compile-1 至 matrix-compile-4-original.log；不是行为测试重跑、没有 skip、没有调用 DB/HTTP 模型。

入口为 `TestPhase2ActionLineageMatrix`，十二叶固定名见冻结表。所有写真实走 Store public API/DeskTurn；SQL 只读 Task/blocks/deps/canonical/audit，唯一 SQL 写为 expiry 叶推进己 schema 的 action_log.created_at，随后由原 flushActionLog 清理既有 undo 窗口。原 C helper/test/gold 未改，产品未改。

original_route_changes 使用两个 synthetic provider ID/model、相同 loopback fake endpoint：秘书 `phase2-origin`，副手/manual `phase2-model`。先完成真实副手结果但不采纳，停止实际 worker 后 prepare waiting manual 与 queued deputy；合法旧包有真实派生正文；只修改秘书 model。前后两目标的完整 ContextRecipient、已准备自身 ContextTask.Recipient 都核对一致。旧包重取、旧 queued dispatch、真实已完成旧结果采纳分别受控拒绝；新三出口仍可供 owner 内容。这个 fixture 不靠目标自身 route 变化制造失败。

revoke_regrant 同一叶先只撤 deputy tuple，核秘书/manual 实际供给与 action.context_stale=false，再撤原 secretary tuple、核 canonical/动作副本/owner 保留，regrant 后拒旧字段。undo 若受控 expired，要求 stable audit ID 在、expired_at 在、changes=[]，并仍核现场 owner 段及三出口无失效派生。实际支持生成字段包含 title/notes/owedTo/waitingFor、task/idea/project notesAppend、动态 check/condition；没有造 progress action。

prepared Run/Task 与真实最终 manifest 的 IDs 用契约 JSON `contextDeskActions` / `desk_actions` 只读核证，引用实际成功 receipt.ActionID，未虚构 Run/origin。positive Indirect 验 exact ref、当前实际 recipient policy ID/revision 与 source scope revision，且 Input 同 ref 不免除 Indirect。promotion 同时核真正已完成/采纳 Run ID 和秘书 action ID 的传播。

正式动态等待 root 指定含 025/迁移配套的组合 product SHA，之后先记录本次 harness SHA 再一次执行十二叶。此前 9480cf2 批次 230 抵达/13 顶层 fail 不覆盖本矩阵；不能拼不同头结果。独立最小红例留在组合回归中，不能替换为弱断言。
