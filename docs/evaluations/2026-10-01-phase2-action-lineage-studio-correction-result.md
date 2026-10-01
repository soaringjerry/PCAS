# Studio exact scope：批准的两叶有因补验结果

产品仍精确 `9d60f310beec1748b4e99ed22a3e86b4493cd1e9`；执行/harness HEAD `63fc697e711b1e821e06c2b29d2be93f159c92d5` 仅把三个 studio fixture flag 显式对齐实际 Task，未改任何原业务断言或产品。测试文件 SHA256 `3f4ec7c400f9d27bb0e0adb10cae253f73ddf2983fb13612df08867aa27cc8a6`；gold/runtime-sequences/只读helper SHA与首次冻结一致。

root 依据首次真实 Task/authorization exact tuple 的 true/false 差异批准；不是随机重跑。先净提交后仅一次：

```text
go test ./internal/postgres -run '^TestPhase2ActionLineageMatrix$/(mixed_update_and_stable_ids|role_and_studio_scope)$' -count=1 -v
exit 0; package 3.145s
```

一个 Matrix顶层与两叶全部 PASS：mixed_update_and_stable_ids 2.33s，role_and_studio_scope 0.81s。无 skip/retries，其他十 Matrix叶与原红复验未选中、未重跑。

此前失败的两个 studio 生成现在实际 Task.scope 与原秘书 policy 的 include_global_constraints 均为 true，真实 HTTP 供给正控、exact source Input/action durable deps 成立。随后原强断言全部通过：notesAppend/task/idea/project 的 owner段保护；稳定 check/condition ID；未授权 manual 遮派生；错误 role、错误 studio 拒供；准确 studio 真供给并记录当前 recipient 的 exact Indirect policy/scope stamp。错误 studio B 的 flag 同为true，只保 studio ID 为有意差异。

本新原日志、完整生成/真实HTTP/Task/manifest/包/ownerstate/origin/audit JSON另存 studio-correction-evidence。首次正式13叶11PASS/2FAIL、exit1、29.652s及原211JSON始终保留在 formal-result/log/evidence，不改写为首次全绿。此前十一叶成功与本两叶有因补验是不同 harness批次；本补验不是完整矩阵重跑，更不是最终完整 PostgreSQL gate、新 UI 真实浏览器闭环或外部 manual 接收证明。

原日志与全部 JSON 已按原路径打入[完整证据压缩包](2026-10-01-phase2-action-lineage-studio-correction-evidence.tar.gz)，用于缩小 PR 的文本差异。[逐文件字节数、SHA256、压缩包校验值及还原命令](2026-10-01-phase2-action-lineage-studio-correction-evidence-index.json)均保留，归档前后逐项相同。仅改变证据存放形式，未改变原结果、测试或门槛。
