# a51fdef：独立最终 memory CI

[run 36890371161](https://github.com/soaringjerry/PCAS/actions/runs/36890371161)，job `110464172035`，2026-10-01 16:13:33Z–16:21:49Z 自然 **success**。只读完成后全 log；未触发 / 取消 CI、未本地运行 test 或 list。

## 精确版本和终态

head `a51fdef8670fb6152ac8ca290e69390f496edf33`；runner actual checkout `ac62139ee0dc99f11ee4ae31365b8bf091b3150d`（log 238、240–241 行）。GitHub git object 与本地 head 完整 tree 同为 `e647f4fe63b12d1e0d81efad13bee87801e5ff12`。相比 cf 仅修改 `web/tests/golden.spec.ts` 日期 fixture；Go 产品 / tests / 配置 / 迁移 / phase2 gold SHA 均不变，详见 head-comparison / version / gold-and-test-sha256。

完整 `make check` 的 fmt-check / vet / 全 `go test -race ./cmd/... ./internal/...` / 最后 build 成功、退出 0。最后 `go build -trimpath -o bin/pcas ./cmd/pcas` 在第 112376 行，随后直接进入 successful post steps；日志没有额外 package 编译输出，**不冒称无编译缓存或全新编译**。

**11 个测试包全真实计时，无 `(cached)` 测试包**；4 个无测试文件包。341 顶层 = 338 PASS + 3 旧 SKIP，755 真叶 = 752 PASS + 3 旧 SKIP；FAIL / 未抵达 / 未终态 / race / panic / timeout 均为 0。fresh 计数等于全部计数，cache 回放计数为 0。原 5 顶层 23 叶同头全 P，终态独立保留，未借用 c8 / cf 的通过结果。

| 包 | 顶层 | 真叶 | PASS | 旧 SKIP | 真实耗时 |
| --- | ---: | ---: | ---: | ---: | --- |
| internal/ai | 13 | 21 | 20 | 1 | 1.431s |
| internal/ai/siwc | 12 | 34 | 34 | 0 | 7.611s |
| internal/blob | 1 | 1 | 1 | 0 | 1.029s |
| internal/config | 1 | 1 | 1 | 0 | 1.011s |
| internal/connectors | 2 | 2 | 2 | 0 | 1.021s |
| internal/httpapi | 9 | 23 | 23 | 0 | 1.253s |
| internal/memory | 6 | 31 | 31 | 0 | 1.022s |
| internal/notify | 10 | 10 | 10 | 0 | 5.764s |
| internal/postgres | 243 | 570 | 568 | 2 | 414.544s |
| internal/telegram | 36 | 52 | 52 | 0 | 10.947s |
| internal/workspace | 8 | 10 | 10 | 0 | 1.077s |

3 旧 SKIP 分别是 postgres 的 `TestLiveCodexSecretaryAndLegacyFormats` / `TestLiveContinuityReplay`（独立真实账号及 embedding 未配置）和 ai 的 `TestInstalledCodexHandshake`（可选 CLI binary 未配置）。保原 opt-in 逻辑，不当真实模型 / 已安装 CLI 实测通过。4 无测试文件包仍是 cmd/pcas、ai/contextwire、testsupport/golden、worker。

## 归档与对照

`full.log.gz` 是完整 112524 行 raw，解压 SHA256 `885eb9c709584caf7050c5429a192eb5c83dfb4cc06e2e1ab7d8521ba3fdc429`。`evidence.tar.gz` 内单一合并 JSON 含 1074 条完整 source-stamped 证据，每条保原 log 行界与源文件位置。summary / counts / selection-reconciliation / retry23-same-head 包含逐包终态、真叶及声明 341→logged RUN 341、零缺失与新 23 叶终态。

`recount.py` 可只读解压 log 重算，排除全部实际 Test 祖先和 package 终态，并区分 cached 包；本轮无测试缓存回放。run / version / gold-and-test-sha256 / artifact-sha256 / evidence-body-sha256 保元数据与各 SHA。

c8、cf 原文件不覆盖，父 README 仅追加本轮对照。`parent-readme-before-a51.md` 保父 README 追加前的完整副本及 SHA，可复核 cf 原 manifest 的父 README 项；c8 最初 README 仍在 cf 子目录，原两个 commit 的 SHA 均可复核。

仅此固定 head 的 Go 技术门通过；浏览器旧红与其后续独立验收不被覆盖。线上 11 题、真实模型 / 真机、一天试用、真实进程恢复仍不由此证明。
