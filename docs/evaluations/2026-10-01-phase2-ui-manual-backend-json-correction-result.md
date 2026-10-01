# 真实 manual：JSON 修复后两例闭环通过

## 精确版本与一次性环境

产品与组合 HEAD `dc2705d0793f1e8986f1476ea63a3770d5d734ef`，包含审定源facade+Task显式lowerCamel序列化+Name/Prompt修复。harness保34acb4f selector修正及原业务断言，spec SHA256 `84e75a62eeb73db8d13f5a9df0f063349ad04446ab17bf1c53169f43b5a78606`。新隔离树 `/tmp/pcas-phase2-manual-browser-json-correction` 起始tracked clean。旧501、selector timeout、JSON crash三批真实失败和最初VCS infra0执行报告全部保留，没有改写为通过。

同HEAD独立build exit0，Node24.18.0/Go1.26.8/Playwright1.63.0/Chromium153 Linux，桌面1440×1000/AsiaShanghai。dist html SHA256 `2b1d555c07e3cc54bf1fc0359b3ef33409df49c786500c44e03e95c21f85a609`；JS `f7245cf9cd1210e438b43e422ad876b15c608c83cc08e30c528ad6e2ebc8280c`。己新tmpfs DockerPG/random loopback端口、18112API、14559callback/new合成owner/token/config，现有golden外部服务fakes。PCAS/worker/API/数据库真实；浏览器API/clipboard没有stub。runner退出清己容器与进程，借用node_modules symlink收尾已移除。

root授权产品修复后的有因单次补验，现有runner custom入口只2新例/retries0；GOFLAGS环境约束沿root批准，不改runner：

```sh
PATH=/root/.nvm/versions/node/v24.18.0/bin:$PATH GOFLAGS=-buildvcs=false PCAS_GOLDEN_PORT=18112 PLAYWRIGHT_JSON_OUTPUT_NAME=test-results/phase2-manual-backend.json bash web/tests/support/real-backend.sh npx playwright test tests/phase2_manual_backend.spec.ts --retries=0 --output=test-results/phase2-manual-backend --reporter=list,json
```

## 原结果

**exit0，2 PASS，0 FAIL / skip / flaky / retry；2例均完成。** JSON startTime `2026-10-01T14:39:34.719Z`，duration7177.795ms（list7.2s）。目标/clipboard/提交1967ms，撤权/拒绝/恢复4522ms。原26mocked与旧三轮没有重跑，本门是新增真实后端补验。

| 叶 | 实际抵达的不可放宽预期 |
|---|---|
| real_target_package_clipboard_submit | 正式source201/auth200；真实UI catalog/golden友好名选择；requestRun200仅provider、server lowercaseTask完整manualroute/waiting/prompt；preview/copy两真实GET，各新UUID delivered_at+unknownreceipt；exact source marker/Input在包而Run.Brief无marker；UI preview等真包、真实Chromium readText等第二真包；实际paste200、持久done/output、自动doc采纳/正文正确、独立owner保留、无golden assistant模型调用 |
| real_revoke_clear_refuse_regenerate_submit | 原waiting run真包含exactsource正控；正式DELETE授权200；真实poll stale/清preview、copy与submit disabled，Chromium sentinel不被覆写；旧包GET与最新workspace revision的旧paste均严格409/version_conflict；旧run无output/adopt/doc；UI向原target/prompt重新生成新ID，旧stalerun保留；新preview/copy各新包UUID，本例已撤source marker及Input删除，owner仍在；实际安全clipboard读回/新paste200、done+自动doc/owner文字保留 |

五份合法GET包的run/attempt及Input数量见package-index.json，UUID五个不同；各原包与完整manifest来自真实GET响应。第二例与第一例复用runner己workspace，其他仍合法授权的合成来源可继续出现；负控精确针对第二例已撤source，不把无关来源误称已撤。

## 证据与边界

目录 `2026-10-01-phase2-ui-manual-backend-json-correction-evidence/` 保存原build/runner/JSON/services，原report附件解码的两个case packages/manifest/state/policy、被动browserHTTP记录、两张真实完成桌面截图。截图是Chromium1440px桌面，不是真机。原成功配置trace retain-on-failure，因此这次成功没有trace，未为补trace重复运行。

Clipboard证据来自两例中真实navigator.clipboard.write/read操作与等包断言全部通过；记录的copied包是比较对象，报告不冒称外部应用实际粘贴或接收。本次直接page.request的旧包/旧paste严格409断言通过，APIRequestContext不发page.response事件，故passive real-http附件不重复记录这两请求；最终refused持久state也保存。报告不将未附单独HTTP正文误称有独立trace。

golden是外部模型替身，合成贴回答案由测试输入，只证明PCAS真实保存/采纳，不证明第三方收到/生成。source.embed无配置的WARN在原worker日志保留，与本闭环source span/token可用和业务断言通过同时存在；不是embedding性能验收。真实网络晚包竞争、390px、完整目标过滤仍属此前mocked门边界；未将分层证据拼成真实外部模型全链路。

现有CI runner仍未在本报告提交修改；按root已授权，下一独立提交仅追加此新spec到原auxiliary清单，最终组合CI门另执行。原三轮gate/旧spec/产品不改。
