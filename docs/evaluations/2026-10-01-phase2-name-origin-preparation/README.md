# 独立Name与Prompt来源预期冻结

本预期在R3完整红证据归档后新增，不修改原gold、D矩阵或207bd706运行版本。批准契约分别为9f15f8f（Title/Name独立来源）和b0e2ec5（实际生成Prompt子集）。[Name三叶人工预期](../../../testdata/phase2/name-origin-sequences.json)及[D4新增控制](../../../testdata/phase2/delegation-origin-supplement.json)先冻结，再实现C独占harness；动态等待root指定完整组合。

两个核心task/idea叶采用真实secretary Input、create成功actionID、正式owner rename、正式仅secretary撤权，检查canonical SQL/Snapshot/Export旧Name清空而owner Title保留。没有后继业务编辑，正式owner Undo必须成功且不复活原源Title/Name。最后manual真实GET交付与副手真实HTTP普通完成均必须无旧atom，不能把提前Conflict当安全通过。

第三叶独立验证owner亲写Prompt，先给目标deputy独立许可，在真实action字段事项准备Run；general origins非空、Prompt origins空。仅撤原secretary后canonical/Snapshot/Export保留owner Prompt。单独schema隔离，避免污染前两叶Undo优先级。

D4仍保持R3两个旧生成Prompt红断言；另核生成Prompt subset恰本次成功delegate actionID，create及继承IDs不在subset。fresh独立deputy依法供原atom，实际HTTP不得有旧完成marker，Task/Manifest origins为空。此负控与Name核心叶无旧atom要求处于不同授权状态。

这里没有动态结果，不代表Name、Prompt、真实serve、UI或最终完整后端已通过。原逆序Undo、原022→023/023→024/025、D字段矩阵断言保持。

## Harness交付（尚未动态）

新增 `phase2_runtime_name_origin_test.go` 两个顶层共三叶，D4原两叶新增Prompt subset、审计before副本、fresh旧marker缺席及空origins断言。全部业务正文仅来自正式DeskTurn/Execute/API；SQL只读取canonical、真实action记录及其原before快照。D4使用真实自动采纳action ID，先证明before快照确含source atom与旧完成marker，再撤权后核整个owner action_log.changes清除二者，且该真实undone worker审计行仍存在。

[编译记录与独立gold/harness SHA](compile.json)：第一次C新增harness编译发现memory.ID与string helper类型不合，修为直接类型相等比较后，`go test ./internal/postgres -run '^$' -count=1`退出0、无测试运行。此编译在原207bd706产品上完成，不表示新字段或Name契约已动态通过。原D4/context snapshot/HTTP/Undo严断言保持。
