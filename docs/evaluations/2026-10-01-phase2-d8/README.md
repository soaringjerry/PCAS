# d8 第二阶段独立动态诊断

2026-10-01。验收者 C；唯一工作区 `/tmp/pcas-phase2-c`，分支 `phase2/independent-acceptance`。

本次运行证明：d8 的零 claim 原话在 Recall 中命中且可读取，但秘书、自动副手、manual 三个真实消费者全部丢失原话。自然授权当前产品入口缺失，不能用 SQL grant 装配诊断冒充自然授权验收。

## 基线与独立环境

- 产品：`d8d6fb3efe92f7711c5567440e92af362df7fe77`。
- gold 冻结提交：`afd30a6`；`gold.json` SHA256 `ff1820de4ef962d82f49f7c204399b8f83362c1aa468ac5017c33cb98e853ec2`。
- harness/补充 K0 序列：`8dfabc4e36229b76ae9ce992bce0e2e18a48820a`；`k0-sequences.json` SHA256 `ff6f0469a47d8c74ed1eb5352ccae5257b3d1365a44071899b7304266a43e0f7`。
- `git diff d8d6fb3 --stat` 只有独立测试和两份 fixture；未修改产品。
- 使用协调者创建的容器 `pcas-phase2-c-20261001-pg`，独立数据库 `phase2_c`，loopback 端口 33273。TCP socket 成功；全部用例完成 DB 登录、UTF8 检查、pgvector/迁移与合成数据操作。每个叶用例自行创建唯一 schema，清理时只删除自己的 schema。
- 模型为 `httptest.NewServer` 的本地假 OpenAI provider，实际调用 `/chat/completions`；没有真实模型、私人资料、通知、部署或线上写入。

## 一次运行与整数结果

执行一次 `go test ./internal/postgres -run '^TestPhase2' -count=1 -json`，通过独立 DSN 和 `/tmp/pcas-phase2-c-d8-evidence` 捕获。退出码 **1**；没有 skip，没有同批重跑，没有把故障改成预期通过。

| 计数口径 | total | pass | fail | skip |
|---|---:|---:|---:|---:|
| 顶层测试 | 5 | 3 | 2 | 0 |
| 叶用例（有 subtest 时不重复计父项） | 9 | 5 | 4 | 0 |

实际 provider 请求 5 份；证据 JSON 28 份；包执行 4.483 秒。`results.json` 保存每条 pass/fail 事件及精确 SHA；`go-test.jsonl.gz` 是完整未删节 JSONL，空的 `go-test.stderr` 也保留。未执行全仓库回归，不将历史 101 mock/41 real、旧 M1 或 CI 技术绿色并入本次计数。

## 逐入口诊断

| 用例/入口 | 实际最终输入 | 依赖/出处 | 结果 |
|---|---|---|---|
| E1 raw / DeskTurn | 三个必需原子均缺失：`Q7-LANTERN-482`、`周六上午九点`、`东门蓝色雨棚`，0/3 | source@1 在 Recall；desk dependencies=[] | FAIL |
| E1 raw / Execute(requestRun)→RunAgents→HTTP provider | 同三个原子均缺失，0/3 | source@1 在 Recall；run ContextVersions=[]；运行完成 done | FAIL |
| E1 raw / manual Brief | 同三个原子均缺失，0/3 | source@1 在 Recall；ContextVersions=[]；waiting 包 | FAIL |
| E2 claim / DeskTurn | `JADE-CONTROL-619` 实际出现，1/1 | exact claim@1；假响应 Used=[]，没有 sources 引用卡 | PASS |
| E2 claim / RunAgents→HTTP provider | 同原子实际出现，1/1 | exact claim@1；HTTP 假响应 Used=[] | PASS |
| E2 claim / manual Brief | 同原子实际出现，1/1 | exact claim@1；PCAS 已准备，外部 Used/收到 unknown | PASS |
| E1-NATURAL / owner DeskTurn 当前完整句 | owner 说“让秘书能用《成都预约资料》”后，非 owner GetSource 为 not found | 无 SQL grant；假 provider 回空 actions；没有可用原文授权事实 | FAIL：当前产品入口缺失 |
| claim-only grant / phase2-model 与 manual GetSource | 两个接收者均 not found，2/2 拒绝 | claim 可读没有扩大到 source | PASS |
| E3 / manual 续做 | `DERIVED-OUTPUT-338` 出现，底层 `INDIRECT-ORIGINAL-927` 不出现 | 底层 exact claim@1 仍在间接依赖；外部收到 unknown | PASS |

E1 的 source 已提交且可读、claims=0、处理队列 pending>0，由测试直接查询和断言。pending 的精确整数未写入本批持久证据，不能补造数字。SQL 仅向两个合成 principal 写 source record_grants，证明旧粗粒度授权后的装配断点；未创建 K0 精确 receiver policy，也未宣称公开/自然授权通过。Recall 的原文命中只作诊断，未用它替代最终输入。

自然授权用例在现有 d8 入口完成一次 owner 请求后检查 GetSource；这一授权步骤已失败，因此尚无“自然授权后 E1 三入口供给”的完整成功证据。K0/K2 应提供确定授权操作，不能让模型空 actions 或 SQL 夹具被称作授权成功。

## 捕获的证明边界

`requests/` 给出 5 份本地 provider 实收 JSON 以及 2 份直接 manual 包；`capture.tar.gz` 保存全部 28 份原始合成捕获，包括 Recall、run、依赖与 E3 完整包。`capture-index.json` 列出各文件字节数和 SHA256。

本批 harness 用 json.RawMessage 再 MarshalIndent 输出，所以保存的是实收请求的完整 JSON 结构与消息内容，原始网络字节空白未留存；不能把文件字节数或文件 hash 称作原始 wire payload 字节/hash。此限制不改变逐字 gold 原子有无的判断。精确最终载荷/byte 映射与持久 attempt 属 K0/K1 新验收，当前 d8 没有这些产品能力的通过证据。fake provider HTTP receive 能证明本地端点收到请求，真实第三方模型内部上下文与因果均 unknown。manual Brief 为 PCAS 返回且持久的交付材料；没有外部接收回执，外部收到 unknown。

## 失败到责任文件

| 失败 | d8 责任位置 | 分派 |
|---|---|---|
| E1 Desk raw 丢失、source typed dep 缺失 | `internal/postgres/desk_turn.go:263` 的 Recall→current claim map 交集；`:393` 最终调用 | A / K1 typed hydrate、最终输入与依赖 |
| E1 自动/manual raw 丢失、source typed dep 缺失 | `internal/postgres/runs.go:127` 的 current claim map；`run_context.go` 准备；`runs.go:445` provider | A / K1 三入口共用闭包 |
| 当前自然授权产品入口缺失 | `internal/postgres/sources.go` 来源粗授权路径；desk 当前命令入口；HTTP 尚无授权 API | B / K2 mutation/helper；A / K1 Desk 接入；B 注册路由 |

责任定位来自运行后只读核对，gold 预期未从产品输出生成。K0 fresh/升级迁移、typed kind、fencing、scope、retention/capacity 均等待协调者冻结准确产品提交后独立执行。

## 阶段入口门槛证据

| 门槛/层次 | 可核证据 | 状态 |
|---|---|---|
| 历史 mock/real 技术检查 | 交接记录 101 mock/41 real；本批未重跑 | 历史记录，不能替代新动态验收 |
| d8 CI check | 调度记录的 main check 成功 | 技术检查；不能替代线上、真机、用户收益 |
| 新 d8 三入口原话贯通 | 本报告 raw 0/3入口成功 | 未通过 |
| 自然来源授权 | 本报告入口 not found | 未通过 |
| 线上实测 11/11 | 未获完成记录 | 未完成证据 |
| 真机提醒/通知 | 未获完成记录 | 未完成证据 |
| 用户连续试用一天 | 未获完成记录 | 未完成证据 |
| 第二阶段真实模型、成都黄金路径/连续任务/成本收益 | 本批使用合成 fake provider，仅验证供给边界 | 未完成证据 |

白皮书、稳定化入口和用户禁 skip 要求均保留。本批隔离实现授权不表示阶段门槛已通过。
