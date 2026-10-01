# c8adbaeb 最终浏览器CI原档案：mocked红、两轮真实绿、一轮环境超时

精确head **c8adbaeb5c02daa9e83f90f74c216d8cf9ee22bf**，GitHub [run36887421678](https://github.com/soaringjerry/PCAS/actions/runs/36887421678)，attempt1，整体completed/**failure**。只读GH API/log/artifacts；未执行本地测试、触发/取消/重跑CI、修改此运行中的产品或断言。

mocked及real1/2/3各自完整原checkout log均为 **de9906eeece777e5016d50b0d85a1d0b72a68b40**。GitHub commit API原响应确认merge parents=6fd9d173+c8adbaeb、tree **e615cd44aa99df110a03592d965afafb06a21c8b**，等于c8源tree。各job真正测试PR merge checkout，不把API head误称checkout。源码spec/runner/workflow SHA见local-source-preflight.json；CI dist字节未上传，不虚构其SHA。同checkout实际build/安装结果见原log。

## 实际终态

| 门 | 原jobID | 实际终态与抵达 |
| --- | --- | --- |
| mocked | 110454170387 | failure/exit1；127全部抵达，125P/2F/0skip/0retry；phase2_manual26叶24P2F，其它101P |
| real round1 | 110454170526 | success/runner退出0；golden12P，timezone1P，aux6P含manual2P；0skip/retry/flaky |
| real round2 | 110454170502 | **cancelled**；环境步骤超时，真实runner skipped，产品/Playwright **0抵达**，计划golden12未执行，无产物 |
| real round3 | 110454170792 | success/runner退出0；golden12P，0skip/retry/flaky；无本轮aux/timezone |
| stable real aggregate gate | 110463099053 | failure，未全轮success；这是job门失败，不是新增测试leaf |

实际测试合计 **158执行/156P/2F/0PlaywrightSkip，另12计划golden未抵达**。所有已执行结果仅retry0；没有把未启动轮次算12Skip或Fail。逐条mock terminal由原带时间戳✓/✘行重算，127编号唯一；real完整JSON stats/results/config independently核对各12/1/6结果、repeatEach1/retries0/flaky0/errors空。[counts.json](counts.json)、[mocked逐条](mocked-individual-terminals.json)、[原jobs终态](jobs-final.json)、selected-evidence完整report gzip可复审。

## 两类原红分别保留

mocked唯一两红是旧原目标请求形状：原spec178/200要求完整recipient，实际UI请求exact `{provider:"writer"}`，sourceRunId均为原合成RunID；两个原mock响应200。原trace只证明前端，真实同target可信Task绑定有此前单恢复独立证据。原赤裸形状比较是harness前提错误；原FAILED终态与新ID/clipboard/clean未抵达断言不回填。见mocked-original-red-brief.md、mocked-two-actual-requests.json及完整原job log.gz。已授权净spec c2b847b/root cf3015cd属于下一头，不改变此原125P/2F。

round2原GitHub conclusion明确 **cancelled**，started15:50:46、completed16:11:03 UTC。原check annotations明确“maximum execution time of 20m0s”，没有本团队取消。step5 `npm ci && npm run build && npx playwright install --with-deps chromium` cancelled，step6 runner skipped；always upload因没有test-results而failure，原API无round2 artifact。完整安装原log及原annotations保留，不能把cancelled改写成测试FAIL。原log显示 npm ci 已完成（4s），Vite build 已完成（2.51s）；之后 Playwright `install --with-deps` 启动 apt 索引刷新，最后输出含 Azure Ubuntu mirror 的 Ign 及其它镜像的 Hit/Get，直到20分钟上限取消。Playwright 安装未完成；更细网络/镜像等待原因未观测。

四job的Go cache tar恢复都有warning，setup-go本身success；mocked和real1/3随后build并抵达测试。不能把这些共同warning当作round2超时因果或产品缺陷，缓存成功也不代替实际行为门。

## 真实链与模型事件边界

两实际golden轮G5原failed Run均由实际副手首请求失败，真实重试POST200携带其原failed ID、无manualRecipient，完成原三步与model assistant events==2断言。实际请求附件分别在selected-evidence/round1-G5-G5-real-retry-request.json及round3同名；events计数由原已抵达真实断言PASS证明，成功retain-on-failure不另产trace/完整events抓包，不声称保存了所有model流量。

round1的manual normal/recovery在原aux shared workspace、backend/chatgpt-direct/continuity/model-api之后实际均P。两新例所有provider-only/自然prompt/sourceRunId/完整server绑定、每次GET package、unknown外部回执、真实clipboard readText等值、保存auto doc/owner原文约束抵达。恢复例正式撤权后清preview、禁copy/paste、sentinel保持，旧GET及paste exact409 version_conflict；新Run和同完整Task recipient、独立preview/copy attempt、撤权atom/manifest source均无、真实贴回完成，assistant model事件0。完整原package/manifest/被动HTTP和原截图在selected-evidence；APIRequestContext直发409不在page.response附件，来自已抵达原精确断言。clipboard严格等值为原真实Chromium断言，不冒称外部已收到。

仅真实PCAS API/serve/worker/新合成PG/Chromium；外部模型/OIDC/通知仍golden fake，无生产/真实账号/模型，没有真实模型星期语言理解验收。截图是桌面浏览器；mock390px是窄视口，不是真机。

## 原证据还原

[artifact-index.json](artifact-index.json)包含3个原ZIP的GitHub ID/digest与实算SHA、完整97个成员文件SHA/字节、原runtimeZIP路径；ZIP完整保留 `/tmp/pcas-phase2-final-browser-ci-original`。mock两trace包含在原ZIP，原失败上下文逐字gzip留存。原全job日志/全部实际report/服务log与关键附件已归档，manifest逐文件SHA及gzip原字节SHA；`gzip -dc file.gz`还原原字节。新cf/a51档案分别独立子目录，不用后续绿覆盖本批红/0抵达/两轮绿。

### 大附件无损压缩补记

原6f7a5d1提交中的较大manual HTTP JSON现仅转换为gzip原字节归档；原净提交仍保历史可审。正文/计数/预期未变。对应包/clipboard附件来源仍可在原完整report及artifact member索引定位：

- [round1-manual-normal-real-http.json](selected-evidence/round1-manual-normal-real-http.json.gz)；`gzip -dc`恢复原JSON。
- [round1-manual-recovery-real-http.json](selected-evidence/round1-manual-recovery-real-http.json.gz)；`gzip -dc`恢复原JSON。
- [round1-manual-recovery-revoke-refuse-recover.json](selected-evidence/round1-manual-recovery-revoke-refuse-recover.json.gz)；`gzip -dc`恢复原JSON。
