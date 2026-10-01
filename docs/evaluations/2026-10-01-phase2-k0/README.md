# K0 独立迁移与存储契约验收

2026-10-01。独立验收者 C；工作区 `/tmp/pcas-phase2-c`，新分支 `phase2/k0-acceptance`。本批 **7 顶层 / 35 叶用例全部通过，0 skip，退出 0**；结论限于 022 迁移及直接 SQL 存储行为，不能表示 2.0 运行时通过。

## 精确基线和冻结预期

- 产品 SHA：`97c7081db6b30b6f04ad55c4155a7b5b12f4ae8a`，协调者冻结；迁移只有新增 `022_phase2_context_contract.sql`，历史 001..021 与 d8 相同。
- harness/迁移 gold 冻结提交：`13fff52d60c589fa879e40a9a5b2dc0eff872fb4`。
- `testdata/phase2/k0-migration-gold.json` SHA256：`ddfd8f42410e9143cfedfb5df05e85f774c0cb73387bcb5765e2b38e9a4a965f`。
- 原始 `gold.json` SHA256 仍为 `ff1820de4ef962d82f49f7c204399b8f83362c1aa468ac5017c33cb98e853ec2`；自有 `k0-sequences.json` 与 `lifecycle-supplement.json` 从诊断分支迁入，未改预期。
- 本分支没有迁入 d8 诊断 harness/结果；PR #40 的 d8 5 pass/4 fail 保持独立，不重复运行已完成 9 题。
- 产品、迁移、共享类型均只读。独立测试只新增 `internal/postgres/phase2_k0_migration_test.go` 与自有 fixture/报告。

## 执行与整数证据

专属 loopback 数据库 `phase2_c`（端口 33273，独立 tmpfs 容器），每个顶层测试自行创建唯一 `pcas_phase2_c_*` schema，清理只删除其自有 schema。没有其他 DB/服务、真实模型、私人资料或通知。

一次执行 `go test ./internal/postgres -run '^TestPhase2K0' -count=1 -json`，使用独立 DSN 和 `/tmp/pcas-phase2-c-k0-evidence`；包执行 1.264 秒，退出 0。没有 skip、调低断言、产品修改或碰运气重跑。

| 计数口径 | total | pass | fail | skip |
|---|---:|---:|---:|---:|
| 顶层 | 7 | 7 | 0 | 0 |
| 叶用例（父容器不重复计） | 35 | 35 | 0 | 0 |

叶用例组成：fresh、legacy、无业务 FK、durable 删除四个独立用例；typed kind/version 20；snapshot 7；metadata 4。完整 JSONL 在 `go-test.jsonl.gz`，全部捕获在 `capture.tar.gz`；`capture-index.json` 保存每个合成产物的字节数与 SHA256；`results.json` 保存完整结束事件。

## 已证明的存储行为

| 项目 | 输入与直接观测 | 结果 |
|---|---|---|
| Fresh Migrate / 再次调用 | 22 条 applied migration，0 source policy、0 memory job；CheckSchema 成功；再次 Migrate 的 checksum/applied_at 账本完全相等 | PASS |
| 真实旧 021→022 | 先仅应用历史 001..021，确认两 kind 列均不存在；source@2（含 v1 历史正文）、claim@3、summary@1 分别建立 run 与 derived 依赖 | PASS |
| Legacy typed 回填 | 3 条 run 与 3 条 derived 共 6 条，ID/version 未变，kind 分别为真实 source/claim/summary；不能全填 claim | PASS |
| Legacy 内容/权限保留 | 16 个相关表的旧列 SQL JSON 前后相等，包括 source 两版本正文、claim/summary、record identity、workspace/run、grants；旧 source grant 没有产生任何精确授权 policy；再次 Migrate 行与账本未变 | PASS |
| 新 typed dependency | attempt 与 durable 两表各接受 source/claim/summary@2 并 round-trip；各拒绝 chunk/entity/episode/relation/unknown 与 0/-1 version（共 14 拒绝）；拒绝 SQLSTATE=23514 | PASS |
| Snapshot 边界 | 262144 bytes 正文逐字 round-trip；262145 拒绝；snapshot_bytes/实际 bytea/input_bytes 不一致拒绝；非 retained 带正文、retained 缺正文、到期顺序颠倒均拒绝 | PASS |
| Metadata 声明边界 | 声明 metadata_bytes=65536 接受，65537 拒绝；manifest 自身超过 65536 拒绝；声明少于 manifest+recipient JSON 字节拒绝 | PASS（有限 DDL 检查） |
| Attempt 独立写 | workspace owner=0、memory record=0 时仍可写 attempt=1 与 source dep=1；诊断表业务 FK=0，仅其子依赖引用 attempt | PASS |
| Durable 不随诊断删除 | 删除 attempt 后 attempt=0、诊断 deps=0、durable deps=1；再删除对应 memory identity 后 durable typed 行逐字段不变 | PASS |

Legacy 输入是独立 gold 的合成资料，经真实旧表约束建立，不是从 022 输出生成答案，也不是先应用 022 再删列伪造升级。旧列前后比较排除的仅是两个新 kind 列。

## 尚未验证的运行时限制

本批 metadata 测试只证明 DDL 的声明上界和 manifest+recipient 下界。DDL 无法证明 metadata_bytes 是否准确计算所有可变 attempt 字段与旁表依赖。依赖旁表可继续新增行、自由文本字段尚靠运行时受控 code 守门；这些必须由 K1 计量、裁剪/拒绝与诊断 gate 闭合，不能称已证明整个 metadata 真实总量上限。

owner 正文 64MiB、metadata 64MiB、10000 骨架并取严的原子累加；单条 metadata 64KiB 的全部可变项计量；保留期实际清扫、启动恢复、读时过期检查；quota 耗尽时外发前拒绝；自由 gaps/reason 不藏正文——均待 K1 动态验收。允许一条虚报较大声明值只是在测声明结构，不说明运行时计量可靠。

本批没有调用自然/公开 source 授权、非 owner missing TrustedTask gates、typed runtime verifier、摘要递归、在途 provider fencing、纠正/删除/撤权、manual 重新获取/提交、回滚/崩溃 attempt、undo 或结果采纳。unknown kind 的 **SQL 拒绝** 通过不代表所有消费者已经拒绝未知/缺 kind；旧 nullable kind 的 runtime fail closed 仍待 K1。直接 DELETE 验证依赖不 cascade，不代表产品 source DELETE 已清所有产物正文。

后续 `023/ExplicitDeny` 是协调者在本次 022 基线冻结后批准的 undo 语义澄清，独立预期见 `testdata/phase2/authorization-undo-gold.json`：undo 首次 grant 回到 raw 不可读而独立 claim 可读；undo explicit revoke 可恢复当前 allow，但旧 attempt/result 不恢复。该 fixture 未被本次 022 测试消费，也未改既有 explicit deny 预期。

## 阶段门槛

d8 三 raw 入口/自然授权的失败仍以 PR #40 报告为准。K0 的迁移/DDL 绿色不补足 K1/K2 消费与生命周期，更不补足线上 11/11、真机、用户一天试用、真实模型、成都黄金路径或连续任务/成本收益；这些均未获完成证据。
