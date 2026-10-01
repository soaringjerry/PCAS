# 最终 memory CI：只读独立核验

[原始 run 36887421622](https://github.com/soaringjerry/PCAS/actions/runs/36887421622)，job `110454172211`；2026-10-01 15:50:45Z–15:59:04Z 自然完成 **success**，没有触发重跑或本地补跑。

## 精确版本

PR #44 head `c8adbaeb5c02daa9e83f90f74c216d8cf9ee22bf`；runner 实际 checkout GitHub merge `de9906eeece777e5016d50b0d85a1d0b72a68b40`（原 log 第 212、239、241–242 行）。GitHub git object 与本地 head 的完整 tree 同为 `e615cd44aa99df110a03592d965afafb06a21c8b`。对象父提交、workflow / Makefile SHA 见 `version.json`；全部测试 / phase2 gold / contracts SHA 见 `gold-and-test-sha256.json`。后续任何 web spec 新头不混入本报告。

## 完整结果

实际执行 `make check`：fmt-check → `go vet ./cmd/... ./internal/...` → `go test -race ./cmd/... ./internal/...` → `go build -trimpath -o bin/pcas ./cmd/pcas`。make step success（退出 0），最终 build 命令及 `cmd/pcas` 输出存在（第 112377–112378 行），随后 job 收尾成功。

- 11 测试包全部 ok、4 包无测试文件；所有测试包均有真实耗时，**无 `(cached)`**。
- 341 顶层 = 338 PASS + 3 既有 SKIP；755 真叶 = 752 PASS + 3 既有 SKIP。0 FAIL、0 未抵达、0 未终态；无 race 告警、panic、timeout。
- 固定源码声明的 341 个顶层与完整 CI 的 341 个 `=== RUN` 精确对应，无缺少或额外；此核对只读源码和日志，没有在本地执行 list/test。
- 原因果集 5 顶层 / 23 真叶在此同头完整 CI **全部 PASS**，逐叶终态见 `retry23-same-head.json`；原首红与六叶修订记录仍保留。

| 包 | 顶层 | 真叶 | 叶 PASS | 既有 SKIP | 真实耗时 |
| --- | ---: | ---: | ---: | ---: | --- |
| internal/ai | 13 | 21 | 20 | 1 | 1.316s |
| internal/ai/siwc | 12 | 34 | 34 | 0 | 7.957s |
| internal/blob | 1 | 1 | 1 | 0 | 1.018s |
| internal/config | 1 | 1 | 1 | 0 | 1.022s |
| internal/connectors | 2 | 2 | 2 | 0 | 1.026s |
| internal/httpapi | 9 | 23 | 23 | 0 | 1.261s |
| internal/memory | 6 | 31 | 31 | 0 | 1.025s |
| internal/notify | 10 | 10 | 10 | 0 | 5.903s |
| internal/postgres | 243 | 570 | 568 | 2 | 412.532s |
| internal/telegram | 36 | 52 | 52 | 0 | 10.562s |
| internal/workspace | 8 | 10 | 10 | 0 | 1.049s |

4 个无测试文件包：`cmd/pcas`、`internal/ai/contextwire`、`internal/testsupport/golden`、`internal/worker`。

### 既有未运行门

- postgres：`TestLiveCodexSecretaryAndLegacyFormats` 缺 dedicated `PCAS_LIVE_CODEX_HOME`；`TestLiveContinuityReplay` 缺真实 embedding / dedicated Codex 配置。共 2 个真实模型 opt-in，逻辑未改。
- ai：`TestInstalledCodexHandshake` 缺可选 `PCAS_TEST_CODEX_BINARY`，共 1 个既有 CLI 协议 opt-in。

因此全包是 **3 SKIP**，postgres 是 **2 SKIP**。未把这些门当作真实模型通过。SIWC 原三 Store 叶已实际通过；其自足 vector setup 原失败日志仍在此前独立报告，本次不覆盖旧红。

## 证据与可复算统计

- `full.log.gz`：完成后一次下载的完整 112526 行 log；解压 SHA256 `98d3ca69d5232a5eabc4160eefd60245fed5bf8ec441f4a2ad5910f465776746`。
- `evidence.tar.gz`：1073 份完整 source-stamped phase2 JSON 合并为一个 `phase2-evidence.json`，每条保原日志起止行、文件、行号、label；包含实际 provider 输入、manifest、ledger、历史与撤权 / 删除 / row-wait 证据，无散文件。原 raw authoritative。
- `run.json` / `version.json` / `summary.json`：GitHub 完成态、精确 checkout/tree、退出及精简终态。
- `counts.json` / `selection-reconciliation.json` / `retry23-same-head.json`：逐包完整 Test 事件、真叶、全部 skip、未终态与声明选择核对。
- `recount.py`：可从解压 log 重算。真叶是同包 terminal Test 没有任何实际执行 Test 以其名字加 `/` 为前缀；排除全部祖先与 package 终态，不重复计父例。此轮各包均非 cache，不存在缓存回放冒称新执行。
- `artifact-sha256.json`：归档各文件 SHA；原 evidence JSON 解压 SHA 另列。

本报告证明上述固定 tree 的完整 Go race / vet / fmt / build 技术门；不替代独立浏览器结果、线上 11 题、真实模型 / 真机、一天用户试用或实际进程崩溃恢复门。
