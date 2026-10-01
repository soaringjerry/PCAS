# Phase 2 首次完整组合动态验收：未通过

## 精确版本与一次执行

产品与运行 harness HEAD **`c621548b41778fa913c2b7fff6ffb1e1886c3826`**，2026-10-01 UTC，独立分支 `phase2/runtime-full-r1`、工作区 `/tmp/pcas-phase2-c`。逐例专属合成 PostgreSQL 唯一 schema，本地假HTTP模型；原会话后台连接终止只操作该例自己的backend PID。

**退出1；31顶层：21 PASS / 10 FAIL；69叶级：41 PASS / 28 FAIL；0 SKIP。** 执行一次，未改产品、未重跑、未削弱gold、未改原会话五组断言。

```sh
# 专属 PCAS_TEST_DATABASE_URL 从环境提供，不写入凭据
PCAS_PHASE2_EVIDENCE_DIR=/tmp/pcas-phase2-c-runtime-full-r1-evidence go test ./internal/postgres -run '^(TestPhase2Runtime.*|TestConversationOrderAcceptance_.*|TestDirectCaptureEntersDefaultRunAsSourceBacked)$' -count=1 -json
```

[31个精确选择名称](selection.json)：所有25 Phase2Runtime + 原ConversationOrderAcceptance五组 + 指定旧manual claim fixture。AI/d8/K0没有混入。

## 失败责任与未抵达部分

| 顶层 / 失败叶数 | 责任和真实观察 |
|---|---|
| 原 DeletedRecoveryDoesNotResurrectThroughHTTP / 1 | **产品 A**：旧断言540要求删除后的scrubbed审计骨架回执，实际Reply被通用‘这条回答依据的记忆已变更’替换。未改旧断言，交A；另外四原会话组通过，包括真实backend终止及迟到输出隔离。 |
| MetadataAndCountCapacityRejectBeforeExternalSend / 2 | **产品 A**：10000 skeleton与wholemetadata仅余128B都没有目标HTTP外发，但返回结果缺冻结的受控record_capacity恢复信号。拒发事实与反馈故障分别记录。 |
| ProviderBarrierMutation… / 6 | **C diagnostic adapter**：ContextAttempts(operationID="")被误当owner全列表，实际API只按给定operation过滤，故后置dispatch证据搜索空。原source mutation均在HTTP已收且response仍held时提交；不能把返回空当产品丢attempt。Run还受下述假响应格式影响，不能证明其迟到结果拒绝是policy原因。 |
| BeforeDispatchFence… / 8 | **C diagnostic adapter**：同空operation ID，prepared阶段读诊断返回0并Fatal，尚未执行mutation→最后fence完整序列。本次只能证明已到真实before_dispatch观察边界，不能宣称已验证known mutation取消。 |
| ZeroClaimNaturalAndPublicActualInputs / 4 | **C diagnostic adapter**：真实自然/公开三入口最终输入gold atoms已抵达，但完整manifest/snapshot断言因空operation取不到attempt未抵达。Run另因fake未回output而failed；不当作产品来源供给缺失。 |
| ManualPackageInvalidationAndRegrantABA / 2 | **C diagnostic adapter**：真实GET交付后空operation搜索Fatal，后续撤权/删除/重取/提交/ABA未抵达。 |
| OwnerBodyQuotaReclaimsOldestBeforeExactSend / 1 | **C diagnostic adapter**：旧body初态64MiB且真实新HTTP发生；后置exact snapshot helper Fatal，最老body状态/owner最终sum尚未验证。 |
| StateExportAndDerivedBlocks… / 2 | **C fake model response**：API通道GenerateWithSearchSchema没有response_format，模型需求在system明确output；C fake只查看response_format而回reply，Run得到错误schema，source-derived正对照未建立，尚未测试route/disabled读gate。 |
| LegacyPureClaimReadControlAndExplicitDeny / 1 | **C fake model response**：同错误响应，pure-claim实际完成结果正对照未建立，legacy兼容/deny未抵达。 |
| RetentionKeepsDurableInvalidation / 1 | **C fake model response**：同错误响应，真实完成Run正对照未建立，body/metaexpiry与durable late-revoke未抵达。 |

计数：产品失败2顶层/3叶；C适配阻断8顶层/25叶。C只在原批证据冻结后作最小测试适配：按真实request/run/operation ID读取正式诊断（必要owner-only SQL列操作ID，再逐ID实际API读取）；fake按实际system的output需求响应同一人工answer/Used[]。不会改产品API为全列表，不改原gold/业务断言；本报告不会因后续修复变为PASS。

## 通过和部分输入观察

21顶层通过包含：来源API/typed读/范围/当前态undo7组，source ancestry三控制，实际cancelled send独立attempt，same/newconversation撤权历史，重复短句与中文escaping原位置映射，UTF8预算23999/24000允许及24001拒发，fresh/023/024/recovery四组，旧manual claim真实GET交付，以及原保序四组。完整逐例与叶级状态在summary，不把局部通过外推为全runtime通过。

自然授权真实owner句‘让秘书能用《成都预约资料》’及公开秘书/副手/manual后续输入中的三原gold atoms确实出现在实际fakeHTTP正文或PCAS package；zero claim + pending source条件记录。最终全部manifest/deps/exact retained snapshot审计因C诊断读取适配失败尚未闭环，**E1完整验收仍未通过**。Used[]不作为无供给证据，实际input与间接deps继续分开。

## 原始证据和后续

[完整原JSONL](acceptance.jsonl.gz)：7792事件。[183份合成原输入/HTTP/manifest/状态/selection](synthetic-evidence.tar.gz)及[全部SHA/整数/每例状态](summary.json)。HTTP输入body_base64与SHA来自实际接收bytes，不冒称第三方内部上下文；manual receipt保持unknown。没有真实model/私人数据/通知。

下一轮需root给明确产品修复+C适配后的精确SHA作有因验收；本批不重复。全retention/quota/读gate/manual生命周期/准备到fence仍有未抵达项；之前迁移恢复和AI独立报告保留各自精确历史。线上11题、真机、一天用户试用仍未完成。
