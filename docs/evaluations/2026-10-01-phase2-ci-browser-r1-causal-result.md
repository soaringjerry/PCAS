# 80bcd46 四例真实浏览器因果补验：原结果3P/1F

## 精确版本与执行

精确产品与harness **`80bcd46d6863d5ed6b6283d28ece9b4d066a81fc`**，从该头新隔离工作区 `/tmp/pcas-phase2-ci-browser-causal-r2`，开测前clean。Node24.18.0同头build退出0，actual list退出0，恰四例。源码和dist开测前SHA冻结、执行后再次核对未变；没有更新产品、spec、runner、配置或预期来取得本轮结果。

- golden spec SHA：`b2e9e83d7d575d079f077394f10768935d1a6634cc6a10bfaa90ec38f7331ada`
- manual spec SHA：`7bfa1acab6b0b126573536b46c6ae29ae211efa93af003b180cef6d665257489`
- dist JS `index-KRxlz-XR.js` SHA：`96395ae98a56ee26a35d3acca03286b1f4c9daaabb7729402b72d60112a134f6`
- dist index.html SHA：`ded8446837303f8d733f93ebcd4b6ad20a664b35b91028157c786f92f880ec1d`

[开测前源码、harness、全部dist、选择与命令](2026-10-01-phase2-ci-browser-r1-causal-evidence/preflight.json)。复用授权的共享UI依赖，只在本地ignored node_modules目录内建链接，没有复制产品配置或读取旧checkout、部署资料。原runner custom-command创建新tmpfs Docker PG、随机loopback PG端口、独立owner/token/目录，API18116、回调14559开测前空闲；结束后原runner清理服务和container。唯一既有构建环境修正仍为root已授权 `GOFLAGS=-buildvcs=false`。

```text
PATH=/root/.nvm/versions/node/v24.18.0/bin:$PATH GOFLAGS=-buildvcs=false PCAS_GOLDEN_PORT=18116 PLAYWRIGHT_JSON_OUTPUT_NAME=test-results/phase2-ci-causal.json bash web/tests/support/real-backend.sh npx playwright test tests/golden.spec.ts tests/phase2_manual_backend.spec.ts --grep 'G5 |F7 连续撤销|real_target_package_clipboard_submit|real_revoke_clear_refuse_regenerate_submit' --retries=0 --output=test-results/phase2-ci-causal --reporter=list,json
```

实际serve+worker+真实PG/Chromium clipboard，API未fulfill/stub；仅外部provider/OIDC/通知继续沿原golden合成fake。不是生产模型、真实外部接收回执或最终三轮CI。原0032883三轮红与sharedaux两manual绿仍独立保留。

2026-10-01 **15:22:30.812794至15:22:55.966485 UTC**，runner25.154s，Playwright18.647528s，**exit1；4例全部抵达、3PASS/1FAIL/0SKIP/0flaky/retry0，正式执行次数1**。

| 原四例 | 实际终态 | 耗时 |
| --- | --- | --- |
| G5首失败真实重试 | PASS | 7.143s |
| F7连续撤销及刷新 | PASS | 1.957s |
| manual正常目标→GET→clipboard→提交 | PASS | 3.559s |
| manual撤权→拒旧包/旧paste→原目标恢复 | **FAIL，旧形状比较** | 5.164s |

[完整原runner日志gzip](2026-10-01-phase2-ci-browser-r1-causal-evidence/runner-original.log.gz)、[原Playwright完整JSON报告gzip](2026-10-01-phase2-ci-browser-r1-causal-evidence/playwright-report-original.json.gz)、[精确进程exit/时刻](2026-10-01-phase2-ci-browser-r1-causal-evidence/execution.json)、[逐例失败原文、全部文件SHA](2026-10-01-phase2-ci-browser-r1-causal-evidence/evidence-manifest.json)保留，不把后续修订盖到本轮。

## 三项通过的实际证据

G5实际requestRun **200**，sourceRunId等于正式snapshot的原failed Run `2498be3c-c291-4d27-b6e7-8b0d6ee2a8e3`，没有manualRecipient键；原完成三步骤与实际副手model events数2断言通过。[实际请求附件](2026-10-01-phase2-ci-browser-r1-causal-evidence/G5-G5-real-retry-request-attachment-original.json)。同runner后续manual case被动捕获的真实workspace进一步显示新Run `78925aff-4b81-4904-a83b-5f5fd0f3924d` done、auto/subtasks及真实事项checklist3，见[只读完成状态抽取](2026-10-01-phase2-ci-browser-r1-causal-evidence/G5-completed-state-readonly-extract.json)。

F7原两Undo、删除、reload同title两回执及均已撤销断言全部通过；[原桌面截图](2026-10-01-phase2-ci-browser-r1-causal-evidence/F7-browser-original.png)保留。成功用例按既有retain-on-failure不产trace，不能声称另外保存了该例reload原HTTP包；没有为取得trace重跑或改配置。

manual正常例原目标provider-only、server Task绑定、每次独立GET package、两个不同attempt UUID/unknown external receipt、真实clipboard readText严格等于第二包、提交及auto doc、owner notes保留与副手事件0全部通过。[完整两包/manifest及原Run附件](2026-10-01-phase2-ci-browser-r1-causal-evidence/manual-normal-two-real-packages-attachment-original.json)、[实际HTTP附件](2026-10-01-phase2-ci-browser-r1-causal-evidence/manual-normal-real-http-attachment-original.json)、[package UUID/SHA/input/recipient索引](2026-10-01-phase2-ci-browser-r1-causal-evidence/package-index.json)、[原完成截图](2026-10-01-phase2-ci-browser-r1-causal-evidence/manual-normal-browser-original.png)。clipboard严格等值来自真实浏览器原断言PASS，外部是否收到仍unknown。

## 恢复例原失败与正式绑定核查

失败在 `phase2_manual_backend.spec.ts:223`：

```ts
expect(regenerationBody.manualRecipient).toEqual(run.manualRecipient)
```

**Expected来自旧Run响应**：

```json
{"principal_id":"","role":"","model":"","provider":"golden","protocol":"","channel":"","route_fingerprint":""}
```

**Received来自实际UI恢复POST**：

```json
{"provider":"golden"}
```

[实际完整request body、旧Run、恢复200、新Run、完整旧/新Task recipient、只读比较及原失败上下文](2026-10-01-phase2-ci-browser-r1-causal-evidence/manual-recovery-shape-cause.json)。旧Run `cfed4221-8006-406f-9634-f89edf4ae77e`；实际requestRun携带该精确sourceRunId与provider-only selection，返回新Run `0d552c6b-b366-403b-b5f3-3d7770a8e586`、waiting/not-stale、同原prompt。旧新 `contextTask.recipient`逐字段完全一致：principal/role/channel均manual、model/provider均golden、protocol openai，fingerprint均 `f8796900bfc4d0637ffe9c38f7bd5a083576d3b5b4bbc9978d72a3f8ba8ab142`。

因此**同provider已被当前正式服务端Task绑定同一实际目标**，不是重试换route或漏绑定。后面新Run/recipient/sourceRunId的原断言尚未抵达；上述事实是只读已捕获真实响应核验，不能把它们记作本用例后续断言PASS。

边界原契约未要求请求重传空授权字段：UI domain `ManualRecipientSelection`仅provider；phase2 contracts §2/§8写owner明确选择由唯一服务端helper重建实际destination，Run可信Task/绑定不能由客户端JSON提供。`run_context.go:132`把原client selection放入Run.ManualRecipient；其Go类型为`memory.Recipient`，`context_contract.go:58`六个identity字段json tags不带omitempty，响应会展开空字符串；`context_recipient.go:34-88`由provider重建完整current recipient，并只核客户端非空identity是否一致。新UI剔除无用/权威字段正符合原provider-only选择，旧deep-equal误把响应零值形状当请求gold。

该失败责任为**独立旧harness对请求/响应形状的错误比较**。不以embedding query后续工作解释此字段失败；本轮Prompt为直接owner输入。产品当前实际200及Task完整绑定不降低原隐私约束。

## 未抵达列表

恢复例已抵达并通过：合法旧包preview、正式撤权200、轮询stale/清pre/禁copy与paste、实际clipboard sentinel不变、旧GET409及旧paste409 exact version_conflict、未保存output/doc、点击原目标重新生成及其200。

首个错误形状assert之后这些原断言**未执行**：prompt保持、新RunID、sourceRunId原ID、new waiting/not-stale/完整Task recipient、旧stale仍留、两row及新row选取、恢复safe preview/manifest/拒撤权atom、独立copy包/clipboard/不同attempt、恢复paste/auto doc/owner notes、副手事件0及最终恢复截图。本报告不把readonly响应核验代替这些尚未完成步骤。

[失败后finally被动保存的完整真实HTTP](2026-10-01-phase2-ci-browser-r1-causal-evidence/manual-recovery-real-http-attachment-original.json)、[原error-context gzip](2026-10-01-phase2-ci-browser-r1-causal-evidence/manual-recovery-error-context-original.md.gz)及trace原ZIP完整保留于 `/tmp/pcas-phase2-ci-browser-causal-r2-original/manual-recovery-trace-original.zip`；原trace SHA和文件路径登记在manifest。其[末个原screencast frame](2026-10-01-phase2-ci-browser-r1-causal-evidence/manual-recovery-last-trace-frame-original.jpeg)仅是原浏览器捕获帧，不代表未抵达的恢复preview/copy已操作成功。APIRequestContext直发的旧409不在page.response附件中，但原trace原包及精确原断言都已抵达，未用任意错误替代。

## 有因gold修订理由（单独spec提交，未在本轮执行中迁移）

root已批准此具体迁移：把恢复request与服务端展开六零值字段的比较，替换为**exact `{provider:run.manualRecipient!.provider}`，并显式要求原provider等于固定UI选择golden**。sourceRunId原ID、新ID、原prompt、完整server Task recipient逐字段等值及后续全部package/409/clipboard/拒原子/owner保护/提交断言不变。不用partial match容纳客户端fingerprint/role/channel，也不允许改目标。

修订依据是开测前原provider-only契约及已保存的实际请求/Task绑定，不是让产品错误通过。原3P/1F、第一失败和未抵达清单不改。下一步须单独净spec提交、compile/list后由root指定产品+harness新精确头；仅该恢复例一次有因补验，不重复其余三项P。最终含后续产品变化的完整CI另为独立门。
