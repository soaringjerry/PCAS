# G1 日历样本有因迁移：原失败与冻结提案

本文件只读分析并冻结gold，不改spec/产品、不跑本地测试、不重跑CI。原cf3015cd67cf112933dfa51badbda8507de30834，run36888959192/attempt1，round3 job110459379568：golden全部12例抵达，11P/1F/0skip/0retry/0flaky、测试退出1，仅G1 golden.spec.ts:31首次hall-task可见失败。

## 原实际证据

GitHub原artifact11175413741 ZIP实算SHA a9864d815e8ef96347f823f85ef313a6cf4be6daaa4d59e178c11bbda23cc485，匹配GitHub digest。原ZIP/trace在 `/tmp/pcas-phase2-final-browser-ci-cf-original/round3-artifact-original.zip` 及其展开目录。实际checkout ba946b860dea2ef81b83c2c1ec3eb5c63f4d1562（原git log记录）。

[完整原job日志gzip](selected-evidence/round3-job-original.log.gz)、[完整原golden JSON报告gzip](selected-evidence/round3-golden-original.json.gz)、[原error-context gzip](selected-evidence/round3-G1-error-context-original.md.gz)、[实际请求/响应安全抽取](selected-evidence/round3-G1-real-http-safe-extract.json)、[原1440×1000桌面截图](selected-evidence/round3-G1-browser-original.png)可复审，D已view。安全抽取保JSON正文，省略HTTP headers/token。

原真实POST DeskTurn 200，创建事项66f68427-8029-418d-8252-1d42fd39002f，due=2026-10-09T07:00:00Z（上海10月9日15:00），reminder nextAt=2026-10-09T06:30:00Z（14:30）；后续workspace GET200仍有原task。error-context/截图显示当前上海10月2日星期五00:04，回执清楚显示10月9日15:00/A项目/14:30提醒，项目面板也有该task。不是事项没存或更新迟到。

原到期、projectId、trigger offset/nextAt及receipt三项断言都实际通过；首次hall可见失败。后续arranged截图、reload、reload后receipt/hall可见、真实Undo、已撤销及task删除/页面消失断言未执行。后续因果通过不回填本轮未抵达项。

## 可重复的夹具前提矛盾

`real.ts:50-53` nextWeekday在当天用 `(day-today+7)%7 || 7` 强行取下一周。cf测试在UTC16:04跨上海午夜后执行，生成+7日周五，恰与原真实due一致。`hall.ts:39/98`既定soon仅当前日历未来3日内，TodayWall只渲染today/waiting/soon，因此+7日不显示符合既有设计。

`whitepaper.md:420`及`phase1/E-acceptance.md:18`原例字面是“周五下午三点…”；后者同时要求Today/soon出现。字面weekday合成样本在一周多数日距周五>3，不能保证该恒可见前提；只修当天||7也不能在周六/周日等保证近期窗口。不把此原例或星期语言理解门删除，也不更改产品Hall范围。

## 拟最小gold（待A独立业务核查与root审定）

仅G1合成样本明确为“明天下午三点和张三对方案，算在 A 项目里”，生成匹配 `localTime(1,15)` 的真实未来日历due。共享arrange可给G1传可选text/due参数；G3默认原周五text/nextWeekday(5,15)完全不变。原白皮书/验收文档周五例保留，迁移理由在本gold明示。

保原absolute due、projectId、offset=-30m、nextAt=14:30、receipt15:00/A项目/14:30、首次hall visible、reload后receipt/hall visible、真实Undo、undone/task删除/hall消失所有业务门。新增样本限定是把原近期任务出现/刷新/撤销门放在明确1日窗，不能只删visible或放宽timeout。

不全局改nextWeekday，不改Hall窗口，不freeze时钟影响真实分钟提醒，不加retry/skip，不造API/stub。星期语言理解由真实模型/原业务语义门另行负责；现有假模型控制直接返回固定due，仅证明实际Store时间转换和UI流程，不能冒称星期自然语言理解通过。

root/A未审定之前不写spec、不执行因果复验。当前其他round终态/原产物独立完整归档，不以本提案覆盖原红。
