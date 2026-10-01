# 组合产品：正式动作字段谱系验收执行前冻结

协调者指定产品 `9d60f310beec1748b4e99ed22a3e86b4493cd1e9`，含 A origin+025 产品 `8d2dc4d`、C迁移配套，以及 D harness 636e0d1/4ccb05b 的组合对应提交（当前测试文件最后提交 `0dcee75`）。隔离 worktree `/tmp/pcas-phase2-action-lineage-formal`，branch `phase2/action-lineage-formal`；原 UI WIP 留 `/tmp/pcas-phase2-action-lineage-repro`，本批不执行新 UI 闭环。

冻结 exact SHA256：

- `internal/postgres/phase2_action_lineage_test.go`：`12c77017ae46479d8908fc24e100c28871468c70b2355ecd2382c71322398e21`，与独立 harness 4ccb05b 一致；原最小红 test body 不改。
- `testdata/phase2/gold.json`：`ff1820de4ef962d82f49f7c204399b8f83362c1aa468ac5017c33cb98e853ec2`。
- `testdata/phase2/runtime-sequences.json`：`716398ebea51811ae6a620add0625028c9be4e7522d22e1408c92830f88fb29d`。
- 只读复用 `internal/postgres/phase2_runtime_helpers_test.go`：`62cf450e45585d64f9968a59ea505ee553f5b86f3f0fc307688cb9420ca74653`。

正式执行一次 `go test ./internal/postgres -run '^TestPhase2ActionLineage(SecretaryOnlyFieldsDoNotReachManual|Matrix)$' -count=1 -v`。预期两顶层、13叶（原红复验1 + Matrix12）；记录实际抵达/成功/失败/未执行/退出与原日志，不 skip、重试或偶然重跑。

只使用已指定合成 loopback PostgreSQL `127.0.0.1:33273/phase2_c`；每叶沿真实 testStore 己唯一 schema/create/drop。模型 HTTP 用 phase2RTSetup 本地 httptest，不生产/账号/部署。输入、owner state、真实 package、attempt/manifest、original model HTTP 保存至本命名前缀 evidence 目录；不把 manual PCAS delivery 声称第三方已收到。

冻结的语义预期见 matrix-expectations；首先按当前 strong assertions 运行，不为预知实现结果调整。任何首次失败先报告责任/因果，产品修复交 A，harness 问题需说明且获协调者批准后再处理。本批不是全 PG 回归，也不是新 manual UI 真实后端验收。
