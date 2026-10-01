# F10 撤销有效期修复评测

2026-10-01。F10 修复 T1-U12/U13 的三个明确失败路径；未合并 main、未部署。其余待裁定撤销语义继续保留，不能据此称稳定化阶段通过。

## 输入与范围

- 输入 F9：`26dba8672b578cf1baadd71c0ba257f7691d8109`（[#25](https://github.com/soaringjerry/PCAS/pull/25)，已含 F7/T2）；输入 T1：`6d0c27e70ede296cb0247151ffc31f239051de3a`（[#26](https://github.com/soaringjerry/PCAS/pull/26)，已含 F7）。两位执行者已停止写入。
- 两输入无冲突合并并推送 `stabilization/expiry-candidate`：`912ffad5189d73dd75fc09555d080c1b8d3609eb`。修复分支 `stabilization/F10-expired-undo`，PR base 为该候选，依赖 #20/#24/#25/#26；依赖合并后仍须对 main 重新验收和调整 base。
- 产品只改 `actions_log.go` 的撤销入口判定、`workspace/model.go` 新增 `ErrExpired`、HTTP 错误映射、前端错误文案，以及 Telegram 的 expired 文案分支。
- 测试接手 T1 PostgreSQL 文件，仅移除 U12/U13 finding skip 与已无调用的专用 finding helper，保留原独立 expired/业务状态断言及四个 pending 叶用例；新增精确有效期和已撤销重叠边界。未改 T1 原报告、任务文档、迁移、共享 helpers 或其他执行者的文件。
- 协调者额外批准旧 `undo_test.go` 两处断言及 Telegram `TestUndoCallbacks` 的一个错误映射用例，逐项说明如下。

## 原始失败与修复

先在候选原代码上执行（DB URL 为自有临时库）：

```sh
PCAS_TEST_DATABASE_URL='<F10 临时库 URL>' PCAS_STABILIZATION_UNDO_RECHECK=1 \
  go test -race -count=1 ./internal/postgres -run '^TestStabilizationUndoU(12_|13_)' -v
```

三个叶路径均真实失败，关键输出：

```text
U12 requires HTTP 409 expired; got 409 {"error":"changed_since"}
cleanup=false: U13 requires HTTP 409 expired; got 200
  tasks=[]; revision=2  （超过 30 天的事项实际被撤销删除）
cleanup=true: U13 requires HTTP 409 expired; got 409 {"error":"changed_since"}
```

原始日志 `/tmp/pcas-F10-original-findings.log`；关键事实已写入本报告，不依赖临时日志作为唯一证据。

撤销入口在锁定该 owner 的动作记录时同时读取是否 `expired_at IS NOT NULL`，或 `created_at < now() - interval '30 days'`。尚未撤销且满足任一条件时，在解析快照、锁定业务行和恢复内容之前返回 `workspace.ErrExpired`；HTTP 映射为 `409 {"error":"expired"}`。无需等待下一次 flush 清理快照。前端及 Telegram 同用契约文案「超过 30 天或相关资料已删除，无法撤销」。既有删除传播、快照清理、审计元数据及指纹算法保持原实现。

判定顺序经协调者确认：合法 ID 与 owner 范围查找 → 不存在 `not_found` → 已撤销 `already_undone` → 有效期/资料删除 `expired` → 既有 `work_started`、`changed_since` 校验和恢复。输入实际曾先判断 expired_at 并返回 changed_since；本次依据 U11 修正「已撤销后再清理快照」重叠情况，未宣称这是输入原本已有的顺序。原 requestId 的命令幂等检查仍在撤销入口外，不修改命令重放实现。

## 覆盖与批准的断言调整

| 验证 | 结果与范围 |
|---|---|
| T1-U12 | 删除来源后 changes=[] 且 expired_at 已写；HTTP 409 expired；业务行逐字段相等，不恢复已删内容 |
| T1-U13 两路径 | 31 天旧动作在后续清理之前、之后都 HTTP 409 expired；业务行不变 |
| U13 精确边界 | 30 天少 1 微秒、恰好 30 天可撤销；多 1 微秒返回 ErrExpired 且业务行不变。用同一 PostgreSQL 事务固定夹具与入口的时钟，无 sleep、无生产时钟接口 |
| U11 重叠边界 | 已撤销动作被改为 31 天旧，分别清理前/后验证：原 requestId 重放成功、当前 workspace 无变更时 State 与首次结果完全相等；新 requestId HTTP 409 already_undone，直接 Undo 亦返回 ErrAlreadyUndone；业务行与完整 Snapshot 不变 |
| 既有 U11 与兼容错误 | 未知/其他 owner 保持 not_found；原请求幂等、普通内容冲突、work_started 保持原断言 |
| Telegram 文案 | `TestUndoCallbacks` 的新增 ErrExpired 行验证正式文案；其余 changed_since/work_started/already_undone/not_found 用例保留 |
| F7/F9 | 连续逆序撤销、旧指纹三种兼容边界、R5 旧提醒抑制/重启/连续撤销、未来提醒与普通补发均纳入完整数据库回归 |
| T1-U16 | 固定种子 1–50、每组 20 次操作及逆序恢复，沿用 T1 独立 oracle |

旧断言调整均在修改前逐项获协调者批准：

1. `TestDeletionExpiresItemAndDocumentSnapshots`：itemAction/docAction/docEdit 已因删除传播设置 expired_at 且快照为空，预期由 ErrChangedSince 改为 ErrExpired；正文删除与无关动作可撤销的断言保留。
2. `TestActionSnapshotRetentionKeepsAuditMetadata`：清理后尚未撤销的 created 预期 ErrExpired，已撤销的 edited 预期 ErrAlreadyUndone；原审计元数据、undone_at、其他 owner 隔离和无变化 flush 断言保留。未对其他 changed_since 断言作全局替换。
3. `TestUndoCallbacks`：仅增加 ErrExpired→正式文案一行，不修改其他回调行为。

新增 U11 清理后重放夹具初次使用 addIdea 触发清理，导致 workspace 本身发生变化，不能要求当前 Snapshot 等于历史 Snapshot。改用既有无业务写入的 flushActionLog 清理，保留逐字段重放和无写入断言；没有修改产品重放实现或降低 T1 原断言。

## 验证环境与命令

自有容器 `pcas-test-F10-expired-undo`，pgvector PostgreSQL 16，数据 tmpfs 1 GiB、WAL 128/32 MiB、Docker 随机 localhost 端口 33253。数据库测试串行独占该库、沿用唯一 schema 清理夹具。模型、通知与 Telegram 均用本地假服务，没有访问真实账户或生产数据库。

| 命令 | 实际结果 |
|---|---|
| `make check`（不传 DB） | fmt-check、go vet、race 单元检查与 build 通过；数据库证据单独见下一行 |
| `PCAS_TEST_DATABASE_URL='<F10 临时库 URL>' GOFLAGS=-v make test-integration` | 全量 PostgreSQL race 集成通过，117.976s；含 T1 明确项、U16 的 50/50 种子及 1000 次逆序撤销、F7/F9 相关回归；四个 pending 叶与两个真实模型环境 skip 保留，无 finding skip |
| `... go test -race -count=1 ./internal/postgres -run '^Test(StabilizationUndoU(11_\|12_\|13_)\|DeletionExpiresItemAndDocumentSnapshots\|ActionSnapshotRetentionKeepsAuditMetadata)' -v` | 修复专项通过，4.928s；U12/U13 三叶及新增 5 叶全部执行，无 finding skip |
| `PCAS_TEST_DATABASE_URL='<F10 临时库 URL>' go test -race -count=1 ./internal/telegram -v` | 全量 Telegram 通过，2.423s；含本地文案映射及真实 PostgreSQL U15，无 skip |
| Node 22.23.3，`npm run lint` / `npm run type-check` / `npm run build` | 全部通过 |
| `PCAS_TEST_BASE_URL=http://127.0.0.1:18140 npx playwright test tests/secretary.spec.ts tests/buttons.spec.ts` | 既有 mock 用例 22/22 通过，24.7s |
| `make fmt-check`、`git diff --check` | 通过 |

前端首次浏览器运行的 22 个失败均为静态 Vite 随工具 shell 退出后的 `ERR_CONNECTION_REFUSED`，未到产品断言。记录自有 PID 的独立会话恢复 Vite，先检查监听再进行一次重跑，22/22 通过；未修改测试或新增框架。Vite PID 3875412 已按记录停止。

## 局限与交接

- U3、U4 用户命令路径、U5、U10 的四个 pending 叶 skip 原样保留；newer_action 的重叠裁定与当前轮新对象引用协议未纳入实现，不称这四项通过。
- T3-D3 与 U12 同源错误码问题由后续 F11 集成候选验证并移除 skip，本任务不重复造 D3 用例。
- 有效期入口与既有清理沿用 PostgreSQL 事务时间；物理快照清理时机仍为既有 flush，入口拒绝不会写业务行或恢复资料。
- 普通内容冲突、旧历史指纹失配的拒绝保持原有行为。没有扩展保留时长、修改指纹排除字段或新增迁移。
- 本地假服务与 mock 浏览器证据不代表真实默认通道、设备推送或用户一天试用通过；生产与 main 未操作。
- 资源已清理：`docker rm -fv pcas-test-F10-expired-undo` 成功，仅移除自有 tmpfs 容器及其卷；Vite PID 3875412 按记录停止，18140 监听已消失，初次 shell 所属进程已自行退出。没有其他持久后台服务；httptest/临时 schema 与 blobs 由测试 cleanup 清理。
- 交付后停止本分支写入并释放 poller.go、poller_test.go 供 F12 接手；F11 可消费本提交并进行 D3 集成验证。
