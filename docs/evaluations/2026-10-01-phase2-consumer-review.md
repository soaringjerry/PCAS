# Phase 2.0 消费者批次：审阅入口

产品草稿 [#44](https://github.com/soaringjerry/PCAS/pull/44) 基于来源批次 #43。当前已推送候选 **`a51fdef8670fb6152ac8ca290e69390f496edf33`**；相比 `cf3015c` 只修 G1 合成样本日期（`golden.spec.ts` 6+/5-），产品代码不变。同头完整 CI 正在启动。本文及原始证据另存仅文档草稿 [#45](https://github.com/soaringjerry/PCAS/pull/45)，不向产品分支追加报告提交。

root 只负责协调、研究判断、审阅与集成；实现及测试由分工明确的 Sol 6.1/high 执行者完成，预期由独立验收者制定。原失败、未抵达项和有因修订分别保留。

## 当前结果

| 范围 | 结论与边界 |
| --- | --- |
| `a51fdef` 当前 CI | 同头完整检查正在启动；新头只迁移 G1 日期样本，不修改产品或断言。[原三轮 G1 原因、业务门与修订依据](2026-10-01-phase2-final-browser-ci/cf3015cd/G1-calendar-gold-proposal.md)。 |
| `cf3015c` 完整 CI | web、完整 memory、127 项 mock 通过。memory 755 真叶 752P/3 既有 opt-in，11 测试包均实际执行、无缓存。三轮真实后端各 12 项抵达、11P/1F，唯一 G1 将七天后任务恒要求在三天窗口首页可见；第 1 轮附时区 1P/辅助 6P。原红与未抵达 reload/undo 保留，不能称完整门通过。 |
| `c8adbae` 完整后端 CI | 一次 `make check` 成功。341 顶层：338 通过、3 既有 opt-in 未启用；755 实际叶：752 通过、3 未启用。11 个测试包全部成功、无 `(cached)`，另 4 个包无测试。PG 两个真实模型用例及 AI InstalledCodexHandshake 未启用；原 23 因果叶同头全 P，最终 build 成功。[完整日志、计数和 checkout](2026-10-01-phase2-final-memory-ci/README.md)。 |
| `c8adbae` web CI | npm ci、lint、typecheck、build 全通过；GitHub PR 检查合并树等于候选树，不是 main 合并。[原日志](2026-10-01-phase2-final-web-ci/README.md)。 |
| `c8adbae` 浏览器 CI | mock 127 项全部抵达：125P/2F。两项错误要求客户端发送完整接收者，正确请求为原 provider 和 SourceRunID。真实后端第 1、3 轮各 12P，第 1 轮附时区 1P/辅助 6P；第 2 轮安装后自然 cancelled，未开始测试，原终止原因正在归档。总门没有通过。[原失败与修订依据](2026-10-01-phase2-final-browser-ci/mocked-original-red-brief.md)。 |
| 后端因果 R1/R2 | `834949f` 一次 race 23 叶 17P/6F；六个夹具或额外预期错误经独立复核、追加 gold，`c8adbae` 只补原六项，一次 6/6 通过。未改产品、未重跑其余 17 项。[原失败](2026-10-01-phase2-retry-causal-r1/README.md)、[六项补验](2026-10-01-phase2-retry-causal-r2/README.md)。 |
| 浏览器因果 | `80bcd46` 一次 G5、F7、普通 manual 通过；恢复例混比请求与响应对象失败。保留原红并按 provider-only 契约修正；`03e5eee` 单恢复例一次通过，409、清预览、新包、真实剪贴板与贴回全部抵达。[四例原结果](2026-10-01-phase2-ci-browser-r1-causal-result.md)、[恢复补验](2026-10-01-phase2-ci-browser-r1-manual-recovery-result.md)。 |
| SIWC 夹具补验 | 全新数据库一次跨包 race，3 顶层、5 叶全通过；各夹具用同一 advisory lock 自足初始化 vector 扩展。[原失败及补验](2026-10-01-phase2-siwc-ci-fixture/README.md)。 |
| 独立静态审阅 | owner 改名、双指纹撤销保护及 embedding 外发隔离已审阅。外发只用本轮 owner 独立输入，生成 Prompt、事项及旧结果保留本地检索。静态不替代动态。[改名/撤销](2026-10-01-phase2-owner-rename-review.md)、[外发边界](2026-10-01-phase2-ci-browser-r1-embedding-review.md)。 |

## 本批实现及边界

- 三入口供给获授权原文，共用原件、结构化记忆和向量的身份、版本与证据；工作室仍是范围。
- 持久 typed 依赖及当前权限、路由、范围复核覆盖生成途中、下一轮、历史与派生读取。区分候选、实际输入、回答 Used 与间接依赖；诊断到期不删除存活产物来源。
- 重试及手动预填改字携旧 Run 定位符；服务端重读原 Prompt 和实际 delegate 来源，分别复核原生成者及当前接收者。owner 独立 Prompt 不继承无关的一般上下文失效。
- 手动每次预览/复制有新交付记录；撤权拒绝旧包和贴回，重新生成仍绑定明确接收者。DeliveredAt 只证明 PCAS 交付，外部接收仍为 unknown。
- 窄的 owner 已撤销回执规则只保审计身份，不授权模型历史。来源仍合法的历史和来源失效后的泛化回执分开校验；Undoable 能力与 Undone 状态不同。
- 独立改名与严格逆序撤销保持受限恢复条件；复制、标点改写和原子片段不能借改名清来源。
- 输入估算上限 8000、正文 7 天、元数据 30 天及容量限制是工程初值，不是 tokenizer、真实费用或连续任务效果保证；详见共享契约与 #44。

## 历史证据不能改称当前通过

`0032883` 的[完整后端 R5](2026-10-01-phase2-backend-r5/README.md)一次 581 实际叶 579P/2 既有 live 未启用；同头原 CI 三轮浏览器各 10P/2F，SIWC 三叶在 vector 前置失败。后续修复及新 CI 分开记录。

R2/R3/R4 曾重复计入父项：[勘误](2026-10-01-phase2-backend-counting-erratum/README.md)和[另一验收者复核 17 份原日志](2026-10-01-phase2-test-count-audit.md)只修计数，原日志、失败和退出保留。被补修取代的 `e6fd9b1` memory/browser CI 由协调者明确取消，不能算通过。不同版本局部绿色不能拼成同头完整结果。

## 尚未完成的门

- `a51fdef` 最终同头完整 CI 尚未结束；`c8adbae` mock 和 `cf3015c` G1 原红保留。真实模型/账号 opt-in 未启用。
- 浏览器使用真实应用、worker、PostgreSQL 与 Chromium，外部模型是假服务；桌面及窄视口截图不是手机实测。
- 线上 11 项、真实手机、一天用户试用尚无完成证据。SQL 恢复检查点不替代真实进程崩溃/重启。
- 2.1 时间线回忆、抽取遗漏补查、已有 ChatGPT ZIP/JSON 导入闭环待下一批；完整私人历史未迁入。
- 2.2 同模型、同预算、简单混合检索基线的连续收益及导入/维护/调用/存储成本尚待实测。
- 未合并、未部署；旧 checkout、生产配置与部署快照未动。
