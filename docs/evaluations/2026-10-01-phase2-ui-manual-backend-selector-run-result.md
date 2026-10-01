# 真实 manual：selector 单点修正后结果（产品序列化故障）

执行 exact HEAD `34acb4f9c62eb60dac6aa1c8a3b0d9db28f11f25`，产品仍为 `dc766ca1b0e80ef39908f14e81f0703cb1a692c9`（新增此前红报告+已审selector修正）；harness spec SHA256 `84e75a62eeb73db8d13f5a9df0f063349ad04446ab17bf1c53169f43b5a78606`。root批准一次有因补验，开始clean，无业务断言修改。

同产品dist hash核对一致并复用：index.html `2b1d555c07e3cc54bf1fc0359b3ef33409df49c786500c44e03e95c21f85a609`，JS `f7245cf9cd1210e438b43e422ad876b15c608c83cc08e30c528ad6e2ebc8280c`。独立己runner容器、loopback随机PG、18112API、14559callback/new synthetic owner/token/config；Node24/Go1.26.8/Playwright1.63.0/Chromium153 Linux桌面1440×1000。旧mocked/三轮不运行，未改runner。GOFLAGS=-buildvcs=false是root既批准的构建环境。命令：

```sh
PATH=/root/.nvm/versions/node/v24.18.0/bin:$PATH GOFLAGS=-buildvcs=false PCAS_GOLDEN_PORT=18112 PLAYWRIGHT_JSON_OUTPUT_NAME=test-results/phase2-manual-backend-selector.json bash web/tests/support/real-backend.sh npx playwright test tests/phase2_manual_backend.spec.ts --retries=0 --output=test-results/phase2-manual-backend-selector --reporter=list,json
```

## 原结果

**exit1，0 PASS / 2 FAIL；两例均到达，skip0/flaky0/retry0。** JSON startTime `2026-10-01T14:36:44.339Z`，duration4426.690ms；目标/复制例1.7s，撤权/恢复例1.4s。保持前次501与selector超时两批真实红证据，没有覆盖或重写。

本轮两例实际 source201/authorization200、owner说明、UI真实catalog200/golden available、目标选择成功、requestRun200。其实际body manualRecipient仅 `{provider:golden}`，原prompt/thingId/manual/ask保持。源typeddependency和server recipient binding在真实状态响应中存在。随后页面因读取undefined route_fingerprint崩溃。

## 产品原因：持久Task JSON与UI契约不同

实际requestRun200的Run.contextTask含默认Go大写 `OwnerID/Recipient/Purpose/Scope/View/Now/Timezone/MemoryBudget/TotalInputTokens`；`Recipient` 内的 snake字段完整，包括有效manual route_fingerprint。`internal/memory/context_contract.go` TrustedTaskContext普通字段缺json tag。UI/domain types以及RunRow依赖的是 `contextTask.recipient` 小写，在 `run.contextTask?.recipient.route_fingerprint` 上访问undefined引发真实pageerror，React页面崩溃。

harness createThroughUI119的小写recipient exact断言也真实失败；finally中的独立pageerror空数组断言成为Playwright最终显示错误，**原最初业务diff仍在完整trace**，本评测额外原样导出 `case-{1,2}-original-trace-errors.json` 以保留这层诊断。poll内部Expected0/Received1或3记录是正常自动轮询未满足时的中间记录，poll最后正常通过；没有因这种轮询失败重跑测试。

这是当前真实后端与UI接口接合的产品缺陷，不应只把测试改成读大写绕过崩溃；应由A统一公开序列化与已有持久JSON兼容，保持正式绑定/不信任客户端Task。未修改产品/测试断言。

## 未抵达

包GET、package/attempt manifest、真实clipboard readback、paste/autodoc、revoke stale/409/recover均未执行。没有真实包/clipboard成功证明；当前截图只是崩溃前/崩溃页的桌面trace帧，不能称交接闭环通过或真机。source auth修复与selector修正正控已实际抵达，不替代整个两例通过。

原runner/JSON/services、非秘密实际API正文、trace errors、error contexts与截图位于 `2026-10-01-phase2-ui-manual-backend-selector-run-evidence/`；完整未改trace保 `/tmp/pcas-phase2-manual-browser-selector-evidence/case-{1,2}-original-trace.zip`，SHA见trace-index。原trace含己临时会话，仓库仅保存无headers/cookies/token的API正文。runner清己容器，临时node_modules借用symlink已移除。

CI新spec清单追加尚未执行：root授权前提为这两真实例通过，本轮失败不满足。等待具体产品修复head再补验；不自动重试或放宽断言。
