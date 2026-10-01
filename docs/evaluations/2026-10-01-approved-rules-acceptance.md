# 已批准规则的独立验收

2026-10-01，T4，Sol/high。用户确认同一事项逆序撤销，以及一句话新建任务后继续加步骤。**本提交是独立测试交接，原候选真实失败已保留；F13/F14 最终候选尚待指定与集成，不能把移除 pending 解释成验收通过。**

## 独立预期与原候选证据

产品基线 `59e25daa67df7447f0a09b7f0bd9e5d65edab65e`。先读协调工作区正式契约 §1.4/§2.1，再编写测试；没有阅读算法来反推 oracle，也没有修改产品、共享夹具或其他工作区。新别名按原 actions 数组 1-based 位置 `N1`/`N2`，不是按成功次数编号。

保留 U3/U4-user/U5 的全业务数据比较、拒绝时 revision/undone_at 检查，增加精确错误码与后续逆序成功断言。U10 通过实际 `Store.DeskTurn` 和真实本地假模型 HTTP 新建「交作业」并经 `N1` 增加「查资料」「写提纲」，验证目标、独立 action_log 和先撤销步骤再撤销创建。原 THIS update/add_steps 用例保留为独立回归，原 U16 50 种子 × 20 操作及其业务预期均未改。

U9 正常 `toggleCheck` 会写 action_log，因此按已确认规则要求 `newer_action`；保留原业务场景并扩展逆序成功验证。另一个直接 SQL 修改 checklist 的子例模拟未记录外部业务更改，仍要求 `changed_since`。这不是把正常接口夹具改成外部写入来回避新规则。

新独立覆盖还包括多个新对象隔离、`project`/`set.project`、新想法引用、解析失败/提交失败后保持原位置、前向/越界/零/非创建别名、附带项目与 `delegate:new` 不绑定 N、类型错误与 THIS 隔离、同 requestId 重放、下一轮 N 清空及 R1 保持既有语义、Used/Links/Show 不接受 N、原数组前十动作上限。三次公开命令及同轮三动作的 first→second→first 场景，要求内容恢复原值时仍不能跳过后续动作。

| 原候选检查 | 真实结果 |
|---|---|
| U3、U4-user、U5 | 应 HTTP 409 `newer_action`，实得 409 `changed_since` |
| U4 未记录外部 title、U9 未记录外部 checklist | HTTP 409 `changed_since`；全业务状态不变，revision 不变，动作未标 undone |
| U9 正常 checklist 接口、原 THIS 同轮回归 | 应 HTTP 409 `newer_action`，实得 409 `changed_since` |
| U10 create_task + N1 add_steps | 创建成功但步骤没有执行，checklist 为空 |
| 三公开命令 first→second→first 后跳撤创建 | 应 HTTP 409 `newer_action`，实得 HTTP 200，任务被删除 |
| 多对象、原位置解析/提交失败、N 重放和动作上限 | 合法 N 依赖动作被跳过，独立契约断言失败；不放宽预期 |

失败已交 F13/F14 作者；没有修改实现。原候选日志保留于 `/tmp/pcas-test-T4/original-failures.jsonl`、`original-new-rules.jsonl`、`original-returned-content.jsonl`、`original-invalid-aliases.jsonl`，相应 `.stderr` 同目录。第一轮 U/THIS 选择运行 2 个 pass、8 个 fail 测试事件；新增 N 选择运行 7 个 pass、5 个 fail；返回原内容选择运行 3 个 fail。事件包含父测试和子测试，不能当作独立场景总数。

命令：

```sh
PCAS_TEST_DATABASE_URL='postgres://postgres:test@127.0.0.1:33268/postgres?sslmode=disable' \
 go test -race -count=1 -json ./internal/postgres \
 -run 'TestStabilizationUndo(U3|U4|U5|U9|U10)|TestStabilizationUndoExistingTHIS'
# 同一隔离数据库，另外独立选择 ^TestApprovedRules_、返回原内容和非法别名回归。
make check
```

测试分支 `make check` 退出 0，fmt/vet/全包无 DB 的 race/build 通过；没有设置数据库的此条命令会跳过 PG 集成，不能代替上述实际数据库复现或最终完整集成。非法 N/THIS 隔离的七个子例在原候选通过，含附带项目与 delegate:new。

## 最终候选验收待补

尚未接管 A1，尚未集成 F13/F14，完整隔离 Go race、前端检查、62 mock 浏览器与默认真实后端 runner 留待协调者指定最终输入后执行。上述新测试均为确定断言，没有四个 U pending 的 skip；是否关闭四项需最终产品候选实际通过。

三项可选 live 继续未运行：`TestInstalledCodexHandshake`、`TestLiveCodexSecretaryAndLegacyFormats`、`TestLiveContinuityReplay`。线上 11 项、真机和一天试用没有因这些假模型测试而完成。没有生产、秘密、真实账号、外发、main 合并或部署。

本任务自建 `pcas-test-T4-integration`，pgvector PostgreSQL16，1GiB tmpfs，Docker 动态 localhost 33268。每个集成测试仍使用共享 helper 的独立随机 schema；只清理自有容器/进程。HTTP 18156/18157 仅为本任务预留，目前未启动固定端口服务。最终资源清理与准确 SHA 在整套验收后补记。
