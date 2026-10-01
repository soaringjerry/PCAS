# 动作字段谱系：指定组合头首次正式结果

产品精确 `9d60f310beec1748b4e99ed22a3e86b4493cd1e9`，本地执行 HEAD `e5c5fd553e6c63a38311cbbfca5b7a85bc405d3e` 仅额外冻结文档；product/测试/gold 内容未改。独立 harness 原提交 `4ccb05b9e7a9fd4ccab822bca557855d51ffbc54`，组合对应 `0dcee75`；测试文件 SHA256 `12c77017ae46479d8908fc24e100c28871468c70b2355ecd2382c71322398e21`。gold/runtime-sequences/helper exact hashes见执行前 preflight。

在隔离 worktree `/tmp/pcas-phase2-action-lineage-formal`，使用指定合成 `127.0.0.1:33273/phase2_c` 与每叶己schema，实际 httptest 模型HTTP，仅正式执行一次：

```text
go test ./internal/postgres -run '^TestPhase2ActionLineage(SecretaryOnlyFieldsDoNotReachManual|Matrix)$' -count=1 -v
exit 1; package time 29.652s
```

两顶层：1 PASS（原红复验0.71s）、1 FAIL（Matrix28.94s）。13叶全部抵达：11 PASS、2 FAIL；未执行0、skip0、重试0。原 fb1c3eae 首次红诊断与原日志另存，不改为绿。本批不是全 PostgreSQL 回归，也不是新 manual UI 的真实浏览器闭环。

| 叶 | 首次结果 | 秒 |
|---|---|---:|
| 原 SecretaryOnlyFieldsDoNotReachManual | PASS | 0.71 |
| create_denied_used_empty | PASS | 2.11 |
| dual_role_direct_and_indirect | PASS | 2.51 |
| dual_role_check_only_indirect | PASS | 1.90 |
| mixed_update_and_stable_ids | FAIL | 2.31 |
| role_and_studio_scope | FAIL | 0.67 |
| original_route_changes | PASS | 5.25 |
| revoke_regrant | PASS | 3.81 |
| source_correction | PASS | 1.82 |
| source_delete | PASS | 1.79 |
| undo_before_blocks | PASS | 1.92 |
| promotion_and_two_origins | PASS | 2.67 |
| diagnostic_and_audit_expiry | PASS | 2.18 |

## 两处失败责任：D studio 夹具 exact scope 不匹配

两个失败共同出现秘书真实 HTTP 缺全部六个 source atom、manifest.Input=[]、成功动作没有对应 source durable dependency，之后 fake 输出 marker 在 manual 可见。必须先解释正控失败，不能把 fake 自己写出的同文字当实际原文泄漏。

持久证据明确：

- mixed/project 原实际 Task.scope：`{kind:studio,studio_id:db6b4777-00b3-4191-9f60-dcd192194f31,include_global_constraints:true}`；D 正式 secretary authorization 同 studio ID 但 `include_global_constraints:false`。
- role_and_studio_scope 原实际 Task.scope：`{kind:studio,studio_id:d851b71f-3840-4d4b-b53b-899e9d3eb618,include_global_constraints:true}`；D 正式 secretary authorization 同 studio ID 但 `include_global_constraints:false`。
- 只读产品 `context_recipient.go:contextScopeForItem` 对实际 project/带 ProjectID 的 item 返回 include_global_constraints=true；`sourcePolicyTuple` 将此 flag 纳入 exact tuple。两个源未供给的失败与此差异一致，既无 source Input、也无候选。本次角色/范围正负控没有满足所冻“先实际供源”的前提，不能称该矩阵覆盖通过，也不是已证实新的产品泄漏。

最小修正建议：仅将 D studio A/project 正控与 studio B 有意错误scope fixture 的 flag 对齐实际 server Task=true，保持 B 的错误 studio ID 作为唯一范围差异，保原源atom/typed依赖/所有拒供断言。已先报告 root，未自行改预期、未重跑。若批准，有因补验仅这两叶且保此首次原红日志；其余十一叶原结果不重复执行。

## 已通过序列的实际边界

原 red repro 在新头实际不再转供仅授权秘书的 title/notes/check，owner独立段保留。矩阵中的实际 Input+Indirect 同 ref、check-only Indirect 与 empty Used均过；original_route 叶分离 producer/target provider，保目标自己的完整 recipient/Task 正控，真实旧包重取、queued deputy dispatch 与 undoAutoAdoption 后已完成结果采纳拒绝均过；新三出口保 owner。target-only deputy revoke 未把合法 secretary origin 永久 stale，之后撤原 secretary/regrant拒旧段；source更正/删除、undo、真实 promotion 两种 origin、attempt/audit期限后的合法读取与撤权继续生效均在对应叶通过。

这是本两顶层真实 Store/合成 HTTP 测试的证据，不代表真实第三方模型内部上下文、外部 manual 接收或手机结果。

原完整日志 `2026-10-01-phase2-action-lineage-formal-run1-original.log` 与 `2026-10-01-phase2-action-lineage-formal-evidence/` 中完整生成/HTTP/Task/manifest、包、ownerstate、origin/audit JSON一并保留。model是本地fake，材料均合成，未记录真实账号凭证，未触碰旧 checkout/部署。

为缩小 PR 文本差异，上述原日志与全部 JSON 现按原路径打入[完整证据压缩包](2026-10-01-phase2-action-lineage-formal-evidence.tar.gz)。归档前后逐文件字节、SHA256 均核对相同；[文件清单、压缩包校验值及还原命令](2026-10-01-phase2-action-lineage-formal-evidence-index.json)随包保存。原失败计数、内容、预期和门槛未变；解压可恢复上述全部路径。
