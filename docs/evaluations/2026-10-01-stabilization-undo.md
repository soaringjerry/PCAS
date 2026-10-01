# T1 撤销与动作序列独立评测

2026-10-01。**发现问题，尚未通过稳定化**。当前是 F7 候选基线上的测试交付，不能代替合并后的 main 验收。

- 分支：`stabilization/T1-undo-tests`；工作树：`/root/PCAS-wt/T1`。
- 候选基线：`e63ee5eb5430ea8b8ebc5cefa766ae806c8fc451`，已含 main `c94b496`；PR base 为 `fix/undo-chain`，依赖 [#20](https://github.com/soaringjerry/PCAS/pull/20)。依赖合并后必须调整 base 并在 main 重验。
- 只新增 PostgreSQL/Telegram 两个 `stabilization_undo_test.go` 与本报告。未修改产品、旧测试、共享 helper、迁移或任务文档。
- 预期来自协调目录最新未推修改的 U1–U16、正式撤销契约及公开类型；读取 schema 和现有夹具，未读取撤销/秘书动作/自动采纳算法来推导预期。

## 覆盖矩阵

| 编号 | 用例与证据 | 状态 |
|---|---|---|
| U1 | 秘书新建带提醒事项，另插入通知 fixture；撤销后事项、嵌入提醒、通知均删除；其他业务表回到初态 | 通过 |
| U2 | 新建、改名、撤销改名、撤销新建；逐步比对业务字段，版本继续增加 | 通过 |
| U3 | 跳过后续改名撤销新建，HTTP 409；业务行、revision、原动作 undone_at 不变 | 待裁定错误码，不能算通过 |
| U4 | 界面命令改名与未记录后台改名分别测试；两者均拒绝且不改变业务状态，后台变化明确返回 changed_since | 用户命令路径错误码待裁定；后台路径通过 |
| U5 | 同对象连续改名 A/B，直接撤销 A 被拒绝且无写入 | 待裁定错误码，不能算通过 |
| U6 | 完成后撤销恢复 todo 和未来提醒；到点 CheckReminders 生成一条通知 | 通过 |
| U7 | 提醒检查和本地假通道成功发送；撤销新建删除事项、提醒、通知，已发送调用保留且没有撤回调用 | 通过 |
| U8 | 本地模型 worker 与手动交接两路径自动采纳 → 撤销 → 手动放回 → 撤销；完成输出保留、副手回到未采纳、样本 1→0→1→0 | 通过 |
| U9 | 自动采纳后勾掉子任务，HTTP 409 changed_since；事项、采纳、文档、样本与通知业务行不变 | 通过 |
| U10 | 合法已有 THIS 对象，同轮 update+add_steps；跳过后续动作被拒绝，逆序撤销逐步正确 | 部分覆盖；原 create+add_steps 无合法当前轮新对象引用协议，待方案裁定 |
| U11 | 同 undo requestId 原信封重放逐字段返回相同 State 且无二次写入；新 requestId 返回 409 already_undone；未知/其他 owner 返回 not_found | 通过 |
| U12 | 删除依赖记忆及来源，快照清空且 expired_at 已写；撤销拒绝，不恢复业务行 | finding T1-U12：错误码 changed_since，应为 expired |
| U13 | 31 天旧动作，分别在后续 flush 清理之前/之后撤销 | finding T1-U13：清理前仍可成功撤销；清理后错误码不符 |
| U14 | SQL queued 且有非零预留费用；撤销删除新建事项、副手并精确释放 run 预留；SQL running 时 HTTP 409 work_started 且业务行不变 | 通过 |
| U15 | 从真实 Telegram 回执取按钮；真实网页命令 HTTP 撤销后点击同按钮，提示“已经撤销过了。”，State 不变且模型只调用一次 | 通过 |
| U16 | 固定种子 1–50，各 20 次合法白名单操作；逆序每一步及终态与开始业务字段一致 | 50/50 组通过，1000/1000 次逆序撤销与逐步 oracle 检查通过 |
| 旧 action_log | 按迁移 016 历史算法写入全 document 指纹：未变更动作可撤销；仅 recordVersion 变更和真实标题变更分别拒绝且无写入 | 兼容边界实测通过；不宣称旧簿记已变化动作被修复 |

U3/U4/U5 错误码重叠尚未经用户裁定，测试不把 observed `changed_since` 当作通过或 finding。U10 跳序码同样保持待裁定。安全断言执行完再 `t.Skip("pending …")`，因此待裁定不是被隐藏的产品失败。

## 独立随机 oracle

每个种子先建立已有事项、用户文档与完成未采纳的副手结果。20 次操作由六个必含族（新建、改名、完成/恢复、加步骤、采纳、文档编辑）随机排列，再加十四次随机合法操作组成；重复采纳族改为设置说明，避免生成非法重复采纳。种子固定为 1–50，目标按测试模型的插入序号选择，不依赖数据库结果顺序。每次撤销之前的预期来自操作前保存的 SQL 业务行，完全不使用 action_log.before 或当前指纹作 oracle。

比较 `work_items`、`work_documents`、`agent_runs`、`training_samples` 与 `workspace_notices` 的全部行和列，包括关系、状态、内容、预留费用、创建时间及样本状态。只去掉契约 §1.2 明列的根级键 `recordVersion`、`updatedAt`、`history`、`evolution`、`sources`，以及 SQL 的对应 `version`/`updated_at` 列。样本业务 `document.version`、`createdAt`、嵌套业务字段仍比较；不把所有时间、所有 ID 或采纳状态都排除。失败/详细模式输出种子及完整动作序列；每一步和最终状态均检查，不只确认“没有报错”。

## 明确发现与保留证据

### T1-U12：删除失效快照返回旧错误码

复现：capture → 接受为 memory → 新建事项并附其 SourceRef → setNotes → deleteMemory(includeSources=true) → 撤销 setNotes。

删除后 `changes=[]` 且 `expired_at IS NOT NULL`，未恢复私密内容。严格 HTTP 断言实际失败：

```text
U12 requires HTTP 409 expired; got 409 {"error":"changed_since"}
```

明确契约已要求相关资料删除后返回 `expired`；这是错误码问题，删除安全断言通过。默认用例先执行快照失效与不写入断言，然后以 finding 暂存 skip，严格 expired 断言保留。

### T1-U13：保留期仅在后续写入清理，直接撤销未拦截

复现：新建事项后把该 owner 动作 `created_at` 设为 31 天前；分别直接撤销，以及先执行另一个可记录动作触发清理再撤销。

```text
cleanup=false: U13 requires HTTP 409 expired; got 200
  response.tasks=[]; response.revision=2  （旧事项实际被删除）
cleanup=true: U13 requires HTTP 409 expired; got 409 {"error":"changed_since"}
```

两个路径都违反已明确的 `expired` 规则，第一条还违背超过保留期拒绝撤销的要求。保留两个严格测试，仅在默认运行暂存 finding skip。

重新实测发现而不更改断言：

```sh
PCAS_TEST_DATABASE_URL='<独占临时库URL>' PCAS_STABILIZATION_UNDO_RECHECK=1 go test -race -count=1 ./internal/postgres \
  -run '^TestStabilizationUndoU(12_|13_)' -v
```

### U10 能力缺口（不列为产品 finding）

最初用 create_task + add_steps(ref=R1) 的本地模型得到新建成功、加步骤跳过“找不到要改的那件事”。协调者核对 B1 文档确认 R1 只引用上下文已有对话事项，当前轮新建对象没有已定义引用协议。因此该探针不是合法协议下的失败：未写入错误预期、未猜另一种别名、未伪造 action_log。最终测试只通过已有 THIS 的同轮多动作验证原子状态与逆序撤销；原序列仍缺少合法触发方式，不能称 U10 通过。

## 验证与隔离环境

测试容器 `pcas-test-T1-undo` 使用 pgvector PostgreSQL 16，数据 tmpfs 1 GiB，WAL 上限/下限 128/32 MiB，Docker 分配 localhost 随机端口。PostgreSQL 用既有唯一 schema 夹具；Telegram 用同一独占测试库上的随机 owner 与临时 blob 目录。模型、Telegram bot、通知通道均为本地假服务；没有调用真实账号，没有连接、重启或修改生产或其他项目容器。

| 命令 | 实际结果 |
|---|---|
| `make check` | 最终测试文件上的 fmt-check、go vet、race 单元检查、build 通过；该命令没有传 DB，数据库证据使用下列独占库命令 |
| `PCAS_TEST_DATABASE_URL='<独占临时库URL>' make test-integration` | 最终文件 PostgreSQL 全量 race 集成通过，118.059s；7 个 pending/finding 叶 skip 仍保留 |
| `PCAS_TEST_DATABASE_URL='<独占临时库URL>' go test -race -count=1 ./internal/postgres -run '^TestStabilizationUndo' -v` | 专项通过，60.974s；U16 种子 1–50 全部通过，7 叶 skip；随后 worker 子路径也纳入上面的最终全量集成并通过 |
| `PCAS_TEST_DATABASE_URL='<独占临时库URL>' go test -race -count=1 ./internal/telegram -v` | 最终 Telegram 全量通过，2.341s，含真实 PostgreSQL U15；无该包专项 skip |
| 上述 U12/U13 strict recheck 命令 | **按预期失败**，三个叶路径均复现明确 finding，未降低断言 |
| `git diff --check` | 通过 |

`<独占临时库URL>` 必须替换成自行建立的临时库 URL；不设置此变量会跳过数据库测试，不能作为复现或验收证据。初始严格专项日志保留于执行者私有 `/tmp/pcas-T1-initial.log`，修正夹具后确认 U12 失败于 `/tmp/pcas-T1-fixture-followup.log`，最终严格复验为 `/tmp/pcas-T1-strict-findings.log`；关键输出已收录本报告，临时日志不作为唯一证据。

资源清理已完成：`docker rm -fv pcas-test-T1-undo` 成功；只有自有 tmpfs 测试容器及其卷被移除。没有启动持久后台测试服务；httptest 服务与临时 blobs/schema 由测试 cleanup 清理，所有测试会话均已退出。

## 局限与验收边界

- 存在 4 个 pending 叶用例（U3、U4 用户路径、U5、U10）及 3 个 finding 叶用例（U12、U13 两路径），**未通过稳定化**。
- U8 包含本地假模型的真实 runAgentOnce worker 路径及手动交接路径；不代表真实模型账号或副手线上服务验收。
- U16 在已完成未采纳副手基线上随机手动采纳，检查副手、文档和训练样本恢复；没有把 requestRun 或 pasteRunResult 放入可撤销白名单，也不对不可撤销费用做“回到零”的断言。
- U14 的 SQL 队列事实为 queued，公开 Snapshot.run.Status 当前呈 running；本测试按契约 SQL 开始执行边界验证撤销，并没有把显示状态当作 worker 已启动。费用检查精确减少副手 reservation，秘书已消费模型费用仍保留。
- U12 是最小撤销安全用例；完整来源删除传播由 T3/D3 负责。Telegram 重复 callback/故障恢复等由 T3 负责，不在本文件再造整套夹具。
- 历史 full-document 指纹只验证三种清楚边界，没有测试或承诺修复所有已被簿记污染的历史快照。
- 本次不进行 main 合并、部署、真实通道/设备实测或用户一天试用；这些验收条件仍未完成。
