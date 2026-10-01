# 新 manual：真实后端浏览器首次结果（保留失败）

## 精确基线与环境

- 产品与组合 harness HEAD：`3963fd98cd51c0f553f6f927f8e19ac3ba91f910`；源自 root 审定产品 `207bd7062f02cf95d4d29ebaba1391d25a7972f1`，后续 Name/Prompt 修复未混入。
- 原冻结 `3df4049972c88bab89f1d4bfa69e2dff257878e2`，独立 harness `baddaac53fa3e2bc58d1ac16582882b12c434955`（root cherry 为 3963fd98）。
- spec SHA256 `2073622d8bf778a05570a0d1ae6afa99da7f1e798db61f626efb8f61f81ec6c5`，所有断言与静态交付一致。
- 新隔离 worktree `/tmp/pcas-phase2-manual-browser-real`，开始 tracked clean；临时共享 node_modules symlink 非产品文件，收尾已移除。旧 checkout/部署未使用。
- 同 HEAD 独立 `npm run build` exit 0；Node 24.18.0、Go 1.26.8、Playwright 1.63.0、Chromium 153.0.8010.12/Linux，1440×1000 桌面浏览器，Asia/Shanghai。
- dist/index.html SHA256 `2b1d555c07e3cc54bf1fc0359b3ef33409df49c786500c44e03e95c21f85a609`；JS index-BBeP_cpR.js SHA256 `f7245cf9cd1210e438b43e422ad876b15c608c83cc08e30c528ad6e2ebc8280c`。
- existing real-backend.sh custom-command；己 tmpfs Docker PostgreSQL、随机 loopback PG 端口、新 owner/token/config 与 golden 合成服务；PCAS HTTP 127.0.0.1:18112，callback 14559。runner 退出清己容器/进程。没有真实账号、模型、个人资料，没有 API/clipboard stub。

## 启动批次分别计数

| 批次 | 退出 | 实际执行 | 结果 |
|---|---|---|---|
| 首次 runner 启动 | 1 | 0 tests | Go build VCS status exit128；服务与浏览器未启动，不算测试 FAIL |
| root 批准受控构建环境后启动 | 1 | 2 tests，0 PASS / 2 FAIL | 两例都在真实 source authorization setup 返回501；skip0/flaky0/retry0 |

首次原日志 `run1-original.log` 仅包含 `error obtaining VCS status: exit status 128`，未擅自原样碰运气重启。只读 git 状态正常，独立构建诊断 fixture/pcas 均 exit0（日志原件在 `/tmp/pcas-phase2-manual-browser-real-evidence/go-*-infra-original.log`）。root 明确批准仅本次 `GOFLAGS=-buildvcs=false`，runner 自身没有其它 GOFLAGS 要求；不改仓库 runner、源码或测试行为，源码哈希另录。

受控启动命令：

```sh
PATH=/root/.nvm/versions/node/v24.18.0/bin:$PATH GOFLAGS=-buildvcs=false PCAS_GOLDEN_PORT=18112 PLAYWRIGHT_JSON_OUTPUT_NAME=test-results/phase2-manual-backend.json bash web/tests/support/real-backend.sh npx playwright test tests/phase2_manual_backend.spec.ts --retries=0 --output=test-results/phase2-manual-backend --reporter=list,json
```

受控启动 JSON startTime `2026-10-01T14:29:23.386Z`，duration 2797.702ms。`real_target_package_clipboard_submit` 722ms FAIL；`real_revoke_clear_refuse_regenerate_submit` 526ms FAIL。两例均到达，失败后不跳过下一例；没有测试重试。原日志、JSON、服务日志与 trace 保留。

## 实际失败与责任

两例源入库真实201、owner addTask/setNotes真实成功；下一步真实 POST `/v1/memory/sources/{id}/authorization` **501 / `{"error":"capability_not_configured"}`**，原断言200保持。请求为 exact source `{id,version:1,kind:source}`、recipient `{principal_id:manual,role:manual,provider:golden}`、unscoped knowledge scope，没有凭空的fingerprint或私有授权注入。真实请求/响应见 `case-1-actual-api.json` 与 `case-2-actual-api.json`。

产品原因有明确静态证据：

1. `cmd/pcas/main.go:144` 给 `httpapi.New` 的 Sources 参数是 `memory.NewService(db)`。
2. `internal/memory/service.go` 的 `Service` 仅实现 Ingest/GetSource，未实现 SourceAuthorizer / SourceScopeEditor。
3. `internal/httpapi/source_authorization.go:10` 对 `s.sources` 的 SourceAuthorizer type assertion 失败，setSourceAuthorization 在 decode/正式 db 授权前 ErrUnavailable。
4. `internal/httpapi/server.go:239` 把该错误映射成501/capability_not_configured，吻合两份真实 response。

这是正式 serve facade 的产品接线缺陷；不把缺接口归为 selector/请求 shape 错误，不改200断言。root 已交 A 修产品。另只读核对同 Service 的 2.0 新端口：当前 httpapi 对 `s.sources` 的可选接口只有上述授权与 scope 两类，没有发现第三个同类端口；manual package 经 Options.Workspace，Notifier 嵌入 *Store，静态接口可转发，但本批尚未运行到 package，不能宣称动态成功。没有扩泛审计或产品修改。

## 未抵达与证据边界

- 未抵达 UI friendly target 选择/requestRun、preview GET、copy GET、attempt manifest、真实 clipboard readback、paste/自动采纳、撤权 stale/精确409/恢复。
- 本批没有包或manifest/clipboard证据；失败前截图来自 trace 最后桌面帧，仅首页 setup，不能称闭环截图或真机。
- 原26 mocked与旧三轮未重跑，也不能代替本批真实闭环缺口。
- worker 有 synthetic source.embed 失败 WARN，未导致本次501，501在HTTP facade typeassert已有独立充分归因；不将其忽略为成功，也不无限扩大此批。

评测目录保存原 build/runner/Playwright/服务日志、从 trace 导出的相关真实API正文（无headers/cookies/token）、error context 与失败前截图。完整原始 trace 未改保存在 `/tmp/pcas-phase2-manual-browser-real-evidence/case-1-original-trace.zip` 与 `case-2-original-trace.zip`，SHA见 trace-index.json。trace包含己临时 Chromium会话细节，仓库仅保存最小非秘密API正文与截图，原 trace 没有删除/重写。

等待 root 指定 A 接线修复产品 head，再针对这两例作有因单次补验；本失败报告不覆盖、不改变。
