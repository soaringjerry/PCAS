# F15：按服务端接受顺序处理秘书轮次

2026-10-01。基线 `210144a93753bed736d2d5561a454f8d07185eb0`，独立分支 `stabilization/F15-conversation-order`，工作区 `/root/PCAS-wt/F15`。根据 Q1 的 1→3→2 / 最终16:00 独立证据修复；root 固定内部契约后实现。没有修改 Q1/F11 原测试、前端、Telegram、共享接口、pool 配置或动作撤销。

## 结果与接受边界

正常新请求用数据库元数据票据决定顺序。同 owner / 规范化 conversation 的「三点开会」「改四点」「最后改五点」，以三个已提交 admission_order 作为屏障，两个 Store 的真实 DeskTurn / 假 HTTP 模型执行与保存历史均为 1→2→3，三个动作指向同一任务，最终截止时间为 `2026-10-02T17:00:00Z`。

共同接受点是 **admission 事务成功提交**，可以从数据库读取票据验证。独立短事务 admission request try-lock 防同键并发注册，admission conversation try-lock 将本 conversation 的 sequence 分配及 commit 一起串行；仅 sequence 大小不能单独证明 commit 顺序。规范化 UUID / lowercase 锁键隔离 owner 并统一大小写身份，原始请求 body 的 hash 保持原正文冲突规则。

原始 HTTP 抵达、进入 DeskTurn、客户端 goroutine 启动、尚未提交 admission 的请求不承诺跨实例网络全序。没有共同先后证据的同时到达请求，由实际 admission commit 给出服务端顺序。

## 执行、幂等与资源

新增迁移 `021_desk_turn_order.sql` 的 `desk_turn_order` 只存 owner/request/conversation/creator UUID、请求 hash、sequence 顺序、accepted/expires 时间及 pending/done/canceled/failed/expired 状态；不含原话、模型上下文、回答、凭据或 State。原 `desk_turns` / source 仍是唯一业务正文存储。

只有最早 pending 的 creator invocation 才能竞争既有 generation slot。未轮到的请求和同键重复 invocation 只做短查询，释放连接后等待，不占 slot。获得 slot 与原 request/conversation 数据库事务 try-lock 后再次检查票据、队首、creator 和期限，随后读取上下文、生成、执行动作。业务、desk_turns 和票据 done 在同一个事务提交；后轮 context 读取前轮已提交结果。互斥跨 Store，slot 仅限制每 Store 的连接占用。

保持每 Store 10 个 pool 连接、最多9个生成事务，剩余连接供 Recall / budget / 短查询。没有模型期间的 owner 锁、独立等待 pool、进程 map mutex、通用调度器或新增设置。阻塞 advisory 方案没有明确 admission FIFO，且同会话等待者可能占满 slots/连接，所以没有采用。25ms 仅用于释放资源后的轮询，排队顺序由数据库票据决定。

同 owner/requestId/hash 重试不新建位置、不延长期限、不抢先执行；conversationId=null 的并发调用复用票据确定的同一 conversation。pending 同键内容不同立即 version_conflict。重复 invocation 不接管活跃 creator，取消重复调用不会取消原请求。已有 desk_turns（包括迁移前及已清理正文的历史）优先走原保存结果重放，不补造旧顺序、不重新建来源；仍读取当前 State 和实时撤销状态。

## 取消、故障与重启

- admission 前取消：返回 caller context 错误，不接受新票据。
- creator 排队期间取消：返回真实取消/超时错误，票据终结 canceled，不保存 desk_turns，不伪称原操作成功，后继可以前进。
- 生成开始后取消、模型/额度/上下文失败：保持 F11，取消模型后由脱离 caller 的既有 persist context 保存普通 capture 原话并提交；票据同事务 done，普通 capture 后台整理语义不变。
- 执行事务/数据库错误：回滚并返回真实错误，使用 pending+creator fence 的短清理事务置 failed；不因 commit 响应不明确将已 done 改 failed。数据库不可用导致清理失败时，由固定 expires_at 兜底。
- 实例死亡：不持久保存第二份请求正文，没有后台正文恢复 worker。未完成票据使用创建 invocation 原2分钟 persist deadline，重试不续期。其他调用或重启 Store 取得 conversation 执行 try-lock 后可将过期 pending 置 expired；活跃旧事务仍持锁时不能被跳过。连接死亡释放数据库事务锁，因此没有永久 running head。

终结 canceled/failed/expired 的同键重试 **只补存原话**，不复活模型或事项动作，即使当前没有后轮也不自动重做。返回200表示原话实际保存成功，capture回执明确：`已记下原话；这轮操作未完成，为避免覆盖后续改动，请重新说明要做的事`。因此前端同键重试和 Telegram 未提交 update 重试能够正常结束，旧16:00动作不会晚于新17:00再执行。补存仍取得原 request/conversation 事务锁并复查保存结果，来源/unknown候选/desk_turns/done 原子提交，后续重放不重复保存。

故障终结后补存是较晚发生的 capture，历史仍按既有保存时间展示，不能把它描述成原事项动作按接受顺序完成。正常轮次保持 FIFO；已终结请求只有原话恢复，不存在迟到业务动作。未重新调用的死亡请求没有自动原话恢复承诺，故障窗口正文仍由现有客户端/Telegram Pending 保留供重试。

## 原话恢复、自动处理与删除

专属来源 identity `connector=desk-incomplete / external_id=requestId` 与普通 capture 隔离。恢复 helper 直接复用 ingestTx/saveCandidate。仅在 `in.Duplicate=false` 的新结果上，同事务精确撤销 owner/source/version/stage=source.chunk/state=queued/attempts=0 的新建未发布 job，保留来源原文及一个待处理 unknown 候选。没有 blocked 配置错误、没有 retryJob 暗门；普通 capture job 不受影响。autoAccept=true 也无法在后台再办旧动作。原话仍可由现有 GetSource/导出/来源界面读取，用户可以明确处理候选。

该专属 identity 异常地已经存在且没有 desk_turns 时，Duplicate 返回真实 version_conflict 并回滚，不删除已发布 job、不伪报恢复成功；不同正文仍由原 ingest hash 冲突拒绝。正常原子恢复已保存 desk_turns，因此普通同键重试会在 admission 优先重放，不触发这个碰撞分支。

授权 `editing.go` 唯一扩展是正文清理的 connector 白名单加入 desk-incomplete（及邻近注释）。删除专属 source 时既有逻辑清空关联 question/answer/dependencies 和 response.turn 正文/卡片/ask，保留 capture 回执 op/status 骨架；done 历史重放优先清理后结果，不建 source、不重启 job。元数据票据没有可复活正文；保留 UUID/hash/顺序/状态用于永久同键 fence，目前没有额外清理设置，存储开销为每请求一行元数据。

## 验证

专属新文件 `desk_turn_order_test.go` 以真实 PostgreSQL、真实 DeskTurn、loopback httptest 模型覆盖以下结果，不改原 Q1/F11 预期：

| 场景 | 验证结果 |
|---|---|
| 三轮已提交接受顺序、两个 Store、conversation 大写别名 | committed sequence 1→2→3；模型/历史1→2→3；同任务最终17:00；保存结果重放及正文冲突 |
| 14个同会话等待者超过9-slot容量 | 持首轮 Store 仅1 slot，peer 0 slot；同owner异会话、不同owner同conversation先完成，队列随后全部完成 |
| null conversation 同键并发、重复调用取消 | 一票据/一conversation/一次模型；duplicate deadline不影响creator；pending正文冲突 |
| admission前、队头/队中取消 | admission前无票据；排队真实错误且无turn；后继继续；终结同键不另入队/不再调用模型 |
| 恢复原话、autoAccept=true、普通capture同requestId | 原话+unknown候选，无任何恢复source job；普通已发布source.chunk仍queued |
| 恢复专属identity异常Duplicate | conflict，无假成功、无误删已发布job，failed票据保留 |
| 专属source删除及重放 | question/answer/response正文清空；capture回执骨架保留；同键不重建来源 |
| 执行连接丢失 | 真实pg_terminate_backend仅终止本测试UUID锁对应的PG连接；原事务失败；新Store后续改17:00；旧同键只capture、不覆写 |
| 死亡admission/新Store expiry | 真实admission后不执行，验证固定原deadline；仅加速该票据expires_at来测到期，活跃conversation锁阻止过期跳过；释放后新Store继续，旧键只capture |

重启专项是新 Store + 已提交未执行票据 / 加速 deadline 的确定测试，另有真实生成连接终止；没有声称实际杀OS进程并等两分钟。既有 F11 测试继续覆盖模型错误、正在执行的取消、同键多实例幂等、9-slot连接余量；原删除/权限/动作原子性测试继续运行。

运行命令与原始证据：

```sh
PCAS_TEST_DATABASE_URL='postgres://f15:f15-local-synthetic@127.0.0.1:33270/f15_order?sslmode=disable' \
  go test -race ./internal/postgres -run '^TestSecretaryOrder' -count=1 -json
make check
PCAS_TEST_DATABASE_URL='postgres://f15:f15-local-synthetic@127.0.0.1:33270/f15_order?sslmode=disable' \
  go test -race -count=1 ./internal/postgres
git diff --check
```

- 最终新自测 native JSON：`/tmp/pcas-f15-order-evidence/order-race.jsonl`，PASS，包耗时8.626秒，无DATA RACE。
- `make check`：PASS；fmt-check、全Go vet/race（数据库测试因未设置DSN正常skip）、build。原日志 `/tmp/pcas-f15-order-evidence/make-check.log`。
- 完整真实PostgreSQL race：PASS，包耗时147.380秒，无DATA RACE；所有默认启用的数据库回归正常执行，需真实服务/显式opt-in的测试仍按既有开关skip。原日志 `/tmp/pcas-f15-order-evidence/postgres-race.log`。
- 原F11 S1基线真实DB race通过7.180秒；核心改动后同专项通过8.791秒。新自测开发时修正过prompt fixture取输入范围及漏初始化owner的FK夹具，产品预期未降低。

数据库仅本任务 Docker `pcas-f15-order-pg-20261001`，镜像pgvector:0.8.2-pg16-bookworm，127.0.0.1:33270，数据目录tmpfs；示例凭据只属于该合成临时库。模型仅httptest随机loopback端口，没有真实模型/账号/通知/密钥。已清理自己的容器，保留worktree和证据；未动他人容器、工作区或UI/T4产物。

独立交付产品、专属测试、迁移和本报告，不合候选/main、不部署、不自行开PR。Q2非实现者独立验收，T4负责最终组合验收。
