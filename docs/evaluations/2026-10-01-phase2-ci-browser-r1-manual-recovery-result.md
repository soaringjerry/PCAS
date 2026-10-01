# 03e5eee 手动恢复单例因果补验：1PASS

## 精确批次

产品+harness组合 **03e5eee98854352c9f88d5c109d1ed6779e2fbc7**；新隔离工作区 `/tmp/pcas-phase2-manual-recovery-r3`，开测前clean。该头含embedding2235121；本例是独立owner Prompt，不以embedding变更解释此前请求形状失败。

独立形状修订commit **560d06423ad1c1ac948f9175307d1e3820dec2df**，manual spec SHA **37c3670823a7908ded8ca34846da9b9efb66156b7f1a864a3e61345453c8245f**。修订只有exact provider-only selection及provider==golden；sourceRunId、新RunID、同prompt、完整server recipient、全部拒供/包/剪贴板/提交断言保留。原80bcd46四例 **3P/1F**及未抵达步骤记录在独立 `2026-10-01-phase2-ci-browser-r1-causal-result.md`，不改其结果。

Node24.18.0同头build退出0。dist JS `index-KRxlz-XR.js` SHA `96395ae98a56ee26a35d3acca03286b1f4c9daaabb7729402b72d60112a134f6`；index.html SHA `ded8446837303f8d733f93ebcd4b6ad20a664b35b91028157c786f92f880ec1d`。与原80bcd46相同UI产物，Go服务从本轮同头重新构建。[preflight](2026-10-01-phase2-ci-browser-r1-manual-recovery-evidence/preflight.json)冻结spec、runner、四Go文件、契约及全部dist；[postflight](2026-10-01-phase2-ci-browser-r1-manual-recovery-evidence/postflight.json)再次核同SHA和clean。

首次只读list使用 `^real_revoke_clear_refuse_regenerate_submit$`，Playwright匹配包含file/suite的完整标题，退出1/零匹配/零执行；保留[原输出](2026-10-01-phase2-ci-browser-r1-manual-recovery-evidence/selection-list-original.log)。去掉首尾锚点后[实际list](2026-10-01-phase2-ci-browser-r1-manual-recovery-evidence/selection-list-corrected-original.log)退出0、精确1例1文件。这是执行前选择修正，没有启动浏览器/服务或重跑产品测试。

```text
PATH=/root/.nvm/versions/node/v24.18.0/bin:$PATH GOFLAGS=-buildvcs=false PCAS_GOLDEN_PORT=18116 PLAYWRIGHT_JSON_OUTPUT_NAME=test-results/phase2-manual-recovery.json bash web/tests/support/real-backend.sh npx playwright test tests/phase2_manual_backend.spec.ts --grep 'real_revoke_clear_refuse_regenerate_submit' --retries=0 --output=test-results/phase2-manual-recovery --reporter=list,json
```

原runner/custom-command未改。独立tmpfs Docker PG及随机loopback PG端口、新owner/token/配置目录、API18116/回调14559开测前空闲；结束清理己服务/container。依赖仅ignored目录链接共享UI node_modules，无旧checkout/部署配置读取。既有授权构建环境 `GOFLAGS=-buildvcs=false` 保持。

2026-10-01 **15:34:25.508558至15:34:37.718819 UTC**，runner12.210s；Playwright5.212719s，用例4.503s。**exit0；1PASS/0FAIL/0SKIP/0flaky/retry0，正式执行次数1；本选择无未抵达用例或步骤。** [完整报告gzip](2026-10-01-phase2-ci-browser-r1-manual-recovery-evidence/playwright-report-original.json.gz)、[原runner日志](2026-10-01-phase2-ci-browser-r1-manual-recovery-evidence/runner-original.log)、[实际退出/命令](2026-10-01-phase2-ci-browser-r1-manual-recovery-evidence/execution.json)。

## 实际恢复链

真实Chromium页面操作、实际serve/worker/PG和clipboard；API无拦截/fulfill。原断言全部抵达：

1. 明确选择golden，仅发送provider；普通新请求没有sourceRunId，服务端Task有完整manual目标绑定；合法旧preview含获准source atom，Brief不含该atom。
2. 正式撤权200。页面轮询stale，清预览，禁copy/paste；真实clipboard保持此前sentinel。APIRequestContext旧GET package及旧paste均精确 **409 `{error:"version_conflict"}`**；没有旧output/adoption/doc写入。
3. 实际点击向原接收者重新生成，POST200；exact `{provider:"golden"}`，sourceRunId等于旧RunID，prompt保持、新ID不同；旧stale保留，新Run waiting/not-stale，旧新可信Task recipient逐字段相等。
4. 新行preview及copy分别GET，新attempt ID不同；包与manifest均无撤权source/atom，独立owner文字保留；真实 `navigator.clipboard.readText()` 严格等于copy返回包；external_receipt仍unknown。
5. 实际贴回保存200，done/auto doc且仅一份文档，输出与doc正文一致，独立owner notes保留；fake实际assistant model events为0。

旧Run **60ddae6a-102d-490f-97f9-c5e9bbc6a4bb**；新Run **a354d0d3-ecd8-4561-8e62-7603d524278e**。两者recipient均principal/role/channel=manual、model/provider=golden、protocol=openai，route fingerprint均 `3d257e8ff11f2dc13fc1b683bb13b1815faa0f3f2015ce1c9a6e857e334d9d14`。

| 实际GET包 | Run | attempt UUID | 包字符数 | 外部回执 |
| --- | --- | --- | --- | --- |
| 撤权前合法preview | 旧Run | 31c7c5d5-9941-43b1-8387-3dd4d2ad4158 | 876 | unknown |
| 恢复preview | 新Run | ae0c02e8-245c-4861-95a8-1e1c623a83b6 | 448 | unknown |
| 恢复copy | 新Run | 7315dae2-b57b-4906-8aa8-50bc145ce9db | 448 | unknown |

[完整三包/manifest、原Run、refused状态、新Run及合成输入](2026-10-01-phase2-ci-browser-r1-manual-recovery-evidence/revoke-refuse-recover-original.json)；[被动实际浏览器HTTP请求/回应](2026-10-01-phase2-ci-browser-r1-manual-recovery-evidence/real-http-original.json)。HTTP附件不含token/header；page.response observer不覆盖APIRequestContext直发的两409，精确code/body来自原实际断言PASS。clipboard严格等值由真实浏览器原断言验证，未另附独立readText调用值。成功按既有retain-on-failure不产trace，没有为trace重跑。

## 证据边界

[原桌面1440×1000截图](2026-10-01-phase2-ci-browser-r1-manual-recovery-evidence/manual-recovered-browser-original.png)可见恢复结果已保存文档，独立owner文字及旧stale请求仍在；不是窄视口或真机验收。D只读view_image核此截图。root此前已只读审80bcd46的F7两条同标题已撤销回执、普通manual保存文档截图，该历史审阅不替代本次步骤。

本次没有运行G5/F7/普通manual或任何Go barrier测试；80bcd46的其余3P属于此前另一精确头，不能合计成03e5eee四例实测全绿。仅外部provider/OIDC/通知为既有合成fake，不能声称真实模型/真实外部接收者收到。完整同头CI仍是独立后续门。

[逐文件SHA/字节数/原路径及还原说明](2026-10-01-phase2-ci-browser-r1-manual-recovery-evidence/evidence-manifest.json)保留全部报告与服务日志。gzip逐份 `gzip -dc <file.gz>` 即还原原字节，校验manifest original_sha256；identity文件直接校验sha256。原文件另留 `/tmp/pcas-phase2-manual-recovery-r3-original` 与本worktree ignored test-results。
