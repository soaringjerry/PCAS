# Retry / owner receipt 因果复验 R2

## 固定版本与结果

产品及 harness HEAD：`c8adbaeb5c02daa9e83f90f74c216d8cf9ee22bf`。执行前工作区 clean；实际 list、全部 gold / tests / contracts SHA256 见 `selection.json`。

仅对 R1 的六个原失败叶执行一次；2 顶层、6 真叶全部 PASS，退出 0，wall 14.269312 秒，Go package 6.139 秒。0 FAIL、0 SKIP、0 未抵达；无 race 告警、panic 或 timeout。原 17 个通过叶未选择，不计作本次通过或跳过。不得把跨版本结果合并为同头 23 叶全通过；最终完整 CI 另计。

```sh
go test -race ./internal/postgres -count=1 -json \
  -run '^TestPhase2Runtime(OwnerUndoReceiptAuditNeverRestoresSourceText|RetryPreservesGeneratedPromptOrigins)$/^(RECEIPT-(EMPTY|SOURCE)|RETRY-LIVE|RETRY-SCOPE-DENIED|RETRY-INVALID-DELETE_SOURCE-(ORIGINAL_PROMPT|PUNCTUATION_ONLY))$'
```

使用 C 专属 loopback 合成 PG，每例唯一 schema；所有模型及 embedding 目标为本地假服务，无真实账户或模型。无产品修改、skip、断言放宽或重复运行。

## 原失败与有因修订

R1 的原始 17 PASS / 6 FAIL、退出 1 与完整原证据保留于 `08a4d2a` 的 `2026-10-01-phase2-retry-causal-r1` 报告。原 gold 不覆写，修订理由追加于 `retry-causal-fixture-erratum.json`；代码净补 `d60ea85` 已经 root 与 D 独立审阅。

| 本次叶 | 明确修订与实际证据 |
| --- | --- |
| RECEIPT-EMPTY | Undoable 表示动作支持撤销的历史能力，不表示当前仍可撤。保留两次真实 Undo 成功、两原 title / action ID / Undone 的刷新正控。 |
| RECEIPT-SOURCE | 先证明来源仍授权时首 create 历史合法；第二 turn 继承已撤 action，必须 generic、无 ThingID / atom，Reply 与 Cards 隐藏。随后正式撤原 source policy，真实 HTTP 两 turn 全响应不得含 atom / 原 title，两个 action ID / Undone 仍存在。真实生成前 Task 与 source typed dependencies 原始正控已保存。 |
| RETRY-LIVE | Studio 用正式 `create_task(project=THIS)` → `delegate(N1)`；实际秘书输入绑定真实 A alias，两 action 均成功，真实新 Task / secretary scope / deputy scope 严格 A。Prompt origins 精确一个实际 delegate ID；general / Manifest origins 精确等于旧 Run 的 create + delegate 集合，无重复或额外成员。真实新 HTTP、实际 source Input 与间接依赖正控保留。 |
| RETRY-SCOPE-DENIED | 在上述真实 A Task 正控后移动同 Task 到 B；B canonical allow 已实际存在，但 source assignment 仍只 A。精确拒绝且无新增 Run、attempt 或外发，隔离 assignment 边界。 |
| DELETE_SOURCE / 原文与标点两叶 | 正式删除实际删除旧 Run，SourceRun 定位必须 exact ErrNotFound。删除前旧 Run 存在；删除后 Run / source / version / record 消失，墓碑阻重导且真实 Ingest 返回 ErrBlocked。旧 attempt invalidated / snapshot deleted，读 snapshot 必须 ErrUnavailable 且正文空；新增 Run / attempt / HTTP / embedding / delivery 均为 0。其余 revoke / correct 的 strict Conflict 断言未改。 |

## 原始证据与统计

- `acceptance.jsonl.gz`：完整 8713 条事件；解压原文 SHA256 `b3dd8230a7379ecc87cb12beb129121c6492ed2a60a1c299802d4185d2e7517a`。
- `evidence.tar.gz`：72 份真实输入、manifest、action / typed origin、正式撤权、删除及负控 JSON；逐文件 SHA256 见 `evidence-sha256.json`。
- `causal-facts.json`：实际 studio proposal / scope、撤权后 HTTP history、删除墓碑与新 Run / attempt 零增长摘录；完整数据仍在原 archive。
- `summary.json`：全部 6 真叶终态与 package 终态。真叶定义为实际 terminal Test 没有任何实际执行的后代 Test；顶层与祖先不重复计叶。
- `execution.json` / `selection.json` / `list.txt`：精确执行、预冻结选择与 SHA。日志含实际本地 provider wire；不把模型 Used=[] 当作没有实际 Input。

本次只闭合六个已明确前提原因的叶；完整所有包 race CI、真实模型、真机、线上 11 题与一天用户试用均不由此报告证明。
