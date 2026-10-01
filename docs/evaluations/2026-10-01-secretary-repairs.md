# F11：秘书轮次串行、失败说明与删除复核

2026-10-01，执行者 `f11_secretary_repairs`，非 T3 测试作者。工作区 `/root/PCAS-wt/F11`，分支 `stabilization/F11-secretary-repairs`。初始候选 `stabilization/secretary-candidate` 为 F9 `26dba8672b578cf1baadd71c0ba257f7691d8109` 与 T3 `071e87dc3dd40f08c834797900e68842180d41f9` 无冲突集成的 `d9f717368bb25dfb4492aef1cce7fd02f5f4dcad`；随后无冲突合入 F10 `8d2a70df721dd433d45d2232b0405280f0156ca7`，最终候选 `7cc48813ffd0906c6b003c4500e5a0cf40b4e322`，两个候选均已推送。F11 自己的修复提交已 rebase 到最终候选，PR base 为该候选，依赖 #20/#24/#25/#26/#27/#28，不合并 main，不部署。

## 真实失败到验证映射

原输入通过 `PCAS_STABILIZATION_RUN_FINDINGS=1` 执行，S1/S8/S9/D1/D3 均真实失败，日志 `/tmp/pcas-test-F11-original-findings.log`。原断言没有降低。

| 编号 | 原观察与结论 | F11 结果 |
|---|---|---|
| S1 | 首轮模型仍在 barrier 内，第二模型已进入，最终事项仍是 15:00，第二轮 update R1 被跳过 | 同 owner/conversation 锁覆盖读取、模型调用、提交，原 barrier 与 16:00 更新断言通过；不同会话和 owner 可以继续 |
| S8 | HTTP client 200ms deadline 被脱敏成一般 unreachable，日志 model_error、用户收到「模型没有响应」 | HTTP transport timeout 返回固定 DeadlineExceeded，日志 timeout，用户「模型响应超时」；500/budget/plain 原分类和文案断言保留通过，含 URL 用户名/密码/路径的 timeout 未泄漏敏感数据 |
| S9 | 2500 汉字原样返回，无提示 | reply 按 rune 保留前 2000 字，追加换行和「回答太长，已截断」；动作、回执、持久缓存和同 requestId 重放保持一致 |
| D1 | 原夹具清除断言失败；诊断发现 dependencies=[]，cards 只有 links，没有真正引用 claim | **撤回产品 finding**。修正授权夹具后，在完全未修改 editing.go 的产品上，原清除/回执骨架/history/replay 断言通过；移除 skip，补充独立 capture 来源保留和生成期间删除边界 |
| D3 | 原输入删除传播 changes=[]/expired_at 已通过，HTTP 返回 changed_since | 消费 F10 #28，移除 expired_code skip，保留 409 expired 原断言并通过；F11 不修改撤销实现 |

### D1 更正证据

原 T3 D1 在 `capture`/`acceptCandidate` 之后才注册 `secretaryModel`。该 claim 创建时 model principal 不存在，没有获得授权。诊断中的持久 `dependencies=[]`，cards 为 links；假模型自己吐出「银色森林」，并没有从服务端读到/引用 M1。原 `len(cards)>0` 被 links 满足，不能证明引用。

经协调者授权，仅将模型注册提前到 capture/accept 前，并增加 sources 卡含目标 claim 和持久 dependencies 含 claim 的强断言，完整清理预期不变。修正夹具在原有删除产品路径上通过，日志 `/tmp/pcas-test-F11-corrected-D1.log`。产品 `editing.go` 零改动。

补充断言明确保留独立 `capture` candidate.Source 对应的原始正文。`memory-input` 是随 claim 的派生来源，会被已有删除闭包清除，不能混称为独立原始来源。新断言最初误取无稳定顺序的 memories.Sources[0]，改为明确 candidate.Source；这是新增夹具修正，不是产品 finding。

生成期间删除测试先确认模型确实收到私密 claim，再在模型 barrier 内 deleteMemory（不连删独立 source），然后放行返回私密回答及 create 动作。原有 commit 前授权/版本复核拒绝旧结果，仅保存不含私密 claim 正文的原问题与 capture 回执；无事项、无私密回答缓存，重放不调模型。

## 锁生命周期与容量

```text
Snapshot 初始化（短事务结束）
→ 可取消取得 Store secretary slot
→ 事务 A：try request advisory lock + owner/conversation advisory lock
  失败：rollback → 归还连接和 slot → 可取消等待 25ms 再试
  成功：A 读取上下文/生成 prompt
       → Recall 及额度预留借用同 pool 的空余连接；额度事务 B 短持 owner 锁并提交
       → 模型网络调用：A 仅持 request/conversation advisory 锁，不持 owner 行锁
       → A 取得 owner 行锁，复核删除/权限/版本，执行动作并保存轮次
       → commit 或 rollback，自动释放 advisory 锁、连接、slot
```

锁由 PostgreSQL 提供，覆盖不同 Store/不同实例，不依赖内存会话状态；连接丢失/进程结束时事务锁由 PG 释放。锁键规范化 UUID 大小写，跨 Store 队列中一半请求用相同 conversation UUID 的大写拼写；PG uuid 的等价输入不能绕过串行。request 锁继续保护同 requestId 的查旧缓存、请求 hash 冲突和仅一次执行；重试只发生在取得两锁之前，生成/动作之后不会自动重做。模型失败或调用方在生成期间取消，沿既有持久 capture 路径提交并释放；排队期间取消直接结束，不新增轮次。事务内不存在的 ThingID 报错 rollback 后同会话仍可继续。

原 DeskTurn 已在持 A 连接时从同 pool 执行 Recall 与预算短事务，10 个不同会话可能耗尽固定 10 连接。获协调者授权，在 `database.go` 初始化每 Store 最多 9 个 DeskTurn 事务的 slot，留 1 连接供这两条嵌套路径使用；等待 slot 不占 DB 连接，try-lock busy 先 rollback 并释放 slot 再等。同一会话堆积不会耗尽 pool。模型容量饱和时新会话正常排队，没有承诺无限并发。此保证仅针对 DeskTurn 的连接使用，不推广到未测旧 AnswerDesk/worker 任意混合占用。

这里保证同会话的轮次在取得锁后覆盖上下文、生成和提交完整串行；已经进入模型的首轮完成之前，下一轮不会进入模型。25ms try-lock 竞争不保证三条以上等待请求严格 FIFO；16 等待者测试验证隔离与完成，不验证按用户发送先后排序。当前协议没有客户端序列号或跨实例持久队列，不能证明网络发送顺序，留给 A1 跨模块审查。没有新增队列或协议。

测试包括两独立 Store/pool 共用数据库上的同会话串行、16 个等待者仍允许另一会话和 owner 完成、12 个不同会话使 9 个模型同时停在 barrier 且独立 Snapshot 成功、排队取消/生成取消/500 后续接、跨实例同 requestId 并发只创建一次、事务 rollback 后释放。

## 改动与验证

产品文件仅 `desk_turn.go`、`provider.go`，以及协调者授权的 `database.go`（slot 初始化）和 `desk_parse.go`（仅超时用户文案）。接手两份 T3 PostgreSQL 测试。全量回归首跑仅旧 `desk_turn_test.go` 的 timeout 精确文案断言失败（120.909s）；经协调者逐项批准，仅将该分支的 reason 从「模型没有响应」改成正式 S8 的「模型响应超时」，日志/原话/capture/状态断言全部保留。未修改 Telegram、前端、公共类型、撤销实现、共享 helpers 或任务文档；原 T3 报告仅追加获授权的 D1 erratum 链接。

测试使用自有 `pcas-test-F11-secretary`，PG data 1GiB tmpfs，max/min WAL 128/32MB，随机 localhost 端口 33255。所有模型/通知服务为本地假服务；无真实账户或生产连接。

```sh
PCAS_TEST_DATABASE_URL='postgres://postgres:test@127.0.0.1:33255/postgres?sslmode=disable' \
  go test -race -count=1 ./internal/postgres -run '^TestStabilization(S|D)' -v

env -u PCAS_TEST_DATABASE_URL -u PCAS_STABILIZATION_RUN_FINDINGS make check
PCAS_TEST_DATABASE_URL='postgres://postgres:test@127.0.0.1:33255/postgres?sslmode=disable' make test-integration
```

最终代码验证：

- S/D 专项退出 0，18 个顶层用例通过，零 finding skip，13.326s；包含 UUID 大小写和 D3 expired。日志 `/tmp/pcas-test-F11-final-SD.log`。
- `make check` 退出 0，fmt-check、vet、Go race 单测、build 全部通过；此命令未使用 DB，不算 DB 验收。日志 `/tmp/pcas-test-F11-make-check.log`。
- 自有 DB 上 `make test-integration` 退出 0，全 PostgreSQL race 集成 128.528s。日志 `/tmp/pcas-test-F11-integration-final.log`；上一轮仅旧超时文案断言失败的日志 `/tmp/pcas-test-F11-integration.log` 保留用于更正证据。

一次 D1 编译与我切分支集成 F10 错误重叠，临时出现 `ErrExpired undefined`；该日志弃用，单列为执行过程错误，不当作产品 finding。之后 checkout/rebase 与构建测试严格串行。新增跨 Store fake server 初次也错误地把历史中的首轮标记当当前请求重复 close，修为只 hold 第一次；保留原产品断言重新运行。

S/D 的 finding skip 已全部移除。F10/T1 仍保留 4 个契约 pending（U3/U4/U5/U10），Telegram T3 的独立 findings 归 F12，本任务全量命令的绿色结果不代表这些尚未裁定或未修部分通过。默认真实 Codex、生产 Telegram/通知/手机、线上 11 项及一天试用未验收。

`pcas-test-F11-secretary` 已执行 stop 后 `docker rm -v`，确认不存在；测试进程均退出，httptest 服务和临时 schema 由测试 cleanup 释放，无持久后台进程。少量 `/tmp/pcas-test-F11-*.log` 留作当前机器证据，诊断临时脚本已删除。最终提交与 PR 由交付消息提供；两个候选与修复分支均已推送，不改 main。
