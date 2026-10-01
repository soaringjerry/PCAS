# 正式 owner rename 与 Undo 恢复边界：独立静态审阅

2026-10-01，D 独立只读。交付树基线 root `216704b`；契约 `3cc9004a8bb5521c829b3b6b55107e0aa662d9af`，正式 owner rename 产品 `68a7579b8cda9a1d9b38f1adba544557901baf74`，Undo 窄修 `b80aab1211dc50042da40c7f2b56e4db59cc5bce`，本审阅发现的双 hash 前提修正 `cf84241d81e7fb0232e3eb59bd20aad2bcc4d55a`。最终复核 root 组合 `0032883950d7dc72b1bdb160c1a98e3da17eded9` 的实际 actions_log 代码与该净补提交一致；A 树在 cf84241 clean。

**最终未见本审阅范围内尚未关闭的静态阻塞。此结论仅静态，不是动态安全矩阵通过。** 没有写产品/测试/gold，没有运行 build、测试、浏览器、模型、DB或追加套件。C 独立编写 Name 负例及执行定点/完整门；当前此前已启动的 e6fd9b1 定点与最终含 cf84241 的完整组合是不同批次，本报告不猜其未返回结果，不用此前绿代替新门。旧两组 action-lineage evidence/原结果归档由 root 独占，本次未改。

## 1 正式 owner 整字段改名

| 核点 | 净代码证据与判断 |
|---|---|
| server marker 的权威 | artifact_blocks.go 的 ownerRenameKey/ownerRename 是私有context类型，withOwnerRename要求Scope.IsOwner、Scope.Valid、actor=user、actionLog.source=command且没有secretaryArtifactContext。唯一产品调用在commandTx正式renameThing分支。客户端Command/模型JSON没有marker字段，客户端传回来源块/Task不产生该信任。 |
| 同item/实际文字 | marker保存从真实getItem读取的item.ID与c.Title；sync严格核rename.ThingID==item.ID及rename.Title==当前field text，不仅检查请求type或owner。saveItem原ID/Title非空及2000-byte拒绝保留。 |
| 模型、notes、undo隔离 | DeskTurn内部renameThing带actor=secretary、source=desk和secretaryArtifactContext，不能获得owner marker；普通setNotes/updateTask/updateProject也没有marker调用。undo恢复不走renameThing，使用下一节独立受双fence限制的restore marker；不是以user身份就能触发rename特例。 |
| 只解绑实际整字段 | 特例仅Title，project正式renameThing同时Name。task/idea改Title没有给Name独立作者标记，旧Name原块与来源仍存在；旧Name也在复制来源集合中，不借改Title绕过它。 |
| 完整新名与复用 | RenameBlocks构造完整新title的一个block，只有所有相关可核来源都未匹配时才独立。任向包含、同词至少2字符、归一化连续3字符（含PIN片段）、原copiedBlock双向相似规则任一成立，就分别累积该源Runs和DeskActions。不以模型Used空、只改标点或改少量字洗来源。special rename随后不再次调用CopyOrigins，避免已经判为完整独立的新名被旧位置hunks再染色。 |
| 原Name/失效清理 | title/name分别作为artifact_fields。task/ideaName没有特殊解绑；既有setField/sanitize/purge/restore分别读写Name，原派生Name在撤权/失效仍可定位清除。project同次改名两字段用相同完整text及固定sources，结果一致。此为静态路径证据，实际Name撤权/undo结果由C动态验。 |

名称来源包括该item已跟踪的各字段，含保留Name、审计与source label；没有仅用Title当前字符串作为来源。普通workspace.EditBlocks和CopyOrigins实现未在这两次产品窄修中改动；普通notes、模型输出/追加、一般patch继续保守原归因。rename并不授权当前consumer消费旧源、重新绑定route或修改origin durable deps。

## 2 有限文本比较与行序

`syncArtifactEditsTx` 查询artifact_fields增加 `ORDER BY field`；persisted JSON blocks保持数组顺序，sources按固定field→block顺序构建。itemArtifactText的map遍历不会消耗全字段共享预算：每次RenameBlocks调用从同sources独立重置64KiB/100000预算，projectTitle/Name同text因而不受map顺序影响。Name在有限预算中的相对位置确定，不随PG无序行偶然解绑或继承不同来源。

新比较的输入title沿既有2000-byte限制；累计正规化旧正文最多64KiB，双向fuzzy比较预算用归一化rune数量乘积的两倍累计100000，词片段/连续片段用集合线性扫描。源超正文预算、fuzzy预算不足、归一化空等情况保该来源所有原labels，不截断来源后默许独立；oversizedtitle路径先保守保来源，后saveItem拒绝。

**该上限是文本复用比较的工程上限，不是整条rename固定上限。** artifact_fields SQL读取、source blocks总遍历仍随历史数据量增长；labels合并沿既有hasRun线性查重，distinct labels累计可产生二次成本。root接受本批不泛改legacy标签/归因算法；本报告不声称任意大历史/标签集合为常数成本。consumer仍有自己的origin容量/深度拒绝门，不把该门冒充rename存储总成本限制。

copy检测是保守文本启发式，可能使独立新名因共同短片段、空块或预算fallback而保留来源；也不能证明跨来源语义改写的作者身份。不宣称自然语言语义相似、跨ID复用或任意重述可绝对识别。工程阈值不是实测效果阈值。

## 3 Undo 窄修与发现闭合

### 原因与授权范围

A定位的TenAction问题是前次action9审计带4字Title，下一action的一般CopyOrigins把该audit来源串回Title；undo10恢复beforeBlocks后saveAction再次从audit归因，改变相对action9.AfterBlocksHash。该归因问题与“重复一步”不同。本次仅允许已经fenced、当前origin已筛选、确实恢复的字段保存原精确块，保全新audit正常同步；没有全局禁audit-copy或弱化newer_action。

### 最终私有恢复 marker

1. 原checkActionSuccessors/newer_action、WorkStarted、当前document after hash与存在时的AfterBlocksHash检查全部保持原实现，并在实际restore循环之前完成。
2. restoreActionBlocksTx按当前合法DeskAction原Task/route/stale/undone及Run原依赖检查筛选块；失效段不能从before快照恢复，空title/name仍用既有通用占位。
3. 仅BeforeBlocks非nil且AfterBlocksHash非nil才在restore成功后loadItemBlocksTx，读实际持久已筛选blocks；局部saveCtx私有marker绑定真实item.ID与整个实际field map。这里不是未经筛选的c.BeforeBlocks复印本。
4. sync只有同item、当前field存在、canonical text==blockText(current blocks)、当前blocks规范JSON与marker.Fields[field]完全相等时才保原块，免二次EditBlocks/CopyOrigins。文字或Runs/DeskActions有任一差异便走普通同步。
5. saveCtx仅用于该次item.saveAction，不传播到其它item或后续command；新撤销history/source回执不在恢复map中或text不匹配，仍正常写入/归因。旧恢复history字段若逐字逐块一致也保其恢复身份，没有把所有audit来源从通用复制规则中排除。
6. 缺BeforeBlocks或缺AfterBlocksHash的legacy行仍沿原restore/普通同步，不获得精确恢复shortcut。既有后继/文档hash/原兼容处理没有改。

### D发现及修正因果

在 b80aab1，block after fence是原条件式 `if c.AfterBlocksHash != nil`，而新marker生成只要求BeforeBlocks非nil。正常新writer会同批写两项，但仅凭正常writer不能声称异常/旧行也“双hash均已校验”。D指出：有BeforeBlocks却缺AfterBlocksHash的行仍会获得新shortcut，与批准前提不符。

root接受，不通过弱化文档处理；A净补 cf84241只将load实际blocks/设置marker包在 `if c.AfterBlocksHash != nil` 内。D在净提交与root00328839逐字核对，旧restore、legacy普通sync和原拒绝/兼容路径保持不变。这个异常legacy边界由静态条件充分界定，本轮没有新增或冒称动态实测。

## 4 精确文件证据

A final cf84241 / root相同产品文件：

| 文件 | SHA256 |
|---|---|
| internal/postgres/artifact_blocks.go | 6fd6a7c03710a11393ff0eb159097cf059cee4b21088f0ca39233e96b4a65d7e |
| internal/postgres/actions_log.go | 156d5eef88568ad8d4ff4e0511a065f98685a73933fc4178ef8334a463df4535 |
| internal/postgres/commands.go | dae23ec26a2c38cf852f1b9641142b5eaad3c80bdd01846f728b371d1f4a907c |
| internal/workspace/ownership.go | f4b82e48cfbbf630d1c1ad1f76989eecea315a17639bf57d7d798ecd860fe900 |
| docs/tasks/phase2/contracts.md | 8942b611db357af90b83b585dfd08239f12c5cc74d10b8437ada447a6b82a2c2 |

审阅方法为git show/net diff、逐入口/restore调用链、准确字段查询顺序与hash读取；不把A的build-only结果当独立测试。最终C定点/新组合完整PG与CI workspace旧ownership门必须分别记录实际产品+harness/退出/计数。此静态报告没有代替那些门。
