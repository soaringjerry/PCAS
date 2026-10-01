# SourceRun retry/query/receipt 因果首轮：原红保留

固定产品+harness `834949f44e257af2e97c8b76338bb60304c67a41`，新隔离tree `phase2/retry-causal-r1` clean后实际list。[selection.json](selection.json)固定5顶层、23人工叶逐名、全部gold/tests/contracts/browser spec SHA；仅一次：

```sh
go test -race ./internal/postgres -count=1 -json -run '^(TestPhase2RuntimeRetryPreservesGeneratedPromptOrigins|TestPhase2RuntimeRetryManualAndFinalFence|TestPhase2RuntimeOwnerUndoReceiptAuditNeverRestoresSourceText|TestPhase2RuntimeRetryQueryIsolationAndDestinationExclusion|TestContinuationHonorsCurrentItemScopeBeforeSemanticRetrieval)$'
```

**退出1；5顶层3 PASS/2 FAIL；23真叶17 PASS/6 FAIL；SKIP与未抵达0**。包17.298s，墙钟25.057s（含race编译）；15729 JSONL事件，207份必要合成JSON；无race告警/panic/timeout。叶定义排除所有实际run名字的proper slash-prefix祖先，实际叶集与23人工计划完全相同。[summary.json](summary.json)逐名终态与原failure文字。此轮不运行完整PG或live opt-in，完整CI/真模型/线上11/真机/一天试用另计。

[完整JSONL](acceptance.jsonl.gz)解压SHA `b4ffb259884dd2f4eabd7d69e34d86d11e770620b95c6805b3bb7e58c5f4dad4`；[全部payload/manifest/row waits](evidence.tar.gz)、[逐文件SHA](evidence-sha256.json)。便利同名attempt/snapshot文件会被该叶后续记录覆盖，原JSONL仍完整；[SOURCE两个attempt从raw重建](receipt-source-attempts-from-raw.json)直接保留first/second精确输入。没有重跑、改产品或删红。

## 唯一六个失败与独立归因

| 失败叶 | 原失败 | 因果判断/后续批准的最小提案 |
| --- | --- | --- |
| RECEIPT-EMPTY | HTTP receipt Undoable=true/Undone=true，而C要求Undoable=false | C额外要求未在人工gold中固定。Undoable是动作历史capability，refresh仅更新Undone；UI按Undone禁当前操作。保严格真实两Undo/双title/actionID/Undone，不将capability当当前可撤状态。 |
| RECEIPT-SOURCE | 同一额外Undoable=false Fatal | 原首create Model Task.DeskActions=[]、source原grant仍live；正常合法首历史保原title，不通过新stale审计特例。second依赖已撤create action，实际已generic、无ThingID/atom，Reply隐藏。原whole-response无atom预期缺真正source失效前提；后续应追加正式source revoke→两个turn均无atom/title/保actionIDs与Undone，保原首红/gold而不覆写。 |
| RETRY-LIVE | studio秘书delegate:new真实skipped，无Run | C fake没给目标项目；schema delegate:new无project字段，规划/执行新Task默认unscoped，req.ThingID不是项目继承。因此A-only政策正确拒绝此新Task。应正式create_task(project=实际当前A alias)→N1 delegate，真实Task/两角色scopeA与delegate Prompt origins正控保留。 |
| RETRY-SCOPE-DENIED | 同studio setup先止，尚未抵达B拒控 | 同前提错误。修setup后仍给同source真实B canonical allow，只A assignment，同Task移动B，再严格拒供，不能用没有policy遮盖硬范围。 |
| RETRY-INVALID-DELETE_SOURCE-ORIGINAL_PROMPT | C要求Conflict，实际NotFound | 删除editing.go544按真实run_dependencies删除旧agent_runs，SourceRun locator queryDocument先NotFound；不抵达producer Conflict。后续先证旧Run删除、source不在/opaque tombstone与reimport block真成立，再仅此两叶严格NotFound/HTTP0/newRun0/不复用旧attempt。 |
| RETRY-INVALID-DELETE_SOURCE-PUNCTUATION_ONLY | 同上 | 相同定位gone原因；revoke/correct的四叶继续严格Conflict，不能宽泛接受任何error。 |

首SOURCE HTTP显示首create title `RECEIPT-AUDIT-SOURCE Q7-LANTERN-482`仍存在、second generic正确。第一次报告一度将首title列为待查泄漏候选；独立查看实际Manifest与source-policy序列后确认，actionUndo不等于raw source删除/撤权，不能据此称新审计例外漏放。两个实际action source exact-dependency SQL正控均抵达并通过，但Fatal之前未输出完整原action context_task JSON；报告不能虚称该直接记录齐备。后续追加更早 evidence capture。

root与D分别独立批准因果修订；此处是原失败责任核查，不修改六个FAIL终态或旧预期文件。尚无有因复验结果。

## 本轮实际通过的必要边界

- 原无效secretary revoke/correct四叶：精确Conflict、无新模型/embedding；独立target未获准、crossowner/crossThing、owner独立Prompt、新获准target等通过。
- 两manual叶真实GET交付/DeliveredAt/unknown/Prompt origins与exact-source Indirect；secretary-only撤权后的标点改写拒交付。
- final真实owner行锁：fixture PID5375，formal revoke PID5373以transactionid等待fixture，retry PID5374以tuple等待5373；先观测两条真实wait才释放。正式revoke回执/持久revoked+explicitDeny/revision2已证；retry final Conflict、新Run0、生成HTTP0；没有SQL fabricated prepared状态或embedding barrier替代。
- Generated retry embedding0；owner currentPrompt-only vector正控、秘书本轮req.Text-only、formal claim exclusion在prepare即拒、旧UX三叶真实GET Ordinary permitted plan与482910/ContextMemoryIDs/scope负控全部通过。三份实际embedding请求：owner独立Prompt、秘书当前query、独立claimRecall正控；无generated retry query。

代理真实外层provider wire记录34次；两个receipt叶各2真实模型调用、秘书query独立叶2调用另计，总40本地合成模型HTTP，另3 embedding HTTP。所有数据合成；fake provider不能证明模型推理或真实模型效果。合法package包含history证明续作保留，未冒称动态抓取了完整lexical query；界面gap没有在该测试声称通过。
