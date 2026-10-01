# Phase 2 第五次完整后端：合成回归通过，完整技术门未通过

精确产品及全部harness **`0032883950d7dc72b1bdb160c1a98e3da17eded9`**，clean独立分支 `phase2/backend-full-r5`，工作区 `/tmp/pcas-phase2-c-postgres-r5`。该头是e6fd9b1因果定点后的唯一追加：restore marker仅在非nil AfterBlocksHash且既有双hash校验成功时启用；harness/gold/原业务断言未变。运行过程中未变更产品或测试。

先实际 `-list .` 冻结[memory6顶层](memory-selection.log)和[postgres239顶层](postgres-selection.log)，专属合成PG每例唯一schema，所有输入本地合成/fake provider，再仅一次执行：

```sh
go test ./internal/memory ./internal/postgres -count=1 -json
```

无选择过滤、重试、新增skip或真实账号，Go默认十分钟超时。2026-10-01 14:58:32.620963 至 15:03:07.259202 UTC，274.638秒，退出 **0**；Go memory包0.009秒、postgres包274.052秒通过，无panic或超时。

| 包 | 选择 / 抵达顶层 | 顶层 PASS / FAIL / SKIP | 真实终态叶 PASS / FAIL / SKIP | 未抵达 |
| --- | --- | --- | --- | --- |
| memory | 6 / 6 | 6 / 0 / 0 | 31 / 0 / 0 | 0 |
| postgres | 239 / 239 | 237 / 0 / 2 | 548 / 0 / 2 | 0 |
| 合计 | 245 / 245 | 243 / 0 / 2 | 579 / 0 / 2 | 0 |

**全部245顶层抵达；581真实叶579通过、2既有live跳过、0失败。** 采用排除所有实际run后裔proper-prefix祖先的正确规则；复合t.Run路径的父终态不重复算叶，package终态不当Test。[summary.json](summary.json)保存每包完整终态和实际叶名单，[计数勘误](../2026-10-01-phase2-backend-counting-erratum/README.md)保留旧R3/R4原文与raw且只更正重复父计数。这里不是新增统计测试或重跑旧版本。

## 此精确头的实际通过证据

- 原完整memory单元门，含Service facade20叶及Task wire3叶：四真实optional方法支持/owner/Unavailable边界、精确参数与结果/errors透传；公开Run小写recipient完整route、固定旧大写Task全身份/时间/视图/预算/origins读回、新wire再读一致；原Ingest校验未绕过。
- Name共11叶全通过：task/idea真实secretary source Input→create Title/Name→正式独立owner rename→只撤secretary，三当前视图清旧Name且保持新Title；严格eligible Undo成功不复活原文；真实manual GET交付/deputy HTTP普通完成无旧atom。owner独立Prompt叶保general origins非空而Prompt origins空。新增复制/标点/PIN/PIN片段×两kind8负叶保真实create来源，撤权后三视图/实际manual/deputy bytes都无**实际LANTERN-482片段**，仍完成普通工作。没有调词或复制算法阈值求绿。
- 原TenAction十动作上限及**全部10→1逆序Undo成功**，one-desk原ApprovedRules、ExistingTHIS和50 seeded旧逆序组全通过。非nil块hash marker收紧后的完整旧legacy/undo业务也通过，未放宽双hash/newer_action门。
- D4两delegate路径、原D1–D3、D独立action字段矩阵均通过。生成Prompt origins恰真实delegate子集、general闭包全；撤秘书后旧Prompt/结果/queue/采纳拒用，autoAdopt+真undo before副本清源atom与旧marker而保真实audit行；fresh独立deputy依法读原source、实际HTTP无旧marker且Task/Manifest origins空。owner亲写Prompt仍保留。
- 原独立conversation-order五组全通过，包括真实PG backend终止、过期head、迟到输出与恢复原断言。025实际旧action/undo语义升级/fresh25、原022→023/023→024链、三真实入口/API/scope/explicitdeny/undo/ABA、provider前及生成中fence、manual再取/提交、State/Export、实际Input/Indirect/Used区分、中文byte span/重复query、whole metadata/count/body quota、UTF8估算预算、retention和显式恢复时钟全通过。
- 全部已迁旧manual/timeout/Codex假服务/正常容量indirect-only及kind/revoke/delete、原31k受控拒交付、UX目标exclude真实普通下游无519823等全部既有业务正反断言通过。原R1/R2/R3/R4真实失败报告和原gold保留；本结果只归属于当前0032883。

## 原始可复核材料

[完整77131事件JSONL](acceptance.jsonl.gz)，解压SHA256 **`32971962598eee260a8c230c39a20697e8ea90b8877143a0d69948a02bd1a765`**；[822份合成输入/实际HTTP/manifest/ledger/状态及action审计快照](synthetic-evidence.tar.gz)（runtime611、D action211）；[逐份SHA/正确整数/完整终态](summary.json)；[全部gold及两包测试SHA、实际选择与clean证据](selection.json)；[时间/环境记录](execution.json)；[两包未抵达清单](not-reached.json)均[]。同名最后状态文件不代替完整JSONL逐事件快照。

两项原有live opt-in SKIP保持：`TestLiveCodexSecretaryAndLegacyFormats` 需专用已登录Codex home，`TestLiveContinuityReplay` 需真实embedding及专用Codex home。环境布尔均false，无真实账号/模型；不新增skip，不把两项算PASS。局部fake Codex/HTTP/SIWC适配证据也不替真实模型门。

## 当前完整技术门仍有红项

根协调在本批结束后提供**同0032883浏览器CI run36880375418**的独立实际结果：round2/3均有G5首次503后UI重试未成功、F7连续Undo删除后刷新应保两条同title已撤销receipt却仅一条。A核查产品、D保留真实trace和原断言，未重跑。本C报告没有自行验证或修改这两浏览器例，但必须据该独立红证据将完整技术门保留为**未通过**；不能把上述后台exit0宣称产品全门通过。

真实serve/UI其他局部通过由D各自精确头/报告负责；当前浏览器红待修复和有因验证。真实进程崩溃重启（区别SQL显式时钟恢复）、线上11题、真机、一天用户试用及两真实模型opt-in仍未完成。本报告只证明当前合成后端完整回归通过。
