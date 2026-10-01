# cf3015c：独立最终 memory CI

[run 36888958920](https://github.com/soaringjerry/PCAS/actions/runs/36888958920)，job `110459377955`，2026-10-01 16:02:35Z–16:11:46Z 自然 **success**。仅只读完成态与完成后全日志；无本地 test/list、dispatch、取消或重跑。

## 版本与结果

head `cf3015cd67cf112933dfa51badbda8507de30834`，actual checkout `ba946b860dea2ef81b83c2c1ec3eb5c63f4d1562`（log 241、243–244 行），两者完整 tree 均为 `52b5a793517add9741df26a08ca64950aa05f034`。与 c8 相比仅 `web/tests/phase2_manual.spec.ts` 改动；Go 产品 / tests / 配置 / 迁移 / gold 均完全相同（只读 diff 与 SHA 文件分别保存），不是改变 Go 预期后重跑。

make check 的 fmt-check / vet / 全 `go test -race ./cmd/... ./internal/...` / 最后 `go build -trimpath -o bin/pcas ./cmd/pcas` 成功，退出 0。build 命令及 cmd 输出在 log 112366–112367 行；后续 job 收尾 success。

**11 测试包全部真实计时、零 `(cached)` 包**；4 无测试文件包。341 顶层 = 338 PASS + 3 旧 SKIP；755 真叶 = 752 PASS + 3 旧 SKIP；0 FAIL、未抵达、未终态、race、panic 或 timeout。故本轮 fresh 计数等于全部计数，cache 计数为 0。原 5 顶层 / 23 因果叶同头全 PASS，未借用 c8 终态。

| 包 | 顶层 | 真叶 | PASS | 旧 SKIP | 真实耗时 |
| --- | ---: | ---: | ---: | ---: | --- |
| internal/ai | 13 | 21 | 20 | 1 | 1.289s |
| internal/ai/siwc | 12 | 34 | 34 | 0 | 8.918s |
| internal/blob | 1 | 1 | 1 | 0 | 1.017s |
| internal/config | 1 | 1 | 1 | 0 | 1.010s |
| internal/connectors | 2 | 2 | 2 | 0 | 1.024s |
| internal/httpapi | 9 | 23 | 23 | 0 | 1.243s |
| internal/memory | 6 | 31 | 31 | 0 | 1.026s |
| internal/notify | 10 | 10 | 10 | 0 | 5.514s |
| internal/postgres | 243 | 570 | 568 | 2 | 467.368s |
| internal/telegram | 36 | 52 | 52 | 0 | 11.492s |
| internal/workspace | 8 | 10 | 10 | 0 | 1.069s |

PG 原 2 live opt-in：`TestLiveCodexSecretaryAndLegacyFormats`、`TestLiveContinuityReplay`；AI 另 1 installed CLI opt-in：`TestInstalledCodexHandshake`。三门保持原环境要求未启用，不当真实模型通过。4 无测试文件包仍是 cmd/pcas、ai/contextwire、testsupport/golden、worker。

## 全量证据

`full.log.gz` 是完整 112528 行 raw，解压 SHA256 `56d494eec29a5a7d8162967f656df1b15ef9e2e19d0d7ed443de49737ea772fe`。`evidence.tar.gz` 内一份合并 JSON 含 1074 条完整 source-stamped 证据，保日志行界；未保存散文件。

summary / counts / run / version / selection-reconciliation / retry23-same-head / gold-and-test-sha256 / artifact-sha256 分别给精简终态、逐包真叶、原始 GitHub 元数据、实际 tree、341 声明→341 logged RUN 对应与零缺失、23 终态、固定源码 SHA 和归档 SHA。`recount.py` 只读日志可复算；排除全部 Test 祖先，按包区分 cached 回放与新执行，本轮无回放。

c8 旧全部证据与统计不覆写；父 README 仅追加对照，原 README 完整副本 `c8-readme-original.md` 与 c8 原 artifact manifest 的 SHA 一致。原 c8 `artifact-sha256.json` 的 README 项仍指 71310bb 原版本；本轮 append 后父 README SHA 在本子目录 manifest 单列。

此报告只证明固定 cf tree 的 Go 技术门。浏览器三轮红由 root / D 独立保留、核因，不由 memory success 覆盖；后续仅 web fixture 新头不混入本次结果。真实模型、真机、线上 11 题、一天用户试用与实际进程恢复仍不由本报告证明。
