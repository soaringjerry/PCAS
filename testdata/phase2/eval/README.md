# 任务 V 合成回忆评测集

先于检索测量冻结预期。虚构用户南枝；所有人名、地点、细节均为合成。共 12 场景，每场景 8 份资料和 3 问，另有 24 份全局噪声：120 份资料、36 问。资料标题大量重复；含秘书式短句、速记、旧聊天（用户/AI）、两份长文件。

`expressed_days`、`recorded_days` 是相对基准日的本地自然日偏移。`{{date:N}}` / `{{year:N}}` 按基准日加 N 天替换；CI 用 `DateFromToday`，工具用 `-anchor`（默认当天）。绝不拿导入时间代替表达时间。`when` 是事件日的左闭右开区间；`gold.items` 使用第 2 批 R1 的 JSON 形状。AI 消息的金标准是空列表（不调用抽取）。

每个 evidence 指向资料及 gold.items 的下标；该项的标准文字作为记忆，或连续证据 quote 作为原话，至少一个必须完整抵达模型的记忆/原话区段。两者同时出现只计一次。多个 evidence 都是必需证据。distractors 指来源资料（或其记忆）的标准文字；不该先于标准证据。facts.any 列可接受字符串/别名，按逐事实 OR 匹配，事实之间 AND；匹配不判断推理与否定，存在局限。逐题 rationale 解释原话标注依据。

召回率 = 已送达标准证据项 / 全部标准证据项（微平均）。干扰率 = 先于证据的干扰-证据对 / 全部标注干扰-证据对；缺失证据视作无穷名次，已到达干扰算排在它前面。两者都未到达则不计先于。名次按实际模型输入中的首次位置，区段之外的用户问话、历史、说明不会命中。

基线为 0，等待协调者定线，不代表通过了检索质量验收。工具的假模型仅复述收到的上下文，验证流程，不证明真实模型的答案准确率。

| 场景 | 干扰 | 标注依据 |
|---|---|---|
| names-tea | same_name, others_plan | 两位同名者用职业区分；标准实体仍按契约合并，不人工消除难点。 |
| names-camera | same_name, others_plan, repeated_details | 同名职业、转述他人计划和复核后数量交叉。 |
| cancel-trip | cancelled, similar_place | 早先计划和AI复述都不能覆盖后来的取消决定。 |
| cancel-course | cancelled, repeated_details | 金额冲突与取消报名的时间先后。 |
| done-letter | completed, others_plan | 完成、未完成以及他人的同类动作。 |
| done-report | completed, repeated_details | 计划的页数不等于实际完成的页数。 |
| year-pottery | wrong_year, late_import | 事件日期和表达日期两年前，记录时间今天。 |
| year-orchard | wrong_year, late_import | 晚导入的旧话与最近记录并存，年份和编号都易错。 |
| place-museum | similar_place, others_plan | 镇和村仅一字之差，分别承载不同展览。 |
| place-cafe | similar_place, cancelled | 音近地名、取消和替代饮品并存。 |
| old-chat | late_import, wrong_year, completed | 导入中AI角色和用户角色混合，保留旧事件及完成时间。 |
| long-exhibition | repeated_details, similar_place, completed | 长文件中部和末尾、重复细节、同音地名，覆盖片段预算风险。 |

同名实体仍按冻结契约合并；评测保留职业上下文区分两个人，不绕过产品限制。事件标签、标准证据和答案不会注入回答模型。没有覆盖权限/删除/跨用户隔离、非中文、时区边界、模糊实体合并、200000 条导入吞吐或模型长上下文上限的精确 tokenizer；这些属于验收或后续评测。
