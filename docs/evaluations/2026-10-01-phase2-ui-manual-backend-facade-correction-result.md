# 真实 manual：源 facade 修复后补验（保留 harness 失败）

精确产品组合 `dc766ca1b0e80ef39908f14e81f0703cb1a692c9`，含正式源 facade 修复 ea00943（原 d9083ac）。harness 原 baddaac53/spec SHA256 `2073622d8bf778a05570a0d1ae6afa99da7f1e798db61f626efb8f61f81ec6c5` 完全不变。新隔离 worktree `/tmp/pcas-phase2-manual-browser-correction`，开始 tracked clean，无旧 checkout/配置/生产使用。

同头 Node24 npm build exit0；dist html SHA `2b1d555c07e3cc54bf1fc0359b3ef33409df49c786500c44e03e95c21f85a609`，JS SHA `f7245cf9cd1210e438b43e422ad876b15c608c83cc08e30c528ad6e2ebc8280c`。复用已有 runner custom-command、己 tmpfs DockerPG/random loopback端口、18112 API、14559 callback、新合成owner/token/config、golden外部fakes。显式 `GOFLAGS=-buildvcs=false` 沿 root 已批准构建环境；没有改 runner。与前次同命令，仅日志路径独立：

```sh
PATH=/root/.nvm/versions/node/v24.18.0/bin:$PATH GOFLAGS=-buildvcs=false PCAS_GOLDEN_PORT=18112 PLAYWRIGHT_JSON_OUTPUT_NAME=test-results/phase2-manual-backend.json bash web/tests/support/real-backend.sh npx playwright test tests/phase2_manual_backend.spec.ts --retries=0 --output=test-results/phase2-manual-backend --reporter=list,json
```

## 原结果

**exit1，0 PASS / 2 FAIL；两例均执行，skip0/flaky0/retry0。** JSON startTime `2026-10-01T14:33:15.124Z`，duration61837.874ms。目标/复制例30.1s，撤权/恢复例30.2s；两例同一 selector 超时，未自动重试。

正式 source ingest201、authorization200已真实成功；owner独立说明保留，UI打开表单、填prompt、未选择target时建立按钮disabled断言通过，UI自己的真实GET /v1/models200、golden友好名/available断言通过。随后 harness line106 的 exact `getByRole(button,{name:转交接收者})` 与真实accessible name `转交接收者：选择接收者` 不匹配，等待30秒超时。

这是D独立harness定位错误，责任明确，不是产品故障；没有将可用真实选项当成不可用。静态 controls.tsx156 的 aria-label `${label}：${current?.label ?? placeholder}` 和两份真实error-context快照提供独立充分证据。parent已批准将该定位改成完整已观测label+placeholder；修复需另净提交，不改本轮原记录。

未抵达选真实模型、requestRun、包/manifest、真实clipboard读回、paste、撤权409/恢复。authorization200证明 facade 入口修复，但不证明未抵达业务闭环，source scope GET/PUT也没有在这两例执行。原26/旧三轮未重跑，旧501红报告不覆盖。

证据目录 `2026-10-01-phase2-ui-manual-backend-facade-correction-evidence/` 保原build/runner/JSON/services、实际API正文（无headers/cookies/token）、错误context与桌面失败帧。完整trace在己 `/tmp/pcas-phase2-manual-browser-correction-evidence/case-{1,2}-original-trace.zip`，SHA见trace-index；原trace未改写/删除。截图仅未选接收者表单，不称完成闭环或真机。临时node_modules借用symlink收尾移除。
