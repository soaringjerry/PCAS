# Q2：连续输入顺序独立验收

2026-10-01。独立执行者 Sol / high，也是 Q1 finding 的发现者，未编写 F15 产品实现。工作区 `/root/PCAS-wt/Q2`，分支 `test/Q2-conversation-order`。初始基线 `210144a93753bed736d2d5561a454f8d07185eb0`；依 root 指令无冲突合入作者最终提交 `9b177007c63d87cf289ecadb90a5b21238890c4b` 后执行本验收。

## 结论

针对正式阶段一契约 §2.1.1 的五组独立验收全部通过：明确接受顺序的三轮按 1→2→3 调用真实 HTTP 假模型并保存历史，同一事项最终为五点；同键并发、重复等待者取消和正文冲突保持一次执行；排队 creator 取消后的旧键重试只保存原话，不迟到执行或后台自动整理；原话可读取/导出，删除后历史、HTTP/Telegram 同键重放和导出不复活；仍运行的旧事务不会被过期时间越过，真实连接终止后新的 Store 可以推进，旧模型迟到完成不能提交业务。

新增 `internal/postgres/conversation_order_acceptance_test.go` 是默认 PostgreSQL 验收测试，仅沿用 `PCAS_TEST_DATABASE_URL` 的集成测试开关。它没有 Q1 的诊断 opt-in/预期失败，不合旧 Q1 分支。独立预期与主体代码在合入 F15 前按最新正式契约编写，未读取、照抄或调用作者新 `desk_turn_order_test.go`。使用的既有 fixture 是原来的 testStore/secretaryModel/turnRequest/workspaceCommand。测试只调用真实 DeskTurn、HTTP 路由及公开读取/导出/删除路径；业务成功、时间、目标、回执和删除结果并非根据锁算法反推。

## 独立断言和可复核证据

| 独立用例 | 观察和用户可见 oracle |
|---|---|
| `CommittedOrderAcrossStores` | 第一次模型真实调用被屏障阻塞；第二票据已在独立连接可见后才提交第三。两个 Store 混用大小写 conversation；commit 后序号 `[1,2,3]`、规范化 conversation 相同。模型和已保存历史均为「三点开会、改四点、最后改五点」，后轮提示分别含 15:00/16:00，三个 done 回执指向同一事项，最终 `2026-10-02T17:00:00Z`。 |
| `SameKeyNullConversationAndDuplicateCancellation` | null conversation 的 creator 模型阻塞时，取消重复 waiter；公开 pgx QueryTracer 观察其真实 DeskTurn 的票据 readiness 查询完成后才取消。票据/conversation/deadline 不变，creator 不受影响。另一成功重复调用也在服务端 readiness 查询完成后才释放 creator，两个响应返回同一 turn/conversation。正文变化在执行期间及完成后均 conflict，只有一条任务、一条 turn 和一次模型调用；修改当前 State 后重放带当前 State。 |
| `TerminalRecoveryPreservesRawWithoutAutomaticActions` | 已接受的排队 creator 被取消，后继五点已经完成；再用旧原话同键经真实 HTTP 请求恢复为 200/capture/done，回执明确“未完成”，无 actionId/thingId、不可撤销，模型调用不增加、同一事项仍五点。AutoAccept=true。`desk-incomplete` 原文精确可读且存在 full export，unknown 候选等待明确用户处理，来源显示“原文已保存”，该来源没有任何后台 job。事先普通 capture 使用相同 external request ID，其原 queued chunk 作业仍在，来源 identity 分离。再次同键恢复重放同一个 turn。 |
| `DeletedRecoveryDoesNotResurrectThroughHTTP` | 删除补存原文来源并阻止重导入后，GetSource 为 not_found；HTTP 历史与 POST 同键重放、Telegram 的 DeskTurnByRequest、完整 export 均不包含原文标记。durable question/answer 为空，response 已清理；重放保留同一 turn ID 及 capture/done 回执骨架，回执内容是“（内容已删除）”。没有模型再调用，五点事项不受影响。 |
| `ConnectionLossAndExpiredHeadFence` | fixture 先持 conversation 执行锁，让第一票据 commit 后仅把其 metadata deadline 注入为 1 秒，再释放 fixture 锁。第一真实模型已进入但继续阻塞；期限到后后继已接受，仍可观察旧 ticket pending 且后继未调用模型。用精确 request advisory key 定位并终止自己测试的 PG backend。新的 Store 在旧模型仍阻塞时成功完成五点事项并使旧票据 expired；释放旧模型后，旧 DeskTurn 真实返回错误、不能提交四点事项。同键旧请求此后只补存原话/capture，不调用模型，来源无后台作业。 |

QueryTracer 只用于确认重复调用已经走到服务端等待检查，不阻塞/改写产品查询，也不伪造 SQL 结果。其他顺序观察直接用独立数据库连接读取 commit 后的 admission_order；没有把 goroutine 启动、HTTP 网络到达或 UUID 排序当 FIFO 证据。

最终关键日志：

```text
accepted_orders=[1,2,3] model=[三点开会 改四点 最后改五点]
history=[三点开会,改四点,最后改五点] final_due=2026-10-02T17:00:00Z
terminal recovery HTTP=200 source=7fab1bc1-d119-4011-9ab9-13f182fbd554
raw_read/export=true jobs=0 ordinary_capture_jobs=1
model=[Q2阻塞三点 Q2新意图五点] final_due=17:00
injected deadline + actual own backend loss pid=206;
fresh Store advanced while old model was held;
late old commit failed; final_due=17:00
```

## 相关回归与实现只读复核

另运行原有 F11 并发/取消以及原话来源删除、S8 模型失败分类契约测试：7 个顶层、12 个叶全部通过，零 fail/skip。其中原有跨实例测试启动 16 个等待者，验证两个 Store 共库时同 owner/其他 owner 的独立 conversation 能前进；不同会话容量测试确认 9 个生成事务仍为嵌套 Recall/budget 与 Snapshot 保留数据库连接。没有把原有 goroutine 启动顺序当成全部等待者已 commit 的顺序证据，也未重复编写一批等价作者用例。

只读审查 F15 产品/迁移所得：admission 请求锁保护同键，独立 conversation admission 锁覆盖序号分配和 commit；队首 readiness 在 slot 前短查询，slot+执行锁后再次核查状态/队首；业务、desk_turns 与 ticket done 在同一事务提交；错误清理限定 creator_id 且 status=pending；expire 路径先取得 conversation 执行锁，所以不能跳过仍活跃事务；固定初始 expires_at 没有重试延长期限路径。故障补存 connector 为 `desk-incomplete`，Duplicate 会冲突而非删除旧作业，新建 queued/attempts=0 作业精确按 owner/source/version 撤除；editing.go 的来源删除白名单包含该 connector。没有发现与此次契约闭环冲突的确定遗漏；这些代码路径不是替代独立行为验证的验收结果。

## 命令、结果与范围

```sh
PCAS_TEST_DATABASE_URL='postgres://q2:q2-acceptance-local@127.0.0.1:33269/q2_order?sslmode=disable' \
go test -race ./internal/postgres -run '^TestConversationOrderAcceptance_' -json -count=1

PCAS_TEST_DATABASE_URL='postgres://q2:q2-acceptance-local@127.0.0.1:33269/q2_order?sslmode=disable' \
go test -race ./internal/postgres \
  -run '^(TestStabilizationS1_|TestStabilizationS8_|TestSecretaryOriginalSourceDeletionScrubsHistoryAndReplay)' \
  -json -count=1

go test ./... -run '^$' -count=1
go vet ./internal/postgres
git diff --cached --check
```

- 独立最终 race：5 个顶层/5 个叶，5 pass、0 fail、0 skip，包耗时 **4.451 秒**，未报告 DATA RACE。
- 相关契约 race：7 个顶层/12 个叶，0 fail、0 skip，包耗时 **9.926 秒**，未报告 DATA RACE。
- 全 Go 编译检查通过；`-run '^$'` 没有执行行为测试，不算全量行为验收。postgres vet、最终 staged diff 检查通过。
- 首次独立 race 就是 5/5 pass（4.666 秒），没有产品或 fixture 失败。随后补强两个重复 waiter 的服务端 readiness 观察及来源必须出现在可见 State 的断言，重跑确认；保留中间输出，不调低 oracle，不改作者产品或旧测试。
- 证据归 Q2：`/tmp/pcas-q2-order-evidence/independent-final.jsonl`、对应 `.log`、`related-contract.jsonl`/`.log`、`compile.log`、`vet.log`。`independent-initial.jsonl` 与 `independent-before-successful-witness.jsonl` 保留补强前的通过记录。

没有运行作者新测试或完整 PostgreSQL 套件，没有验证真实模型/账号/通知/前端浏览器；完整最终组合验收归 T4。期限测试是显式注入 metadata、保留活跃模型屏障并实际杀掉一个自有 PG 连接，不声称等待真实两分钟或杀过服务进程。fresh Store 验证可共享数据库推进，但不等同于完整服务重启恢复。没有正文 worker 自动恢复承诺；故障原话仅在同键调用者重试时保存。单个明确接受序列加代码 fence 检查证明本用例满足契约，不以几次未倒置推导任意调度公平性。

## 资源与交付归属

本任务新增且仅交付 `internal/postgres/conversation_order_acceptance_test.go` 与本报告。F15 产品通过 root 指定最终 SHA 的 merge 进入独立验证分支，没有拷贝作者在途工作；没有修改原 Q1 worktree，也没有合入旧诊断、A1、main 或部署。测试/报告独立提交交 root/T4 指定集成。

Q2 独占 Docker container `pcas-q2-order-pg-20261001`，镜像 `pgvector/pgvector:0.8.2-pg16-bookworm`，tmpfs `/var/lib/postgresql/data`，PG loopback `127.0.0.1:33269`，合成数据库 `q2_order`/临时公开凭据；所有测试按已有 helper 使用独立 schema 并自动删除。假模型 HTTP 使用 httptest 分配的 loopback 临时端口并自动关闭。只终止本测试 request advisory key 证明属于自己的 PG backend，未触碰其他进程/容器/数据库。

完成后已删除自己的 container，释放端口与 tmpfs 数据；保留 Q2 worktree、独立分支及上述证据供 T4 复核。本次验收停止，最终候选集成与整体结论由 root/T4 决定。
