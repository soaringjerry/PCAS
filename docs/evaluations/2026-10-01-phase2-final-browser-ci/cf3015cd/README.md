# cf3015cd 原浏览器CI：mocked全绿，三轮同一G1日历前提红

精确 head **cf3015cd67cf112933dfa51badbda8507de30834**；[run36888959192](https://github.com/soaringjerry/PCAS/actions/runs/36888959192)，attempt1，completed/**failure**。本批只含相对c8的两处provider-only mocked预期迁移（9+/3-）；产品/Go/其它测试未变。所有收证只读，未本地跑测、触发、取消或重跑。

四job完整checkout log分别核为 **ba946b860dea2ef81b83c2c1ec3eb5c63f4d1562**；原GitHub commit API tree **52b5a793517add9741df26a08ca64950aa05f034**等于cf源tree。不能把PR head误称实际merge checkout。spec/runner/workflow SHA见local-source-preflight.json；CI未上传dist字节，不虚构其SHA。

| 门 | 原job | 实际终态 |
| --- | --- | --- |
| mocked | 110459379926 | success；127P，phase2_manual26P；0skip/retry/flaky |
| real round1 | 110459379842 | failure/runner exit1；golden11P1F（仅G1），aux6P含manual2，timezone1P |
| real round2 | 110459379711 | failure/runner exit1；golden11P1F（仅G1），本轮无aux/timezone |
| real round3 | 110459379568 | failure/runner exit1；golden11P1F（仅G1），本轮无aux/timezone |
| stable aggregate | 110462416188 | failure；三轮非success，不另计测试leaf |

共 **170实际执行/167P/3F/0skip/0retry/0flaky**。三份golden原JSON各12结果、唯一失败G1；全部result.retry0，project.repeatEach1/retries0，global errors空。完整stats及逐例终态在[counts.json](counts.json)。mocked逐条原✓行127编号唯一，无✘或retry；success时该job原failure-only uploader不上传artifact，属既有配置。

## 原G1失败与未抵达

三轮均在初次首页 `hall-task` 可见断言（golden.spec.ts:31）失败；due、项目、-30分钟提醒及保存回执断言已过。之后 arranged截图、reload及其回执/首页断言、Undo和删除/消失断言均**未抵达**，不以其它轮或后续绿回填。

原round3真实DeskTurn POST200创建任务，following workspace GET200仍保存：due `2026-10-09T07:00:00Z`、trigger `2026-10-09T06:30:00Z`。浏览器Asia/Shanghai已跨到10月2日周五00:04；原 `nextWeekday(5,15)` 当日 `||7` 指下周五+7天。Hall的soon范围≤3天，因此首页排除这条真实存在的任务。三轮同类失败，未发现第二类红；具体HTTP/原截图/前后契约和未抵达清单见[G1原证据与补充gold](G1-calendar-gold-proposal.md)。这是合成日历样本前提不一致，不能删除可见断言、加超时或改产品Hall来解释。

已独立审批的后续G1明天样本净spec8cd7204→候选a51fdef属于**另一头**：仅G1可选arrange参数，保G3默认周五/nextWeekday及全部旧时间/项目/提醒/首页/刷新/撤销断言。白皮书原周五例和真实模型星期理解门未删除；固定fake回复从未证明weekday自然理解。本批3F原终态保持。

## 已实际抵达的真实链

三轮G5全部P：首真实副手请求失败、retry POST200携带原failed Run ID、manualRecipient缺席，完成原业务步骤和assistant events==2断言。关键实际body在selected-evidence/round{1,2,3}-G5-G5-real-retry-request.json.gz；events由已抵达原断言证明，成功例retain-on-failure无trace，不冒称另保存完整模型流量。

round1共享aux中manual两例均P，全部明确provider-only请求、sourceRunId、完整server Task recipient绑定、每次GET独立attempt/manifest、真实Chromium clipboard严格等值、unknown外部接收和真实贴回auto文档完成约束抵达。恢复例正式撤权旧GET/paste exact409 version_conflict、预览清除/禁copy/paste/sentinel、保owner原文、新Run同recipient、package无原source atom/manifestsource、复制/贴回均抵达；manual assistant events==0。原包/manifest、被动HTTP和桌面截图均逐字留存。APIRequestContext的直发409来自原精确断言，不冒称出现在page.response附件。

每个job setup/go及build后实际抵达；Go cache tar恢复warning不等于产品失败或有效缓存证明。真实PCAS serve/worker/独立合成PG和Chromium，外部模型/OIDC/通知仍fake。桌面截图不是真机，不宣称真实模型语言语义或外部收到。

## 字节保真归档

[artifact-index.json](artifact-index.json)含3个原ZIP的API digest、实算SHA、117个成员路径/字节/SHA；完整ZIP保留 `/tmp/pcas-phase2-final-browser-ci-cf-original`，G1 trace仍在原ZIP内。所有job完整原log、完整report JSON、service log和较大原附件采用gzip，无删正文。`gzip -dc <file.gz>`恢复原字节；[evidence-manifest.json](evidence-manifest.json)列压缩与恢复字节/SHA。关键附件的原report attachment名称/path同时保留于完整JSON report及ZIP member索引。c8红/环境未抵达与a51后续各自独立，未覆盖。
