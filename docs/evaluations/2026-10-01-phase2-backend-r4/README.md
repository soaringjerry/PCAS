# Phase 2 第四次完整后端：memory通过，PostgreSQL仍失败

固定产品及全部harness HEAD **`4c6c929ace4b4d6b7b6561dea28861011af84217`**，clean独立分支 `phase2/postgres-full-r4`，工作区 `/tmp/pcas-phase2-c-postgres-r4`。此头含A全部已审修复、Name独立字段、Prompt子集及真实审计before清理、目标exclude、Service facade、Task JSON及C新Name/D4/facade/wire断言；测试期间未变更头或任何断言。

先确认专属合成loopback PG返回 `phase2_c/phase2_c`，每例唯一schema；真实 `-list .` 分别选择[memory 6](memory-selection.log)和[postgres 238](postgres-selection.log)顶层。[selection.json](selection.json)保存精确版本及全部gold、两包所有测试文件SHA。然后仅一次无筛选执行：

```sh
go test ./internal/memory ./internal/postgres -count=1 -json
```

无重试或新增skip，Go默认十分钟超时。2026-10-01 14:42:13.936828 至 14:46:47.451616 UTC，共273.515秒，退出 **1**。Go memory包0.013秒通过，postgres包272.936秒失败；无panic或超时。

| 包 | 选择 / 抵达顶层 | 顶层 PASS / FAIL / SKIP | 终态叶 PASS / FAIL / SKIP | 未抵达 |
| --- | --- | --- | --- | --- |
| memory | 6 / 6 | 6 / 0 / 0 | 33 / 0 / 0 | 0 |
| postgres | 238 / 238 | 234 / 2 / 2 | 541 / 3 / 2 | 0 |
| 合计 | 244 / 244 | 240 / 2 / 2 | 574 / 3 / 2 | 0 |

共579终态叶、67438条JSONL事件、718份合成JSON（C runtime507、D action211）。父子测试不重复算作叶。顶层全部抵达不表示失败叶Fatal后每个业务步骤已执行。

## 本轮具体通过的边界

- 新memory.Service四方法facade **20叶PASS**：支持repo精确context/owner Scope/typed输入/结果/stamp/actionID及同一错误透传，nonowner/invalid-owner先拒绝，旧Sources-only受控Unavailable且不误调用原文入口。原Ingest/Chunks原断言也通过。这里是实际公共facade的unit能力，不替真实serve接线验收。
- 新Task wire **3叶PASS**：真实workspace.Run Marshal的contextTask全部10个冻结键（ownerId、recipient等及desk_actions）、完整小写recipient route、固定旧大写literal全身份/视图/预算/时间/origins回读、新输出roundtrip。没有UI mock或第二套解析器。
- D4两真实delegate路径全部 **PASS**：real成功delegate receipt恰Prompt origins子集，create/继承ID只位于general闭包；秘书撤权后旧生成Prompt/Output/Brief拒读、旧queue真实HTTP0、旧完成采纳拒绝。真实autoAdopt+undo审计before在正控含source atom及旧完成marker，撤权后实际owner action_log.changes不含二者，真实undone worker audit ID仍在。fresh独立deputy真实HTTP依法供原source atom，同时无旧完成marker且Task/Manifest/general/Prompt origins均为空。原D1–D3也PASS。
- 新owner独立Prompt叶 **PASS**：实际source-derived task上正式owner requestRun，目标deputy独立policy始终allow，真实create origin在general Task而Prompt origins为空；只撤secretary后canonical/Snapshot/Export仍保原owner Prompt。不是以general origins非空猜生成正文。
- Name task/idea叶 **仍FAIL**；但无旧源Name、严格eligible owner Undo成功不复活原Title/Name、真实manual GET有交付且副手实际HTTP普通完成无旧atom这些后续控制均抵达。具体独立owner Title丢失见下表，不能因其余控制通过称Name闭环通过。
- 原五组独立conversation-order、D action-lineage字段矩阵、025及023/024升级/recovery、既有三入口/API/scope/deny/undo/route/ABA、实际bytes/spans、whole metadata/count/body quota及UTF8预算等原断言均PASS。正常容量indirect-only后续kind/revoke/delete、原31k容量拒交付也PASS。
- R3其余红项对应组现在PASS：one-desk逆序Undo、ExistingTHIS逆序Undo、verify反馈完整后缀、D4旧Prompt、UX exclude真实普通完成及下游HTTP无519823。十动作逆序门仍红，不能将部分逆序序列通过推广为全门通过。

## 唯一失败集合：2顶层 / 3叶

| 顶层 / 叶 | 原始观察与因果责任 |
| --- | --- |
| `Phase2RuntimeCanonicalNameOriginSurvivesOwnerTitleRewrite/task`、`idea` | 实际secretary HTTP/exact source Input和真实create成功action均成立，canonical Title/Name原为源atom；正式owner renameThing新Title分别为 `OWNER-INDEPENDENT-TASK-TITLE-681` / `OWNER-INDEPENDENT-IDEA-TITLE-682`，原Name保持源atom，真实owner action/before正控成立。仅撤secretary后Name源atom已清，但**canonical、Snapshot、Export三个当前视图的独立owner Title都被变为“内容已失效”**（name_origin_test.go:189）。均用t.Error保留后续严格真实Undo、manual交付及deputy HTTP控制，且那些步骤成功。产品把独立改名文本仍绑定旧title来源导致撤权误清；只读候选路径 `syncArtifactEditsTx`→`workspace.EditBlocks` 保留替换段DeskActions labels→purge清title；`setField(name)`已独立，不能再修为忽略Name以掩盖来源。此类必须同时保旧Name清除和新owner Title保留，不能接受placeholder或换owner文本求绿。 |
| `SameTurnOriginalTenActionLimit` | 原十动作上限、九个checklist、首次undo第10项成功；第9项在same_turn_actions_test.go:312仍返回精确ChangedSinceAction。该Fatal后的第8→第1步未执行。其他同事务逆序组/50 seeded组通过不能代替此真实十动作边界。产品逆序恢复后的document/blocks fence仍不兼容，责任actions_log/secretary artifact恢复及其hash核查；不把原成功撤销改成允许ChangedSinceAction。 |

两顶层均为实际产品边界失败，C未迁移gold或改断言；任何作者后续修复须另有精确版本和有因完整复验。本次未执行项列表是**顶层0**，上述Fatal后的同叶业务步骤另明确，不冒称其通过。

## 原始证据与仍未完成的门

[完整原始JSONL](acceptance.jsonl.gz)，解压SHA256 **`18de99dd5436a43007eaa4282c88b12611a740c515ee5c2472ee82353487a2d0`**；[718份合成输入/HTTP/manifest/ledger/状态及审计快照](synthetic-evidence.tar.gz)；[每包终态、整数、失败原话、逐份SHA及全部gold/harness SHA](summary.json)；[执行记录](execution.json)；[各包未抵达清单](not-reached.json)均[]。同名文件若后续状态覆盖，完整原始JSONL仍逐事件保留全部t.Log JSON，包括Name撤权前后及Undo后所有视图，不仅留最后快照。旧R1/R2/R3红报告没有覆盖。

两项既有live opt-in SKIP（均在postgres）：`TestLiveCodexSecretaryAndLegacyFormats` 需专用已登录Codex home；`TestLiveContinuityReplay` 需真实embedding+专用Codex home。本次没有设置真实账户/模型，不新增skip、不把两项算PASS。完整合成后端仍红。

D独立真实serve/UI结果由D自己的精确头及日志负责，本批不替其结论，也不当真机或真实模型通过。真实进程崩溃/重启（区别SQL显式恢复时钟）、线上11题、真机及一天用户试用仍无完成证据。
