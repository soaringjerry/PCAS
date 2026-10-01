# a51fdef 同头浏览器CI：170P，三轮真实门与辅助门均通过

精确 head **a51fdef8670fb6152ac8ca290e69390f496edf33**；[run36890371142](https://github.com/soaringjerry/PCAS/actions/runs/36890371142)，attempt1，原API completed/**success**。D仅只读收取原GitHub job/log/artifact；没有本地跑测、触发、取消或重跑，也没有在该运行中改产品/预期。

相对cf只含已独立审定的G1近日期合成样本稳定化，golden.spec.ts 6+/5-，产品/Go/其它测试不变。mocked和三轮real各自完整原checkout日志均核为 **ac62139ee0dc99f11ee4ae31365b8bf091b3150d**。原GitHub merge commit API tree **e647f4fe63b12d1e0d81efad13bee87801e5ff12**等于a51源tree；实际PR merge checkout与API head分别记录。spec/runner/workflow精确SHA见[local-source-preflight.json](local-source-preflight.json)。CI原log证明该checkout实际build，未上传dist字节，不虚构dist SHA。

## 原终态与计数

| 门 | 原job ID | 原结论、实际抵达 |
| --- | --- | --- |
| mocked | 110464171915 | success/exit0；127P（其中phase2_manual26P） |
| real round1 | 110464172118 | success/runner exit0；golden12P、aux6P含manual2P、timezone1P |
| real round2 | 110464172489 | success/runner exit0；golden12P，本轮无aux/timezone |
| real round3 | 110464172216 | success/runner exit0；golden12P，本轮无aux/timezone |
| stable real aggregate | 110468347285 | success/exit0；原env ROUND_RESULT=success，全部三轮success |

共 **170实际执行/170P/0F/0skip/0retry/0flaky**；没有未抵达叶。三份golden各12、aux6、timezone1的完整原JSON逐条核对，仅单一result且retry0、project.repeatEach1/retries0、stats skipped/flaky0、global errors空。mocked从原带时间戳✓行独立匹配127个唯一编号、26个phase2叶；无✘/retry。计数及每例状态见[counts.json](counts.json)、[mocked逐条原终态](mocked-individual-terminals.json)。三轮是runner原矩阵三次独立新合成环境，不称随机重试。

四测试job setup/build全部成功且实际抵达测试。各Go cache tar恢复有一次warning，setup-go仍success；不把warning当失败、不把缓存状态当行为证据。round2安装步骤16:13:56→16:19:26 UTC后进入真实runner，golden原开始16:19:40.758，实际12P；与c8旧轮20分钟环境取消独立。

## G1全部业务断言及桌面原截图

三轮G1均实际通过：精确绝对due、A项目、-30分钟trigger、保存回执、初次首页可见、reload后同回执/可见、实际Undo、undone/task删除/首页消失。原安排截图发生在reload/Undo之前；这些后续断言由同一例原PASS证明，不以截图替代。D只读view round1原1440×1000 arranged图：近期待办明天截止、回执明天15:00/A项目/14:30提醒、右侧项目同任务可见。

- [G1 round1 arranged原图](selected-evidence/round1-G1-arranged.png)
- [G1 round2 arranged原图](selected-evidence/round2-G1-arranged.png)
- [G1 round3 arranged原图](selected-evidence/round3-G1-arranged.png)

本批仅G1明确“明天下午三点…”与localTime(1,15)对应；G3默认周五/nextWeekday、白皮书原周五例未删，全部原断言/timeout/retries/runner保持。合成固定fake due不证明自然星期语言理解；真实模型语义门不由本CI闭合。旧cf三G1失败及reload/Undo未抵达仍保原独立档案。

## 真实重试、撤销、手动交接及模型事件

三轮G5均通过。原附件保存真实failed Run和实际retry请求：sourceRunId严格等于该旧failed ID、manualRecipient键缺席，POST200，随后实际完成三步及assistant events==2原断言通过。三个旧ID分别 b2cb00dc-0372-47c0-bdae-3672b84de2b2、1e186e88-3e7c-441b-8f64-c113184c2834、df6bad6c-4da2-47b6-a6f8-e8434c029cdf；附件见selected-evidence/round{1,2,3}-G5-G5-real-retry-request.json.gz。F7连续撤销/顺序冲突/采纳撤销三例各轮均实际P，原断言未削弱。成功retain-on-failure不产trace；模型事件2来自真实fixture事件查询的原已抵达assertion，不声称另保完整外部模型流。

round1 aux沿正式已有backend/continuity/chatgpt-direct/model-api shared workspace之后，新manual normal/recovery两例实际P。明确接收者/固定golden provider、fresh请求无sourceRunId、初始/恢复请求provider-only、恢复sourceRunId原ID、完整server Task recipient逐字段绑定、独立GET/attempt/manifest、禁Brief复制、unknown外部回执、真实Chromium clipboard readText严格等于package、贴回完成auto文档/保owner原文全部抵达。

恢复例正式撤权200，旧GET及paste精确409 version_conflict、清preview/禁copy-paste/sentinel保留；新Run不同ID/同完整recipient、不含撤权source atom/manifestsource、独立preview/copy attempt、实际复制/贴回成功，assistant model事件0。原正常attempt bd57faf7-e61a-4ac0-bf97-af63cc532903→8249bd6f-7367-4735-a1ee-3b19ff2a7a24；恢复旧Run b5228963-eb80-4f9a-b11d-17d220e62536，三个包attempt 003715f9-9515-49e5-828d-487a1cb8e3e2 / 33de0912-1e35-4487-90a8-c9df0b52058d / be018de2-a231-4b8d-b863-37b05fe72603。原body/HTTP/package/manifest见以下逐字gzip附件与完整legacy report：

- [normal原包](selected-evidence/round1-manual-normal-two-real-packages.json.gz) / [normal被动真实HTTP](selected-evidence/round1-manual-normal-real-http.json.gz)
- [恢复原包/Run/manifest](selected-evidence/round1-manual-recovery-revoke-refuse-recover.json.gz) / [恢复被动真实HTTP](selected-evidence/round1-manual-recovery-real-http.json.gz)
- [manual recovered原桌面图](selected-evidence/round1-manual-recovery-manual-real-recovered.png) / [normal completed原桌面图](selected-evidence/round1-manual-normal-manual-real-completed.png)

APIRequestContext直发409不在page.response附件，证据来自已抵达原精确断言；clipboard严格等值来自实际Chromium权限/读取原断言，不声称外部对方已收到。所有截图均桌面浏览器，未作手机真机验收。

真实PCAS API/serve/worker/新合成Postgres/Chromium，外部模型/OIDC/通知仍golden fake。没有生产、真实模型账号或私人资料；mocked26仅前端截获API，不与real两例拼为真实模型链。

## 原字节还原与独立批次

[artifact-index.json](artifact-index.json)记录三原ZIP：round1 **11175759413**、round2 **11176986491**、round3 **11175913681**；GitHub digest与下载ZIP SHA逐一相等，117个原成员路径/字节/SHA完整列出。全部原ZIP保留 `/tmp/pcas-phase2-final-browser-ci-a51-original`。mocked success时既有failure-only uploader不产artifact，非遗漏。

全部测试job/aggregate完整log、所有实际report JSON、service log、关键原附件与截图已归档；大JSON gzip不删正文，`gzip -dc <file.gz>`恢复原字节。[evidence-manifest.json](evidence-manifest.json)同时列压缩和恢复SHA/字节；原report中attachment名称/path与artifact成员索引可定位。旧c8 mocked2F、round2 cancelled0抵达以及cf G1三F各自保留，最终同头绿不改其历史终态。
