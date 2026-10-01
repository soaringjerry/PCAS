# 2.1 ChatGPT 聊天记录导入：窄范围复核

2026-10-01；已部署基线 `d8d6fb3efe92f7711c5567440e92af362df7fe77`。
只读 `/root/PCAS-deploy/current/source`；以下路径相对此目录。本轮未运行测试、未访问个人聊天/账户/生产，未开发或修改repo/git/worktree。
结论：**现有ZIP/JSON导入不是从零开始；2.1应补齐可日常使用的导入与记忆贯通，而不是再写一个ChatGPT parser。**
只确认当前代码支持的mapping结构；没有读取当前官方导出样本，不声称全部现行官方导出兼容。

## 已有可直接复用的链路

1. UI：`web/src/components/ConnectorSettings.tsx:31` 已有“导入聊天记录”，提示ChatGPT/Claude/通用归档，accept ZIP/JSON/JSONL/TXT/MD、20MB。
2. API：`internal/httpapi/connectors.go:89–116` owner-only POST `/v1/connectors/archive`，multipart 20MiB文件上限，202返回。
3. `ImportArchive/ingestArchive`，`internal/postgres/connectors.go:322–348`：先确定性DecodeArchive校验，保存原始blob，排后台parse；返回“原始归档已保留，记录正在后台解析”。
4. `ProcessAttachment`，attachments.go:86–130，重新解析blob，以`archive-records` namespace写逐条source，关联原始archive版本；record处理仍走既有jobs。
5. 原始blob可通过现有授权`OpenAttachment/GetSource`展开；`archive_entries`与derived_from保留归档→消息来源关系。

## 格式、身份、时间与分支的代码事实

`internal/connectors/archive.go:133–186` 识别ChatGPT `mapping`消息树；文件名不必恰为conversations.json，结构匹配即可。
conversation ID取id/uuid（125–127），message ID取message.id或mapping key（171–174），source external ID为`chatgpt/<conversation>/<message>`。
记录conversation_id、parent（节点parent）、role（author.role）、表达create_time（186）；parseTime:287–298支持数值Unix秒和RFC3339Nano，不能解析的为unknown。
从current_node沿parent反向标active（138–145），其余所有message标historical（182–185）；保留历史分支，不只导入最后一条路径。
不是完整原始树字段投影：children/current_node原值、节点key与message.id差异、author.name、message metadata/content_type等只在原始归档保留。
缺current_node会全部historical；循环有steps与seen保护，但没有显式报告循环/断parent/当前节点不存在的结构缺口。
消息文本只读content.parts字符串；parts非字符串记“图片或其他非文本内容”缺口（164–180），仅非文本消息写占位。
部分非parts格式可能被跳过（空文本且无gap为continue），metadata里的attachments也未专门枚举；这必须进入格式兼容fixture。
ZIP：archive.go:26–74内存解析；最多20,000 entries，仅尝试json/jsonl/md/txt；其他文件跳过；不在文件系统解压路径。
ZIP中的原图/音频不自动导入附件/OCR/transcript；仅保留原ZIP和消息非文本gap。缺口不是“附件已解析”，也没有逐附件pending进度。
角色原字符串保留，user/assistant/system/tool有下游保护；没有来源真实性签名验证或角色白名单，上传内容仍是owner提供的资料。

## 幂等、更新档与不复活

原archive identity以整个上传字节SHA256（connectors.go:336–337）；相同字节同名可duplicate，换文件名可能因同ExternalVersion不同Title冲突，不能承诺改名同档无条件幂等。
消息namespace固定archive-records，ID使用原conversation/message，因此新ZIP/新文件字节不应自动创建一套全新消息ID。
`finishBatch`，archive.go:265–267，缺version时hash整个规范Record，包含role/branch/parent/time/gaps；内容/branch变化形成source新版本，历史保留。
`importBatchTx`，connectors.go:187–212，对duplicate复核source_context一致，不一致拒绝；新version写context（223），episode会话成员更新（259–319）。
删除重放已有基础：ingestTx检查source identity reimport_blocks，importBatchTx:179–181计blocked；ProcessAttachment:124–126对含blocked内容的新archive清理原blob。
`redactArchivesTx`，connectors.go:353–365，把blob清理/attachment_redacted与gap写入；删除子消息会清理含该消息的原档副本。
已有测试覆盖同ID重放与新archive含被删消息；不能外推到改ID、改写语义、不同导入namespace都绝不复活。
纠正沿既有claim版本/redirect/reimport block；更新档重导入“已纠正原文”、source覆盖与历史分支变化的ChatGPT专用验收尚缺。

## assistant归属、source授权与收费作业

processing.go:237 extractionInstructions已有role/branch限定，assistant提案不是用户决定，adjacent仅解指代不供quote。
431–440对assistant/system/tool或historical强制explicit=false、acquisition=inferred；非user标主体“AI或工具（非用户）”，task降unknown。
242–253自动adopt仅current capture；归档记录即使role=user也不自动当confirmed/adopted。476禁非user/historical触发条件signal。
但active表示导出树当前分支，**不表示今天的新指令**；未见独立historical-import/记忆迁入模式。processing.go:469–470仅按settings.AutoAccept、task、explicit、confidence≥0.98采纳，不检查archive来源/表达年份。
所以ChatGPT去年active分支user旧待办可被自动创建成当前事项（静态风险，未动态复现）；同类旧user线索在WakeIdeas下也可触发条件。历史分支/非user保护不足以覆盖“年代久远但仍active”。
默认应按迁入记忆处理，禁止归档旧文本自行创建事项、提醒或唤醒；用户在当下另行明确要求后再执行。列为2.1阻断验收，不泛化为全部导入已自动执行。
role为任意未知字符串、parent节点key≠message.id导致邻接误配等边界需验收；来源角色元数据由文件提供，不能当可信身份认证。
**消息raw source没有自动agent record_grants**（sources.go ingestTx；claims.go:183只默认授予新claim）；source可见性入口和2.1三消费者原文贯通依赖主记忆内核。
archive上传与解析本身是确定性处理；但解析写每条source后排source.chunk，jobs.go:112继续source.extract/source.embed/source.tokenize。
配置可用模型时worker会自动尝试抽取/embedding、并走预算；没有archive批次“仅存原文/稍后整理/预算预览”的独立开关。不能宣传导入全流程免费。
HTTP前置Decode与后台再次Decode都在内存；全部records单事务导入。20MiB上传、64MiB文本展开、10,000 records、单条1MiB是当前限制，不是大归档性能保证。

## 2.1最小新增范围（建议5项，不启动开发）

1. 明确第一批兼容格式：ChatGPT mapping ZIP/conversations.json；冻结合成新旧形态fixture，未知格式/角色/parts清楚报缺口，不无声跳过。
2. 补导入批次体验：原档保存/解析/索引/可检索/被阻止/失败状态、数量与缺口、重试；错误保留具体原因，超限给可执行下一步。
3. 接主记忆内核：选择工作室scope/原文授权，typed refs与role/branch/time/spans进入desk/model/manual实际输入；不可只让资料库出现记录。
4. 批量成本策略：确定性raw解析与模型整理分开，可暂停/继续与小批预算；大文件是否提高上限或分批是另一个受控决定，先测内存/事务/积压。
5. 对更新/重放/删除/纠错/attachments明确保证范围；保留原ids，原件缺失或未解析可见，assistant建议不能升级为用户决定。

## 最小验收与现有测试边界

同一合成ChatGPT ZIP与JSON：user/assistant/system/tool、active/historical、多分支parent、unknown时间、图片/音频、非parts内容；逐条id/time/role/branch/gap不丢。
同字节同名/改名、更新字节相同消息、新消息、current_node变更；分别断言archive与message去重/版本/历史，不重复收费job。
删source/删claim/纠正后同档与更新档重导；明确IncludeSources与授权范围，禁止目标不进入下一轮三入口/摘要/采纳/原blob。
配置fake计费provider：raw存储不发模型，后台整理受选择/预算约束；原文pending可按授权召回且不会因尚未抽claim被永久排除。
AutoAccept/WakeIdeas开启下导入去年active user“明天交材料”及旧条件命中：不新建当前事项/提醒、不唤醒；assistant/system/tool/historical同样不执行，当前用户明确转为任务才允许。
错误/超限/10,000 records/ZIP展开界限/取消/重试进度可见；没有个人资料、真实账户或生产请求参与这些fixture。
已有定位：archive_test.go TestChatBranchesRolesAndGaps、TestArchiveLimitsAndUnknownFiles；continuity_test.go TestConnectorReplayAndSummary、TestArchiveDeletionErasesOriginalCopies；extraction_lifecycle_test.go TestExtractionConfirmationRequiresCurrentVerbatimCapture；web/tests/continuity.spec.ts仅通用records上传202/消息提示。
这些现有测试本轮未运行；未发现覆盖真实ChatGPT ZIP到三入口的完整浏览器/最终provider输入验收。官方当前导出兼容声明须等实际脱敏样本与独立验收后再写。
