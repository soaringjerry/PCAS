# 模型动作字段跨角色供给：首次真实最小复现失败

首次正式执行1 failed、0 passed、0 skipped，退出1，0.466s，count=1，无重跑。真实产品缺陷：仅secretary授权的raw source经模型create_task与add_steps写入title/notes/check后，三个派生marker进入没有该source许可的fresh ManualRunPackage；manual清单Input和Indirect为空。需要业务字段来源链修复，不能以测试迁移或弱化断言消除此失败。

## 精确基线

- 产品：fb1c3eae593527640de156fff806d8789ab5e282（已含delegate修复，尚无action字段谱系）。
- 冻结预期与初harness：7f472dba754033ba9b4dfb0bbd7a68934b8ecd9e。
- 正式执行harness：336bf00fb2f3c88f4eb796261e303285a6ce5897（只把测试环境gate适配为显式loopback合成数据库规则）。
- 隔离工作区/tmp/pcas-phase2-action-lineage-repro，分支phase2/action-lineage-repro；只新测试文件与对应docs，未改产品、原测试/gold/helper。
- Go1.26.8，协调者指定的127.0.0.1:33273/phase2_c合成PostgreSQL；helper真实Migrate/独立schema，结束cleanup只DROP己schema。未生产/真实账号/真实模型。

两次compile-only（初版与CI安全环境gate变更后）都退出0，正式仅一次：

```sh
PCAS_TEST_DATABASE_URL='postgres://phase2_c:phase2-synthetic-only@127.0.0.1:33273/phase2_c?sslmode=disable' \
PCAS_PHASE2_ACTION_EVIDENCE_DIR='/tmp/pcas-phase2-action-lineage-repro/docs/evaluations/2026-10-01-phase2-action-lineage-evidence' \
go test ./internal/postgres -run '^TestPhase2ActionLineageSecretaryOnlyFieldsDoNotReachManual$' -count=1 -v
```

原日志：[初compile](2026-10-01-phase2-action-lineage-compile-original.log)、[CI安全gate compile](2026-10-01-phase2-action-lineage-compile-ci-safe-original.log)、[首次正式失败](2026-10-01-phase2-action-lineage-run1-original.log)。

## 实测正控与失败

1. 公开Ingest建立合成source，claims=0；公开SetSourceAuthorization授予phase2-model/secretary；SourceAuthorizations确认为一条secretary角色政策。
2. 真实DeskTurn actual HTTP正文含三个源atom，持久secretary manifest.Input exact source id/version/kind，Used声明经产品标supported。模型正常返回create_task(title+notes)和add_steps(N1)，两个receipt均done，实际owner Snapshot含三字段marker。
3. 公开owner命令追加独立notes/check；公开requestRun明确manualRecipient provider=phase2-model，可信manual角色存在，仍无manual source政策。
4. 真实ManualRunPackage成功交付PCAS package，delivered_at存在，external_receipt=unknown，没有额外模型HTTP发送。实际package包含三个禁用派生marker，触发三条冻结安全断言失败：

```text
secretary-only source-derived field reached unauthorized manual package: DERIVED_ACTION_TITLE_731
secretary-only source-derived field reached unauthorized manual package: DERIVED_ACTION_NOTES_842
secretary-only source-derived field reached unauthorized manual package: DERIVED_ACTION_CHECK_953
```

owner独立OWNER_INDEPENDENT_NOTES_164与OWNER_INDEPENDENT_CHECK_275仍在package，未触发保留断言失败。manual manifest.Input=[]、IndirectDependencies=[]，证明这些内容由事项字段复制进Brief/package，未作为来源关联受到当前角色验证。

完整[合成证据JSON](2026-10-01-phase2-action-lineage-evidence/minimal-repro.json)保留source policies、secretary request/actual HTTP body/attempt/响应/actions/receipts、独立owner编辑后的完整state、实际manual run、fresh package及其attempt/manifest。没有因为预先静态发现缺口而放宽assert，也没有跳过失败。

本报告证明PCAS把未经manual角色许可的派生内容交付了手动package，不证明外部模型收到或使用。本轮尚未测deputy、多动作独立矩阵、撤权/删除、undo/promotion、诊断到期；后续设计见[冻结预期](2026-10-01-phase2-action-lineage-expectations.md)，等待实现者共享field provenance契约后另以新产品SHA一次验收，不拼接成已完成大矩阵。
