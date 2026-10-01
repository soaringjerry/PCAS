# Phase 2 独立手动交接 UI 验收结果

首次正式执行：26 passed，0 failed，0 skipped，0 retries，退出码0，Playwright汇总耗时1.0m。未通过重跑取绿。没有发现本矩阵覆盖范围内的产品缺陷。

## 版本与执行

- 冻结预期表 SHA：a66d57967a932e50c1d404f2a2a4eca45dc401d2。
- Harness SHA：d7be03ccae365b1956c605a7d69424da6b5f7933，`web/tests/phase2_manual.spec.ts`；独立工作区 `/tmp/pcas-phase2-ui-acceptance`。
- 实际测试源码组合 HEAD：acfc1f750eca4faf1e8efc8c86b6021fa7639939，含产品 UI1 8221d2c4d4cc5525a4a3c008dc4e9d52764037a9；`web/src` 与协调者组合产品9f58c892188d8593036ff357a90fe34af3283a5a无diff。后端基线a5046d53，不在此次浏览器测试中调用。
- HTTP静态预览：`http://127.0.0.1:18110`，只serve作者独立工作区构建产物；产物JS `index-BBeP_cpR.js`，SHA256 `f7245cf9cd1210e438b43e422ad876b15c608c83cc08e30c528ad6e2ebc8280c`。本地dist和实际HTTP取回JS hash相同。
- 浏览器harness运行Node v20.19.5，Playwright1.63.0，默认headless Chromium。产品构建由协调者/实现者使用Node24完成；本次不把harness运行算成Node22+产品构建验收。
- 从隔离工作区web目录运行：

```sh
PCAS_TEST_BASE_URL=http://127.0.0.1:18110 node node_modules/@playwright/test/cli.js test tests/phase2_manual.spec.ts --retries=0 --output=/tmp/pcas-phase2-ui-evidence/run1
```

原日志：[2026-10-01-phase2-ui-run1-original.log](2026-10-01-phase2-ui-run1-original.log)。首次test发现列表为26 cases；新增harness ESLint退出0。

## 通过的行为

| 分组 | 场景数 | 结果 |
| --- | ---: | --- |
| 文字接收者交集、友好名、必须选择、最小新请求body | 1 | 通过 |
| 每次fresh package GET/no-store、Brief隔离、unknown外部收到表述 | 1 | 通过 |
| revision/stale/route/status/authorization变化：已有预览清除和晚preview/copy抑制 | 15 | 通过 |
| 错run、未交付、错误manifest provider/route拒绝 | 4 | 通过 |
| package失败原目标重新生成、paste失败清预览、failed run原目标重试 | 3 | 通过 |
| 换目标新run、保持原prompt/旧run/owner原文 | 1 | 通过 |
| 1280px桌面、390px浏览器视口、选择表单无横向溢出 | 1 | 通过 |

所有场景同时断言没有浏览器pageerror、没有非预期API路径；合成事项notes和独立owner原文保留。原目标重试保留完整ManualRecipient；新目标body只有provider，没有fingerprint/scope。

## 截图

- [桌面1280px](2026-10-01-phase2-ui-evidence/phase2-manual-desktop.png)
- [390px交接预览](2026-10-01-phase2-ui-evidence/phase2-manual-390px.png)
- [390px选择新接收者](2026-10-01-phase2-ui-evidence/phase2-manual-selection-390px.png)

图片已独立查看，呈现转交卡、自然请求与接收者友好名；390px没有水平溢出。它们是浏览器窄视口截图，不是真机验证。应用内部滚动容器不由Playwright fullPage展开，截图展示该滚动位置的可见内容。

## 证据范围

全部内容、请求、账号/路由标识为合成。所有`/v1/**`在浏览器中截获，真实服务仅提供静态资产；clipboard使用测试stub，证明向clipboard API提交正确fresh文本及不提交晚包，不证明系统剪贴板权限或操作系统集成。竞态通过真实UI轮询接收合成新state；authorization场景为workspace中接收者enabled撤销和revision更新，不覆盖真实登录失效。

本报告只证明前端行为。已有独立后端manual矩阵不与本报告拼成真实端到端证据；现有真实后端浏览器套件缺手动交接路径，详见[只读盘点](2026-10-01-phase2-ui-legacy-inventory.md)。CI仅把新spec追加到browser-mocked原7文件清单，旧测试、三轮real-backend和所有gate保持原样。

## 有因补验：390px按钮可达性

协调者查看首次selection截图，发现主按钮靠底边，要求保留原26PASS并新增滚动/遮挡/实际click断言。产品仍为8221d2c，静态资产未变；未重跑26例矩阵。

- 增强harness SHA `1c1da74f5333d564242112aa133112b6d85aacc1`：保留原末用例全部断言，追加scrollIntoViewIfNeeded、完整bbox处于390×1000视口、hit-test不被固定栏遮挡、普通click后新run命令和原prompt/target保留。
- 此增强用例第一次退出1（1 failed，0 passed，0 skipped/retries），原日志[保留](2026-10-01-phase2-ui-button-risk-original.log)，[原trace](2026-10-01-phase2-ui-evidence/button-risk-initial-trace.zip)。完整bbox断言通过；hit-test失败。只读trace/CSS检查发现测试取点为bbox下两角内缩2px，产品按钮为`border-radius:var(--r-pill)`，这些点落在胶囊绘制区域外，因此父元素命中是测试几何错误。未把此失败当产品遮挡证据。
- 修正harness SHA `f023585c1a11989f119b4f2dfc1c5aaf72777760`：取按钮中心及四边中点内缩2px，共5个实际绘制内部点；所有bbox/overflow/原请求保护/普通click断言保留。协调者审阅并认可修正依据。
- 仅末用例执行一次有因补验：1 passed，0 failed，0 skipped，0 retries，退出0，2.1s。普通click产生一个新requestRun（new id、other provider、draft、原prompt），旧run完整recipient和原owner文案保留。[补验原日志](2026-10-01-phase2-ui-button-risk-geometry-fixed-original.log)。
- [390px可操作按钮截图](2026-10-01-phase2-ui-evidence/phase2-manual-selection-390px-operable.png)。此截图在滚动后、普通click前记录当前视口；bbox与5点hit-test已证明该按钮完整可见且没有固定秘书栏拦截。它仍是浏览器窄视口截图，不是真机。

运行命令与首次相同，但增加`--grep 'desktop and 390px browser views'`，分别使用独立output目录button-risk和button-risk-geometry-fixed。首次26例证据与补验、harness错误日志均保留，没有skip、force click、削弱既有业务断言或无因重跑。
