# 0032883 实际 CI 浏览器失败独立归因

## 版本、原运行与范围

正式 CI [run 36880375418](https://github.com/soaringjerry/PCAS/actions/runs/36880375418)，run attempt **1**，精确产品及测试源码 **`0032883950d7dc72b1bdb160c1a98e3da17eded9`**。本审阅在从该头新建的隔离工作区 `/tmp/pcas-phase2-ci-browser-r1-review` 只读进行，没有运行产品测试、改产品、改runner、改旧spec、放宽断言或重试浏览器。后续作者修复不属于本次失败证据。

`web/tests/golden.spec.ts` SHA256为 **`4e7272195a889f67e38ef4486919e0595bf4ef39ceaec126c2cd039ab177d1f5`**；两新真实manual例spec SHA仍为 **`84e75a62eeb73db8d13f5a9df0f063349ad04446ab17bf1c53169f43b5a78606`**。其余产品/runner SHA、原artifact每文件SHA、下载ZIP SHA及GitHub digest均在 [输入与交付清单](2026-10-01-phase2-ci-browser-r1-evidence/original-input-manifest.json)。三个下载ZIP的SHA全部吻合GitHub artifact登记digest。

| 原CI批次 | 原artifact ID | golden实际终态 | G5 / F7连续撤销耗时 | golden retries / skips | 原runner exit |
| --- | --- | --- | --- | --- | --- |
| round1 | 11171382802 | 12例：10P / 2F | 24.035s / 7.749s | 0 / 0 | 1 |
| round2 | 11170683166 | 12例：10P / 2F | 23.573s / 6.857s | 0 / 0 | 1 |
| round3 | 11171233091 | 12例：10P / 2F | 24.037s / 7.851s | 0 / 0 | 1 |

三轮失败都恰为 `G5 失败恢复：副手首请求报错，原地重试成功` 和 `F7 连续撤销：改期、新建依次撤销，刷新保持已撤销`。`F7 顺序冲突`、`F7 采纳链`在三轮均通过；不能用这两项通过覆盖连续撤销的真实回归。原报告：[round1](2026-10-01-phase2-ci-browser-r1-evidence/round1-golden.json.gz)、[round2](2026-10-01-phase2-ci-browser-r1-evidence/round2-golden.json.gz)、[round3](2026-10-01-phase2-ci-browser-r1-evidence/round3-golden.json.gz)，gzip只压缩原报告字节，完整原失败信息保留。

round1同一workspace的 auxiliary **6例全部PASS，0skip、0retry、0flaky**，包含 `real_target_package_clipboard_submit` 与 `real_revoke_clear_refuse_regenerate_submit` 两新真实manual例；[原legacy报告](2026-10-01-phase2-ci-browser-r1-evidence/round1-legacy.json.gz)确认它们在旧backend/chatgpt-direct/continuity/model-api之后执行。该通过包括原spec的实际Chromium clipboard和真实API断言，不是本次新执行；仍不能使本CI整体转绿。round2/3artifact没有auxiliary报告，本审阅不把round1的6P重复归到它们。

## G5：UI把API绑定误传到manualRecipient，正式后端400早拒

三轮均观察到：

1. 原fixture control设定秘书 `delegate/breakdown/拆成三步` 及副手 `status:503, once:true`；DeskTurn实际200建立原API Run，随后真实workspace轮询显示该Run为`failed`。
2. 用户实际点击“重试”。POST `/v1/workspace/commands` 为 `type:requestRun`、`agentId:golden`、同thing/kind/prompt，却错误附带 `manualRecipient`。该对象是原 `contextTask.recipient` 完整绑定，含 `role:deputy`、`channel:api`、model/provider/protocol及route fingerprint。
3. 正式后端实际返回 **400 `{"error":"invalid_input"}`**；后续workspace仍只有原failed Run，没有新Run。等待20秒不会改变这次早拒，原失败不是selector点击错位或上游第二次输出格式错误。

| 轮次 | retry实际时间UTC | 返回 | 之后该事项Run |
| --- | --- | --- | --- |
| 1 | 2026-10-01 14:58:49.457 | 400 invalid_input | 原7a611551…，failed；无新增 |
| 2 | 2026-10-01 14:58:13.505 | 400 invalid_input | 原d8d83a1c…，failed；无新增 |
| 3 | 2026-10-01 14:58:24.432 | 400 invalid_input | 原effc24f1…，failed；无新增 |

完整实际请求、响应body SHA、原/最终Run和recipient：[round1](2026-10-01-phase2-ci-browser-r1-evidence/round1-G5-http-evidence.json)、[round2](2026-10-01-phase2-ci-browser-r1-evidence/round2-G5-http-evidence.json)、[round3](2026-10-01-phase2-ci-browser-r1-evidence/round3-G5-http-evidence.json)。

静态因果与实际请求一致：`ThingPage.tsx:693` 的 `originalManualRecipient` 无条件fallback到 `run.contextTask?.recipient`；`:845`同agent retry、`:870`和`:889` regenerate调用复用该值。`context_recipient.go:36-41`只允许manual agent带此参数，API agent收到非nil值按正式契约返回Invalid。**责任是UI请求组装；后端拒绝正确，不能以放宽服务端输入验证修复。**

计数/协议证据边界：trace记录了503-once的配置与首Run失败，没有保存该用例后续 `/control` GET的实际model events；原spec `events()`副手调用数2断言在等待成功后，因等待失败而**未抵达**。因此本报告不声称观察了首个上游HTTP响应的503原包或实际第二model事件计数。retry没有外发新模型调用的判断来自实际400、没有新Run及早拒代码路径，是因果推断；不是把未抵达的调用数断言算作通过。正式fake `main.go:177`已按Run协议包装原output/used，原503-once规则保留，不是旧plain-text fixture问题。G5没有clipboard操作。

## F7：两次Undo已成功，刷新后更新回执被整体泛化

三轮均实际观察到：

- 秘书create_task及随后改期DeskTurn均200，两个回执原text都含该批独有title，并绑定同thingId。
- 点击更新Undo、再点击创建Undo，两次POST均 **200**；第二次后workspace不再有该事项，首页对应条目为空。旧断言在刷新前均已抵达且通过。
- reload实际GET `/v1/desk/turns` 200返回两个回执，二者`undone:true`。create回执保留原op/title/thingId；update回执被改为`op:"action"`、`text:"这项操作已完成；相关回答内容已隐藏"`、`thingId`缺失、`undoable:false`，其reply也改为`（这条回答依据的记忆已变更）`。
- 所以DOM同title回执实际只匹配1条，原 `toHaveCount(2)`失败；不是第二次Undo未发生、task未删除或页面刷新尚未拿到回执。

| 轮次 | 两次Undo状态 | reload history时间UTC | 两个回执undone | 同title回执 |
| --- | --- | --- | --- | --- |
| 1 | 200 / 200 | 2026-10-01 15:02:25.836 | true / true | 1，原期望2 |
| 2 | 200 / 200 | 2026-10-01 15:01:54.971 | true / true | 1，原期望2 |
| 3 | 200 / 200 | 2026-10-01 15:02:06.109 | true / true | 1，原期望2 |

实际两次Undo请求/状态、事项删除状态、reload完整两turn/两receipt JSON及body SHA：[round1](2026-10-01-phase2-ci-browser-r1-evidence/round1-F7-http-evidence.json)、[round2](2026-10-01-phase2-ci-browser-r1-evidence/round2-F7-http-evidence.json)、[round3](2026-10-01-phase2-ci-browser-r1-evidence/round3-F7-http-evidence.json)。

静态读路径解释了已观测的泛化：`desk_turn.go:803`先调用`deskTurnContextTx`，origin检查失败则调用redactor；`secretary_artifacts.go:224`把原action已Undo视为不再有效的派生origin；`context_desk_history.go:218-231`在redactor内仅保actionID/undone审计事实、移除原text和事项关联。创建action撤销后，后续更新turn携带的创建origin使该历史回答被拒读。这一具体origin链因果为代码推断，原artifact不包含DB context_task行，不能冒称已动态读取原DAG或typed依赖为空。

**责任是产品历史回执呈现边界；实际Undo命令成功。** 回复/派生供给的失效门仍应保留；修复需在独立审定的审计事实边界内保留可识别回执，不能删除origin检查或泛化解除所有历史失效。现有golden断言、fixture规则和两次真实Undo序列均保持不变。F7没有clipboard操作。

## 原始资料、截图及可复核边界

下载原ZIP完整保留于 `/tmp/pcas-phase2-ci-browser-r1-original-artifacts/round{1,2,3}-original.zip`；解包后的原trace、网络resource和服务日志未修改。原job日志仍为root已下载的 `/tmp/pcas-phase2-ci-0032883-round{1,2,3}.log`。本目录保存原报告gzip、原服务日志、六张原失败截图及原error-context gzip；HTTP JSON是针对相关API的可复核抽取，排除认证/header/cookie，注明完整body和原trace SHA，不冒充原trace文件。

| ZIP | SHA256 |
| --- | --- |
| round1-original.zip | `169d44171b59575e2db5ae01b3dee925146ff6af1cadedfe7f796884156a8fb9` |
| round2-original.zip | `65a4bdde67881b3f27e9b462931f2ac4ed92b0ab5230bedaa28413e3768b64b9` |
| round3-original.zip | `2a04cdc0fd47457c2da741588b0c103a251b5b526692546c477a92ff8e3b5a59` |

可通过 `gh api repos/soaringjerry/PCAS/actions/artifacts/<ID>/zip > original.zip` 重新下载上述原artifact，按登记ZIP SHA核对；`gzip -dc roundN-golden.json.gz`可还原原Playwright报告。每个原ZIP成员及本次抽取资料的SHA见清单。没有把原trace资源或截图改成新实现的证据。原error-context codeframe空行自带尾空格，首次文档diff-check退出2；交付仅改为gzip保留原字节，`gzip -dc roundN-G5-error-context-original.md.gz`等可无损还原，未重写失败正文。

原失败桌面截图（不是窄视口或真机证据）：[round2 G5](2026-10-01-phase2-ci-browser-r1-evidence/round2-G5-browser-original.png)、[round2 F7](2026-10-01-phase2-ci-browser-r1-evidence/round2-F7-browser-original.png)；另两轮同样各保存G5/F7原截图。服务日志包含既有`source.embed`告警，完整保留；上述400与receipt实际响应已提供更直接归因，没有将告警当成这两项失败原因。

## 待批准的单次因果补验方案

本报告没有执行补验。待两处产品窄修获独立审定、root指定精确组合头后，沿既有real-backend custom-command入口、自有合成PG/ports、同head dist，以retries0**只运行四例一次**：旧golden G5、旧golden F7连续撤销、原两manual_backend真实例。UI helper同时用于manual retry/regenerate，因此两manual例是该具体影响面的保护；不重跑26 mocked、不修改旧三轮CI门或原golden业务期望。

保留G5实际retry请求body（API不带manualRecipient）、正式成功Run/三步骤及实际副手调用数2原断言；F7仍保两次Undo、删除、刷新同title两回执及两次undone原断言。两manual例继续验证真实目标、每次GET package、clipboard、精确409和恢复提交。失败保留并归因，不自动重试。该定点通过也不能替代最终组合头的完整三轮CI。
