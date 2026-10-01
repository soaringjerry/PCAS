# F13：已批准的同事项逆序撤销

2026-10-01。基线 `59e25daa67df7447f0a09b7f0bd9e5d65edab65e`；独立工作区 `/root/PCAS-wt/F13`，分支 `stabilization/F13-approved-undo`。正式行为依据为协调工作区最新版 phase1/contracts.md §1.4（含历史顺序兼容边界）。本报告是实现者证据；T4 的独立验收另行交付。

## 基线观察与最小修复

实际执行新增专项前，三种来源 command / desk / worker 的已记录后续 rename 都使早动作返回 `409 changed_since`，缺少正式要求的 `newer_action`。同一个 PostgreSQL 事务中的三个独立动作把标题依次改为 first、second、first，时间戳相同；跳过两个后续动作撤销第一个竟返回 HTTP 200。业务指纹相等不足以保证逆序安全。原始作者失败日志 `/tmp/F13-baseline.log`。无关事项夹具首次复用已删除 UUID 碰到既有 version_conflict，后改为新 UUID；这是夹具问题，不是产品 finding。

修复只增加后续动作判定与错误映射，并保留原指纹、业务行恢复、版本增长、删除传播和副手保护。目标已撤销 / 已过期仍先返回 already_undone / expired；尚有相同行后续动作先返回 newer_action，撤销后续后再判原 work_started 和 changed_since。相同行按 changes 的 table + id 判断，查询及历史回执均限定 owner，不按操作者来源区分。已经撤销的记录、已清空 changes 的过期记录不阻挡。

新增 `workspace.ErrNewerAction`、HTTP 409 映射、网页提示和 Telegram 回调提示「后面还有改动，请先撤销它」。两处 actions_log_test.go、一处 auto_adopt_test.go 和一处 workspace_revision_test.go 的已记录后续业务修改改为期待 ErrNewerAction；原数据保护断言保留。未改 T4 所属的 stabilization_undo_test.go 或 F14 的秘书实现。

## 持久顺序与历史兼容

新增迁移 `020_action_order.sql`：action_log.action_order 为 bigint；新行默认从专属 sequence 取得值，有唯一索引和 owner 待撤销顺序索引。created_at 原样保留，包括同事务并列。sequence 的数值可有回滚空缺，比较先后不要求连续。

检查了全部产品 flush 入口：Execute 在 workspace_owners 行锁内；DeskTurn 在同一 owner 锁内的 action 子事务；worker 的 runAgentOnce 在完成 / 自动采纳事务先取得 owner 锁，调用 autoAdoptRunTx 后 flush；命令触发的自动采纳继承 Execute 锁。故同 owner 的新动作 insertion order 与已串行的实际执行顺序一致。worker claim 的状态更新不记录 action_log，原 run 行锁仍负责防止撤销已开始工作。

历史行保留 action_order NULL，迁移不以 UUID、ctid 或无序回填假装恢复真实先后。历史行之间先按 created_at 兼容比较；时间并列且属于同一 desk turn 时，只有完整、无重复 actionId 的保存回执能证明先后。回执缺失、重复或未知旧并列保守返回 changed_since，避免互相提示先撤销另一条的死循环；若另有可证明的后续动作，仍优先 newer_action。所有新动作均在迁移前历史动作之后。

**历史限制**：PostgreSQL now() 是事务开始时间；历史不同事务等待 owner 锁时，created_at 不一定代表实际执行顺序。历史时间兼容不能提供新 sequence 的同等级保证，也不能承诺修复全部旧撤销链。迁移前旧全文指纹经簿记变更失配的既有边界仍保留，未扩大忽略字段。

## 已执行证据

自有 tmpfs PostgreSQL 容器 `pcas-test-F13-approved-undo`，pgvector 0.8.2 / PostgreSQL 16，localhost Docker 随机端口 33267；数据 tmpfs 1 GiB，WAL 128 / 32 MiB。每个测试独占 schema 并清理。假模型配置 / Telegram 假 Bot，不访问真实账户、生产或秘密；没有外发通知。

- `PCAS_TEST_DATABASE_URL=... go test -race -count=1 ./internal/postgres ./internal/telegram -run '^TestApprovedUndo|^TestAutoAdopt' -v`：通过。作者新增 PG 六个顶层用例、十二个叶用例及 Telegram 一例零 skip；并验证自动采纳全部去向和失败回滚。日志 `/tmp/F13-targeted-final.log`。
- 新增用例覆盖：三种来源统一 newer_action；未记录外部业务变化 changed_since；无关事项互不阻挡；同事务三个相同 created_at 动作，业务内容恢复原值仍不可跳撤；逆序全部成功且 already_undone 保留；历史不同时间 / 完整回执成功逆序；缺失 / 重复 / 未知并列 changed_since；历史未知并列与已知新后续同时存在时 newer_action 优先；新顺序迁移旧表不改变既有审计 / 快照、不伪回填且可重复执行；后续动作 → work_started → expired → already_undone 的保护顺序。
- `make check`：最终代码通过 fmt-check、go vet、Go race 单元测试和构建；此命令未设置数据库 URL，不冒称数据库测试执行。日志 `/tmp/F13-check-final.log`。
- 完整 `go test -race -count=1 ./internal/postgres ./internal/telegram` 已运行一次：Telegram 通过，PG 140.403s 后仅有三处旧错误码断言失败（auto_adopt、U9、workspace_revision），实得 newer_action。auto_adopt 与 workspace_revision 经授权只改错误码后专项通过；U9 归 T4，作者保持文件未改。完整日志 `/tmp/F13-integration.log`。不把这次单独分支完整测试写成通过；最终 F13/F14/T4 合验由 T4 运行。
- 四个旧断言所在专项及五字段指纹保护：`go test -race -count=1 ./internal/postgres -run '^(TestWorkspaceStaleUndoStillChecksAfterHash|TestUndoRejectsContentChanges|TestUndoLegacyFingerprint|TestUndoIgnoresOnlyBookkeeping)$' -v` 通过，日志 `/tmp/F13-compatibility-final.log`；auto_adopt 的最终通过证据已包含于上方全去向专项。改动仅适配新合同，不降低业务拒绝或版本保护。
- 基线继承的 U3/U4-user/U5/U10 pending 由 T4 在独立测试中移除与验收，作者没有把继承 skip 算作通过。

## 交付与边界

Draft PR base 为 `stabilization/acceptance-candidate`。最终提交和 PR 链接由交接消息提供。产品不合 main、不部署。作者专项不替代 T4 的 U3/U4/U5/U9/U10 和最终 F13/F14 集成验收。未运行真实通知、真实账户、线上 11 项或一天用户试用；没有更改二阶段入口条件。

自有容器已用精确名称 `docker rm -f pcas-test-F13-approved-undo` 删除，tmpfs 数据随容器清理；所有自有执行会话已结束，没有后台 HTTP / 模型进程。原始日志保留于上述 `/tmp/F13-*.log`。作者交最终 SHA 后停止写入。
