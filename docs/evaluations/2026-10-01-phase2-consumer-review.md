# Phase 2.0 消费者批次：最终审阅入口

产品草稿 [#44](https://github.com/soaringjerry/PCAS/pull/44) 固定 **`a51fdef8670fb6152ac8ca290e69390f496edf33`**，基于来源批次 #43。同头 web、完整 memory、mocked、三轮真实后端浏览器及汇总门全部通过。本文及原始证据另存纯文档草稿 [#45](https://github.com/soaringjerry/PCAS/pull/45)，不向产品分支追加报告提交。未合并、未部署。

root 只协调、研究判断、审阅和集成；实现及测试由明确分工的 Sol 6.1/high 执行者完成，gold 和测试由独立验收者制定。没有调用 Opus/Astra。原失败、未抵达项和有因修订分别保留。

## 当前同头技术结果

| 范围 | 实际结果与原证据 |
| --- | --- |
| 完整后端 | `make check` 的 fmt/vet/race/build 全通过。341 顶层：338P、3 既有 opt-in 未启用；755 真叶：752P、3 未启用。11 测试包全实际执行，无测试缓存命中，零失败/未抵达/race/panic。原 23 因果叶同头全 P。最后 build 命令成功，不声称编译缓存未使用。[完整证据](2026-10-01-phase2-final-memory-ci/a51fdef/README.md)。 |
| 浏览器 | mock 127P；三轮 golden 各 12P；第 1 轮另有 aux 6P（含 manual 两例）和 timezone 1P。合计 170 次执行全部通过，0 skip/retry/flaky/未抵达；汇总门成功。三轮是原矩阵的独立合成环境，不称 170 个不重复场景。[完整证据](2026-10-01-phase2-final-browser-ci/a51fdef/README.md)。 |
| web | npm ci、lint、type-check、build 全通过；npm 下载缓存不等于跳过执行。[完整日志](2026-10-01-phase2-final-web-ci/README.md)。 |
| 精确版本 | CI 实际 checkout GitHub PR 检查合成提交 `ac62139ee0dc99f11ee4ae31365b8bf091b3150d`；其 tree 与最终候选完全相同：`e647f4fe63b12d1e0d81efad13bee87801e5ff12`。这不是 main 合并。各报告分别保存 head、checkout、tree 和 SHA。 |

PG 两个真实模型 opt-in 和 AI InstalledCodexHandshake 未启用，共三项既有跳过，没有新增 skip。真实后端浏览器使用实际 PCAS serve/worker/PostgreSQL/Chromium，外部模型、OIDC、通知是假服务；不证明真实模型效果或外部收件。root 核看本轮 G1 日程及 manual recovered 原 1440×1000 桌面截图，不算手机实测。

## 本批实现及边界

- 秘书、副手、手动交接供给获授权原文，共用原件、结构化记忆和向量的身份、版本与证据；工作室仍是范围。
- 持久 typed 依赖及当前权限、路由、范围复核覆盖生成途中、下一轮、历史和派生读取。分别记录检索候选、实际输入、回答 Used 与间接依赖；诊断到期不删除存活产物来源。
- 重试与手动预填改字携旧 Run 定位符；服务器重读原 Prompt 和实际 delegate 来源，分别校验原生成者与当前接收者。owner 独立 Prompt 不继承无关的一般上下文失效。
- 手动每次预览/复制重新交付并记录新 attempt；撤权拒绝旧包及贴回，新请求仍绑定明确接收者。DeliveredAt 只表示 PCAS 交付，外部是否收到仍为 unknown。
- 已撤销回执的窄 owner 审计规则不授权模型历史；独立改名和严格逆序撤销保持受限恢复条件，复制/标点/原子片段不能借改名清来源。
- 本地完整检索与 embedding 外发输入分开；只外发本轮 owner 独立输入，生成 Prompt、事项和旧结果留在本地检索。整理后台的进一步用途与接收者契约列入 2.1。
- 8000 的最终输入估算上限、正文 7 天、元数据 30 天及各容量限制是已采用的工程初值，不是 tokenizer、真实费用或连续任务收益保证；详见共享契约与 #44。

## 原失败与修订链

| 原版本 | 保留的原结论及后续依据 |
| --- | --- |
| `0032883` | [完整后端 R5](2026-10-01-phase2-backend-r5/README.md)581 叶 579P/2 旧 live；原 CI 三轮浏览器各 10P/2F，SIWC 三叶在 vector 前置失败。后来修复与此原红分开。 |
| 后端因果 R1/R2 | `834949f` 一次 race 23 叶 17P/6F；六项夹具或额外预期错误经独立复核、追加 gold，`c8adbae` 仅补原六项一次 6/6P。未改产品迎合预期；当前完整 CI 又实际覆盖全部 23 叶。[R1](2026-10-01-phase2-retry-causal-r1/README.md)、[R2](2026-10-01-phase2-retry-causal-r2/README.md)。 |
| 浏览器因果 | `80bcd46` 四例 3P/1F，恢复请求/响应对象混比失败；按原 provider-only 契约修正后 `03e5eee` 单恢复例通过，409/新包/剪贴板/贴回全抵达。[原结果](2026-10-01-phase2-ci-browser-r1-causal-result.md)、[补验](2026-10-01-phase2-ci-browser-r1-manual-recovery-result.md)。 |
| `c8adbae` | 完整后端和 web P；mock125P/2F。real1/3各12P，real1附7P；real2在 apt 依赖安装超过20分钟自然 cancelled，产品测试0抵达、12计划项未执行，非团队取消。汇总门 F。[浏览器原档案](2026-10-01-phase2-final-browser-ci/README.md)、[后端](2026-10-01-phase2-final-memory-ci/README.md)。 |
| `cf3015c` | 两条 mock 修订后127P，完整后端/web P；三轮 real各11P/1F，唯一 G1 在上海跨日后把7天后事项要求在3天窗口首页可见。真实保存/日期/项目/提醒正确，原刷新/撤销未抵达。先冻结补充gold，再仅迁移G1样本为明天，保全部断言、G3默认周五、原白皮书示例及真实星期理解门。[三轮原红与依据](2026-10-01-phase2-final-browser-ci/cf3015cd/README.md)、[后端](2026-10-01-phase2-final-memory-ci/cf3015c/README.md)。 |

[SIWC 自足夹具补验](2026-10-01-phase2-siwc-ci-fixture/README.md)、[改名/撤销审阅](2026-10-01-phase2-owner-rename-review.md)、[外发边界审阅](2026-10-01-phase2-ci-browser-r1-embedding-review.md)分别保留静态与动态边界。R2/R3/R4 曾重复计入父项；[勘误](2026-10-01-phase2-backend-counting-erratum/README.md)及[另一验收者复核17份原日志](2026-10-01-phase2-test-count-audit.md)只修计数，原日志、失败和退出不变。更早被补修取代的 `e6fd9b1` memory/browser 曾由协调者明确取消，不能算通过。

## 尚未完成的门

- 线上11项、真实模型/账号、手机真机、一天用户试用尚无本批通过证据。SQL恢复检查点不替代真实进程崩溃/重启。
- 2.1 时间线回忆、抽取遗漏补查和既有 ChatGPT ZIP/JSON 导入闭环待下一批；[只读拆分提案](../tasks/phase2/next-batch-2.1.md)已整理，不是已实现或已批准的新预算。完整私人历史未迁入。
- 2.2 同模型、同预算、简单混合检索基线的连续任务收益及导入/维护/调用/存储成本仍待实测。
- main 仍为 `d8d6fb3efe92f7711c5567440e92af362df7fe77`；旧 checkout、生产配置和部署快照未动。PR均保留draft，合并与部署等待用户另行明确指令。
