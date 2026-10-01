# F14：同轮新建并继续操作

2026-10-01。执行者 Sol / high。分支 `stabilization/F14-same-turn-actions`，工作区 `/root/PCAS-wt/F14`，基线 `59e25daa67df7447f0a09b7f0bd9e5d65edab65e`，Draft PR base 为 `stabilization/acceptance-candidate`。

## 结果与范围

用户明确要求的「建交作业任务，再给它加查资料、写提纲两个步骤」通过真实 `Store.DeskTurn` 入口和实际假模型 HTTP 请求，一轮完成新建及加步骤。两条回执指向同一新任务，分别拥有可撤销动作记录；同 requestId 重放不重复执行。F14 自测最终 9 个顶层、17 个叶用例全部通过、零 skip，启用 Go race。`make check` 通过。

本报告只声称 F14 自测和本分支检查通过。T4 的独立 U10、F13 与 F14 集成后的完整 PostgreSQL race、最终验收另由 T4 交付。当前基线的 U3/U4/U5/U10 四个待决叶未在本分支测试文件里修改，也未算作通过。本任务不代表最终候选、真实模型、线上或部署验收完成。

## 契约与实现

先审查解析、动作执行、子事务提交与请求重放路径，再由协调者更新正式契约 §2.1（协调文档提交 `2e7315f`）后实施：

- `N1` 表示原 `actions` 数组第 1 个动作成功创建的事项。序号从 1 开始，解析失败和执行失败仍占位置。
- 仅 `create_task`、`create_idea`、`create_project` 的 done 回执且动作子事务成功提交后，才绑定 `N{原数组序号}`。
- 每轮将原 T/P/I/R/THIS 拷贝到独立动作引用表。`ref`、`project`、`set.project` 沿用现有执行器，项目和任务类型检查仍生效。原上下文、卡片与后轮引用表不加入 N。
- `delegate:new` 和 `project:new:名称` 的附带创建不绑定 N。非法、前向、非创建、失败位置及跨轮 N 没有匹配项，依赖动作生成有原因的 skipped 回执，不回退到 THIS、R、标题或 UUID。
- 提示词及动作格式说明明确 N 序号、创建类型、允许字段和当前轮范围，并提供用户场景的双动作例子。已有 R、THIS、delegate:new 与前 10 动作上限保持原语义。

产品只修改 `internal/postgres/desk_turn.go`（提示词和动作引用表）与 `internal/postgres/desk_actions.go` 的引用说明注释。原解析器已逐项保留数组位置，`desk_parse.go` 无需改动。不增加持久表、迁移、外部适配层或第二份状态；每个成功动作继续使用原 action_log 和撤销流程。未修改 F13 的 `actions_log.go`、T4 的 `stabilization_undo_test.go` 或共享测试 helper。

## 自测证据

新增 `internal/postgres/same_turn_actions_test.go`。全部用例调用真实 DeskTurn，走现有 OpenAI HTTP provider adapter，使用 F14 假模型，不直接调用动作内部实现。测试在自己的临时 schema 中执行，模型服务实际监听 `127.0.0.1:18154`；CI 默认使用临时端口，可通过 `PCAS_F14_MODEL_ADDR` 指定。模型请求正文中的实际 system instructions 已检查包含 N 协议及示例。

| 用例 | 已观察结果 |
|---|---|
| 新建任务、N1 加两步、show/used/links N1 | 目标为新任务，旧 THIS 不加步骤，两条服务端回执正确，N 不进入卡片 |
| 多种创建与项目引用 | N1 项目供后续任务、想法的 project/set.project 使用；N2、N5 两任务步骤分别正确；N3 想法更新正确，加步骤受类型检查跳过 |
| 解析失败与真正创建回滚 | 第 1 个创建字段类型错误；第 2 个任务先 INSERT，随后 notes UPDATE 经测试 schema 的触发器注入失败，子事务回滚；第 5 个成功创建只能经 N5 引用，N1/N2 均跳过，旧 THIS 保持原状 |
| 非法 N 边界（9 叶） | N0、N-1、N01、n1、前后空格、自指 N2、前向 N3、越界 N999 全跳过；合法后续创建继续执行，无误改旧事项 |
| 仅显式 create 绑定 | 附带项目不借用任务 N；update 不生成新 N；delegate:new 不生成 N；项目标题不能当引用，错误依赖动作有原因且无日志 |
| delegate 引用 N1 | 副手 run 和回执指向新任务；该 delegate 不产生 N2；逆序撤销副手动作再撤销创建后，保留旧任务 |
| R/THIS 及下一轮 | 当前轮新建后，R1 仍修改上一轮对象，THIS 仍修改页面对象；下一轮 N1 跳过，R1 正常加步骤 |
| requestId 重放与冲突 | 相同请求只有一次模型调用、一次创建和加步骤；撤销后重放不重建，回执实时显示 undone；不同正文同 requestId 返回冲突且无额外写入 |
| 原 10 动作上限与逆序撤销 | 12 动作输入实际只执行前 10 条，即一个任务加九步；现有第 11 条上限 skipped 回执保持；十条成功记录逆序撤销后任务消失 |

每个成功回执都检查独立 actionId、undoable、source=desk、turn_id、summary 与实际目标的 changes；skipped 回执检查 reason 非空，actionId/thingId 为空且不可撤销。核心双动作测试还检查两条回执的服务端文案和创建后的完整步骤，不依赖模型自报成功。

执行命令与最终证据：

```sh
PCAS_TEST_DATABASE_URL='postgres://postgres:test@127.0.0.1:33266/postgres?sslmode=disable' \
PCAS_F14_MODEL_ADDR=127.0.0.1:18154 \
go test -race -count=1 ./internal/postgres -run '^TestSameTurn' -json

make check
git diff --check
```

- 专项原始 JSONL：`/tmp/pcas-f14-evidence/same-turn-final.jsonl`。9 顶层 / 17 叶，0 fail、0 skip，包耗时 7.255 秒。
- 首轮 8 顶层 / 16 叶证据：`/tmp/pcas-f14-evidence/same-turn-initial.log`。
- 后补 delegate 用例首次错误地要求公开 `State.Runs.Status` 为 queued；现有公开状态为 running，数据库队列状态单独保存。目标和日志当时已经正确。这是测试夹具错误，删除与 N 契约无关的状态断言，保留目标、日志与逆序撤销检查后，最终专项全部通过。该次失败保留在 `/tmp/pcas-f14-evidence/same-turn-delegate-fixture-failure.jsonl`，不计为产品 finding。
- `make check` 日志：`/tmp/pcas-f14-evidence/make-check.log`，包含 gofmt、vet、全 Go 非数据库 race 和 build。该命令未设置数据库环境变量，数据库用例 skip 不属于集成通过证据。
- 根据协调者安排，不重复执行完整 PostgreSQL 包；由 T4 在含 F13/F14 的最终候选执行，F14 本地只运行上列 PostgreSQL 专项。

T4 已独立在原候选复现 U10：create_task 与 add_steps N1 后新任务 checklist 为空。其原始失败证据由 T4 保存于 `/tmp/pcas-test-T4/original-failures.jsonl`。这是他方已报告证据，本报告不替代 T4 的独立最终结果。

## 资源与限制

测试库容器 `pcas-test-f14-20261001`，使用 `pgvector/pgvector:0.8.2-pg16-bookworm`、UTF8，数据目录为 768 MiB tmpfs，Docker 动态分配数据库端口 33266。未连接生产容器、生产数据库、真实模型或外部通知账户；未读取生产秘密、合并 main 或部署。

假模型由每个测试的 Cleanup 关闭；测试 schema 均由现有 testStore Cleanup 删除。最终专项完成后已执行 `docker rm -f pcas-test-f14-20261001`，自有数据库容器及 tmpfs 数据移除；未遗留后台模型/通知进程，保留上列小体积本地证据。

未覆盖真实模型对新提示词的生成质量、浏览器/Telegram 手工体验与生产行为；这些并非当前假模型验证可证明的结果。未发现额外 F14 产品边界问题。
