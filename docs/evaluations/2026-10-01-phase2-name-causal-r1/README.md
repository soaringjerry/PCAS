# Name与十动作Undo一次因果定点：4顶层 / 12叶通过

精确产品+harness **`e6fd9b1902db7e3e05b882094027ff8841ecf1ab`**，clean独立分支 `phase2/name-causal-acceptance`，工作区 `/tmp/pcas-phase2-c-name-r1`。按root批准选择[实际四顶层及人工12叶](selection.json)，gold/所有PG测试SHA一并冻结，随后仅一次：

```sh
go test ./internal/postgres -count=1 -json -run '^(TestPhase2RuntimeCanonicalNameOriginSurvivesOwnerTitleRewrite|TestPhase2RuntimeOwnerPromptDoesNotInheritGeneratedPromptOrigins|TestPhase2RuntimeOwnerRenameCannotLaunderKnownSourceText|TestSameTurnOriginalTenActionLimit)$'
```

专属合成PG每例唯一schema、fake provider，无重试/断言修改/skip。2026-10-01 14:56:09.017059 至 14:56:17.490560 UTC，8.473秒，退出 **0**。**4选择/4抵达，4顶层PASS；12真实叶PASS，0FAIL/0SKIP/0未抵达。** 实际终态叶集合与预先人工12叶逐名相同。使用全部祖先排除计数，见[勘误规则](../2026-10-01-phase2-backend-counting-erratum/README.md)；复合子例的父终态不重复计叶。

- task/idea两个独立owner Title正例：真实secretary source Input、成功create Title/Name正控，正式owner改名真实action/before，撤secretary后canonical/Snapshot/Export清旧Name而保持owner Title；精确eligible Undo成功不复活原源Title/Name；真实manual GET交付及deputy实际HTTP普通完成无旧atom。
- owner独立Prompt正例：目标角色独立allow、真实action字段事项上亲写Prompt，general origin非空/Prompt subset空；仅撤secretary后canonical/State/Export保独立Prompt。
- 四复用文字×task/idea八负例：复制、标点、完整PIN、PIN片段通过正式rename，title仍保真实create origin；撤权后三视图与实际manual/deputy bytes都无**实际LANTERN-482片段**，普通工作仍完成。未复制匹配算法或调词重试。
- 原十动作上限及全部10→1逆序Undo严格成功通过，没有把ChangedSinceAction改成可接受。

[完整14479事件JSONL](acceptance.jsonl.gz)解压SHA **`319208006ff3ed555329815fe98cdf715fbe3b614f9d839c5b532f574f6ec9bd`**；[139合成输入/实际HTTP/manifest/状态及audit JSON](synthetic-evidence.tar.gz)；[正确叶集合/逐份SHA](summary.json)；[完整执行记录](execution.json)。同名最后状态文件不代替JSONL逐事件完整快照，原R4失败证据保持。

本定点不代表完整两包或真实模型通过。root随后要求仅补AfterBlocksHash非nil的restore-marker前置，完整门使用新精确0032883，不重复此次定点。两live opt-in在本选择之外，不计已执行；线上11题/真机/一天试用仍未完成。
