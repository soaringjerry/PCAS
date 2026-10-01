# Phase 2 首次完整 PostgreSQL 回归：失败并提前终止

产品与harness精确HEAD `7877b08fd72b2c0e4422acfb929a9e6935f2eccb`，2026-10-01 UTC，独立clean分支 `phase2/postgres-full-r1`、工作区 `/tmp/pcas-phase2-c`。专属合成DB，每例唯一schema；只有本地fake模型/HTTP，不触及私人数据或其他服务。

先保存 `go test ./internal/postgres -list .` 的[229顶层选择清单](selection.json)，再**仅一次**无筛选执行：

```sh
PCAS_PHASE2_EVIDENCE_DIR=/tmp/pcas-phase2-c-postgres-full-r1-evidence go test ./internal/postgres -count=1 -json
```

保留Go默认十分钟超时；实际18.471秒退出1，未skip/重试/延长超时。执行到 `TestAutoAdoptChangedSince` 测试端第358行nil指针panic后整个包停止。

**229选择；28顶层抵达：23 PASS / 5 FAIL；111终态叶：91 PASS / 20 FAIL；0 SKIP；201顶层未执行。** 已开始测试均在原JSONL记录终态；未执行不计PASS。[未抵达清单](not-reached.json)包含新manual indirect-only/31k/缺target控制、全部Phase2Runtime和原保序五组，此批不能为它们提供通过证据。之前R2/R3各精确版本证据仍独立保留。

## 失败责任与最小后续

| 顶层 / 失败叶 | 原始观察与责任 |
|---|---|
| TestAutoAdoptDestinationsAndUndo / 14 | 7 worker假provider `autoAdoptModel`仍把原正文直接放message.content；真实新Run请求要求严格 `{output,used}`。产品按输入schema拒绝得到failed而无Adopted。另7 manual用动态 `agent="manual"`，未被原直接AgentID盘点覆盖，缺ManualRecipient/GET，第180行Invalid。**C旧协议/漏迁移**，保原每个output/checklist/cost/undo/样本断言；fake按真实协议包装同output/Used[]，manual显式target并GET。 |
| TestAutoAdoptChangedSince / 1 | 同plain fake造成无自动采纳，测试第358行直接解引用Adopted=nil，panic终止全包。**C旧fake+缺前置正断言**；应修共用协议并在取ID前硬断言done/Adopted，失败仍Fatal，不吞panic。未证实真实产品自动采纳回归。 |
| TestEditedArtifactRetainsFieldProvenance / 3 | task/idea第46行仍调用无Store的legacy free `sanitizeItemTx`，对typed ContextTask明确不能验证，故第48行依赖空。project的claim在project创建前已无studio归属，typed hard scope正确不许可普通unscoped fact，正对照第28行缺claim。**C内部wrapper/scope夹具**；用真实Store typed sanitizer/Task，project给原claim同被测事项的studio归属，不制造global或授raw。原依赖/编辑/撤权/删除全部保留。 |
| TestApprovedUndoProtectionPriority / 1 | 第287行VersionConflict发生在fixture直接 `s.commandTx(delegateTask)`，该内部调用缺preparedRunContext。root与A继续核证公共入口边界；建议fixture迁真实Execute保同ActionID，原newer_action/work_started/expiry优先级不变；正式Execute若也冲突须归产品。此批失败仍保留，**不能称undo优先级通过**。 |
| TestApprovedRules_InvalidAliasesNeverFallBackToTHIS / 1 | delegate-new真实DeskTurn未创建原预期附带新Task，delegate receipt skipped，旧Task仅1而预期2；下一N1 add_steps仍skipped。**自然delegate产品准备边界待A核证**，原成功创建/非法alias不落到THIS断言不改，不将成功预期改失败。 |

前三顶层已明确C夹具18失败叶；另两顶层2叶涉及delegate准备边界待正式入口/作者调查。完整原日志包含实际状态与panic栈。此批fake原plain wire没有新增rawbody捕获，协议因果依据冻结helper代码与真实消费者strictJSON及失败Run；不虚构已捕获provider请求正文。10份已有合成输入证据主要为真实manual HTTP package与选择清单。

## 原始证据与门槛

[完整693事件JSONL](acceptance.jsonl.gz)，原JSONL SHA256 `c4017fa4b4a316d5b94b601b8d01361bf79cb2e9bbef1bfe5762975a577cd06e`；[全部10份合成JSON证据](synthetic-evidence.tar.gz)；[状态、整数和逐文件SHA](summary.json)。失败发生后未修到绿或重复执行。原人工gold、正文、撤权/删除/undo/版本、owner独立写作、模型次数与成本断言保留。

下一轮仅在具体产品/夹具修复后root给精确组合SHA有因完整验证。全postgres门仍未通过，正常indirect-only的新构造未尝试调词，31k未缩短；UI/完整Store SIWC由另一独立验收者负责，本报告不代称完成。线上11题、真机、一天用户试用、真实崩溃重启仍无完成证据。
