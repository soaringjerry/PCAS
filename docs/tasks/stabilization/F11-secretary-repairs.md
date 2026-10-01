# F11：对话串行、删除传播与失败说明

执行者：6.1 Sol / high，`f11_secretary_repairs`，不是 T3 测试作者。T3 已交付 #27 / `071e87d` 并停止写入，可以启动。实现已有正式契约，不新建记忆系统或另写聊天状态库。

工作区 `/root/PCAS-wt/F11`。初始候选 `stabilization/secretary-candidate` 由 F9 `26dba8672b578cf1baadd71c0ba257f7691d8109` 与 T3 `071e87dc3dd40f08c834797900e68842180d41f9` 无冲突集成；记录初始 SHA，从其创建 `stabilization/F11-secretary-repairs`。F10 完成后，将其最终提交合入候选并记录最终 SHA，把本任务自己的提交 rebase 到最终候选（冲突先报告），再验证 D3。PR base 为最终候选，正文列明这些依赖；不合并 main。

## 已发现的序列与要求

| 编号 | 独立观察 | 修复要求 |
|---|---|---|
| S1 | 首轮模型尚未返回，第二轮已调用模型，无法引用首轮新建事项 | 同 owner / conversation 的轮次覆盖读取上下文、生成、提交的串行；第二轮看到第一轮结果 |
| S8 | HTTP client timeout 被归为 model_error | 用户说明与安全日志准确区分超时、HTTP 500、预算和协议失败 |
| S9 | 2500 字回复原样返回 | 按 2000 字约定截断并附提示；动作仍完整执行，保存与重放一致 |
| D1 | 删除被引用 claim、不连带删来源时，desk_turns 与 response 仍留依赖正文 | 按删除传播契约清理受影响整轮缓存，保留回执骨架；原独立来源按删除范围处理 |

T3 的 D3 / T1 的 U12 是同一个 expired 错误码问题，归 F10；此处只修 D1。Telegram 的 T2/T3/T6 归 F12，不修改 poller。

## 实现边界

- 串行不能通过在模型调用期间持有整个 owner 的数据库事务锁实现；无关会话和 owner 能继续工作。说明锁的生命周期、取消/失败释放、多实例行为以及不会重复执行的保证。先复用现有并发手段，有共享结构变化先报告。
- 超时分类不得通过把带凭据 URL 的底层原始错误拼进日志实现。保留 HTTP status/安全类别和既有用户文案约定。
- 长度按字符处理，不截断 UTF-8；回答裁剪不影响 receipts/actions/ask 的处理。不要顺手改卡片协议。
- D1 核查原话、模型依赖、缓存、history/replay 的具体来源。IncludeSources=false 不授权删独立原始来源，但也不能保留已删除 claim 的派生回答。不得靠隐藏前端卡片代替清理持久缓存。
- 同时验证生成期间发生删除不会把已删信息重新提交；复用既有授权/版本/删除检查，不绕过它们。

## 文件归属

预留 `internal/postgres/desk_turn.go`、`internal/postgres/editing.go`、`internal/ai/provider.go`；已追加授权 `internal/postgres/desk_parse.go` 仅 secretaryCaptureText 的超时文案分支，以及 `database.go` 初始化 Store 的有界 secretarySlots。当前 pool 固定 10 连接，DeskTurn 最多占 9 个，给既有 Recall/额度短事务保留余量；锁冲突时先回滚归还连接和 slot 再等待，取消/失败/提交均释放。跨 Store 的同会话串行仍依 PostgreSQL 锁，不能用仅本地 channel 代替。需要其他 Store 字段、迁移、共享类型或文件仍先请求归属。

T3 停止写入后，接手 `internal/postgres/stabilization_secretary_test.go` 与 `internal/postgres/stabilization_deletion_test.go`，仅移除自己已修 finding skip 和补充必要边界。D3 skip 由 F10 的类型/实现修复后，在集成候选上验证并移除；不改其错误码预期。

额外批准 `internal/postgres/desk_turn_test.go` 中 `TestSecretaryRejectsStaleRowsAndKeepsOriginalOnCancellation/timeout` 的用户文案从旧「模型没有响应」改为正式 S8 所需「模型响应超时」；该分支原日志已期待 timeout。其余断言不变，报告逐项登记。

唯一报告 `docs/evaluations/2026-10-01-secretary-repairs.md`。不改任务文档、前端、动作撤销、Telegram 文件和其他报告。

D1 夹具复核结论：T3 原先在创建 claim 后才注册模型，原 `cards>0` 被 links 卡满足，不能证明真的引用了 claim。已批准把模型注册提前，并增加 sources 卡及真实 claim 依赖断言；正确夹具在未改 editing.go 的实现上已通过清理、历史/重放及回执骨架断言，原 D1 产品 finding 撤回。保留补强夹具和生成期间删除的边界验证，不为旧夹具修改产品。额外允许仅在原 T3 报告 D1 节追加更正及证据链接，保留原记录。

## 验收与交付

先运行 T3 原失败，再实现；原断言正常执行，任何调整逐条向协调者说明。并发用 barrier 验证顺序，不凭短 sleep 观察。补充同会话取消/失败后可继续、不同会话可并发、敏感错误不入日志、删除后不重放正文等关键边界。按统一规则使用自有 tmpfs 库和假外部服务，运行 make check、数据库集成及相关回归，清理后提交独立 PR，不合并、不部署。

## 最终 CI 补验（06:56 UTC）

PR #29 最终 e5841a2 的真实后端 CI run 36826114049 失败：G9 三轮仍精确等待旧提示「已记下原话；模型没有响应，稍后会自动整理」，其余 33 个 golden 和 4 个 backend 通过。此前 S8 已按正式要求修正超时分类与文案。原 F11 执行者现在接手唯一新增归属 web/tests/golden.spec.ts 的 G9 超时精确文案断言及自己的 F11 报告，不能改其他 golden 行为/原话保存/来源查询/重复次数。先核实真实页面/trace 实际是正确 timeout 文案，再同步旧断言；若实际产品没有保存原话等，另报真实缺陷，不能只改断言。

在原 F11 工作区和修复分支补提交，依旧不合并 main。自有服务预留 18146/18147，自有 tmpfs 数据库，针对 G9 验证三轮及相邻用例；完整最终候选 runner 将由 A1 在纳入 U1 后一次执行，避免无意义重复。报告准确列补验范围/CI 链接；交付最终 SHA 后停止写入。U1 独占其他前端/CSS/秘书与时区测试，不得接触；A1 工作区也不得修改。
