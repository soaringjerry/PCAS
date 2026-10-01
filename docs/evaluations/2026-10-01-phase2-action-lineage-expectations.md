# 模型动作字段来源链：最小独立复现预期（冻结）

产品精确基线fb1c3eae593527640de156fff806d8789ab5e282（已含delegate修复，尚无动作字段来源链）。隔离分支phase2/action-lineage-repro，工作区/tmp/pcas-phase2-action-lineage-repro。只新增internal/postgres/phase2_action_lineage_test.go和本命名前缀文档；原gold/helper/tests与产品不改。

所有材料均为合成。使用原phase2RTSetup只读测试helper启动真实Store/唯一schema/loopback HTTP fake。独立source literal含3个不同atom；零claim且只有正式SourceAuthorization API授给secretary角色，manual可信target存在但没有该source许可。模型输出create_task的title/notes与add_steps(ref N1)写3个派生marker；后续另用公开owner命令追加独立说明与check。

冻结最小行为预期：

1. 实际DeskTurn完成，fake服务真正收到的秘书HTTP正文包含三个source atom；持久manifest.Input列exact source ref，作为来源供给正控。
2. 实际owner Snapshot中的title/notes/check包含三个派生marker；owner独立说明/check完整保留，证明派生字段确实写入，而非fixture凭空指定。
3. 正式requestRun选manual与已配置provider，不带来源授权；实际ManualRunPackage不得含任何secretary-only派生marker，也不得含raw source atom；owner独立说明/check应继续供给。manual attempt.manifest.Input/Indirect不得声称获得未经许可source。
4. 原始秘书HTTP input、两角色政策、ownerstate、fresh ManualRunPackage、其attempt/manifest及退出均保存。观察到泄漏时，按预期失败并保留原日志；不skip、不force、不弱化assert、不以重跑取绿。

运行前先commit预期+harness，再compile gate，然后正式最小复现一次。只使用协调者指定loopback合成PostgreSQL，己schema清理，不生产/真实账号/真实模型。

## 后续矩阵设计（尚不宣称实现通过）

- create_task(title/notes)、update(title/notesAppend)、add_steps(check)分别验证deputy/manual无相应raw许可时拒供。
- 相应双角色授权的正对照允许派生字段；保exact source/version/policy依赖，不把source改成claim。直接供给与Indirect区分；check-only场景避免query读取check，以强制验证exactsource Indirect链。
- owner独立notes/check与派生块混排；撤权/删除清派生、保owner原文，后续consumer再验。
- update撤销后恢复历史派生文本不能洗去来源；idea body→promotion复制到task.notes仍保原依赖。
- 诊断body/metadata到期仅影响诊断保存，不免除durable业务来源链；cleanup后撤权仍拒供。

后续矩阵等待协调者与实现者确定共享field provenance契约，再冻结精确用例/断言/计数并一次执行。本轮最小复现不覆盖整个后续矩阵。
