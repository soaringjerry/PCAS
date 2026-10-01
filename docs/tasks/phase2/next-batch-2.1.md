# Phase 2.1 下一批拆分提案

2026-10-01，静态基线 `a51fdef8670fb6152ac8ca290e69390f496edf33`。A 只读研究，root 收敛为下批任务入口；未在此批实现、编写测试或调用真实模型。这不是新契约已批准，也不表示 2.0 线上/真机/试用门已通过。预算、规模和效果阈值仍需独立基线校准。

## 复用与缺口

继续共用原件、claim、向量、episode/graph 的身份、版本与 typed 证据，复用 ChatGPT ZIP/JSON mapping parser，不另造工作室或导入记忆库。

| 当前代码 | 静态发现；不是新动态结论 |
| --- | --- |
| `internal/postgres/claims.go` createRecord | claim 创建未写表达时间；现抽取 statement 缺少有证据的经历时间/地点字段。 |
| `internal/postgres/retrieval.go` | Recall 有 ValidAt/KnownAt 时点，未提供表达年区间规划；raw 与 claim 共用排序和预算，尚无结构化非零也补原文遗漏、再追查后续状态的完整流程。 |
| `internal/postgres/desk_turn.go` secretaryCardsTx | 现时间轴按来源记录时间，状态按同 source 任取 work_item；不能直接声称是该计划的表达时间或精确当前状态。 |
| `internal/postgres/processing.go` | 旧 active 用户待办的自动采纳及条件唤醒缺少历史迁入用途隔离，属于静态可达风险，尚未动态复现。抽取后台以 owner 读取后调用模型，须明确整理接收者和邻文许可，不把 owner 查看权自动当作模型授权。 |

## 先冻结共享契约

- 区分表达时间、事实有效时间和系统得知时间；未知保留未知，导入日期不能代替原表达日期，晚到旧记录不能覆盖更新状态。
- 保留发言者、分支、原始出处及逐字证据；AI 建议、用户考虑、明确决定、否定、引用他人分别处理。
- 历史迁入默认补记忆，AutoAccept/WakeIdeas 开启也不能把旧计划变成当前事项、提醒或唤醒；当前用户明确恢复安排才走正常动作。
- 回忆的“去年”限制要能查到今年的取消/完成；状态变化须关联具体意向，无法确认就不填“仍有效”。
- 共用 2.0 权限、版本及最终输入记录；派生检索词仍遵守 embedding 独立接收者边界。后台整理接收者和预算拒发条件须明确。

## 最多三个任务包

以下是待分派范围；正式执行前由 root 为精确组合 SHA 冻结唯一写入者，跨文件交接先记录，不默认允许同时修改共享类型或 processing.go。

| 任务 | 目标与候选可修改文件 | 依赖与独立验收 | 交付物 |
| --- | --- | --- | --- |
| 2.1-A 证据化抽取与用途隔离 | `processing.go`、`claims.go`、`source_context.go`；必要 memory/connectors 类型、`connectors.go`、`jobs.go`、单一迁移。沿用 quote 验证和版本 fencing，补最小表达时间与有证据的关联。 | 先冻结上述契约。独立 gold 覆盖建议≠决定、否定/引用/考虑、晚到资料、迟到 worker、AI/用户角色；历史 active 用户计划在 AutoAccept/WakeIdeas 开启时仍零当前动作、零唤醒。 | 小契约/迁移/实现提交；独立原始输入与实际外发、模型调用数、动作数和失败证据。 |
| 2.1-B 共用回忆规划与成都时间轴 | 新共享 planner，`retrieval.go`、`graph.go`、必要 expand 接点；`desk_turn.go`、`desk.go`、`run_context.go`接同一规划；必要卡片类型和 `SecretaryCards.tsx` 最小适配。 | 依赖 A 最小字段和用途冻结，raw 补查不等全库抽取。覆盖成都/春熙路、pending/遗漏第二事项、今年取消去年计划、他人同名意愿、纠错删除撤权；三真实入口分别记录检索、实际输入、引用与间接依赖。 | 共用 planner、计划/变化/当前状态卡片或时间线、可展开原话；独立合成与小样本结果，未知和缺口明确呈现。 |
| 2.1-C ChatGPT 迁入闭环 | 复用 `connectors/archive.go`；必要 `connectors.go`、`attachments.go`、`httpapi/connectors.go`、连接类型、`ConnectorSettings.tsx`。收费整理接点从 A 串行交接。 | A/B 记忆基本链路通过后接入。预览→原文保存→预算内整理→进度/暂停/失败重试；分支/角色/表达日/附件缺口；同档改名/更新档、纠正删除后重导不复活、未抽取原文仍可查、未开始整理模型调用为零、大文件受界限控制。 | 可审查导入 PR、合成 fixture、小批样本验收及费用记录；完整私人历史仍等导入闭环通过后再迁入。 |

两个后端执行者可以在共同类型冻结后按上述文件拆分并行，独立验收者先冻结 gold；UI 审美收尾才考虑本机真实 Claude Code 的有限 Opus 入口。主协调者不写产品或测试。此提案未调用 Opus/Astra，也不承诺新召回率、P95、真实费用或连续任务收益。

2.2 再用同模型、同预算与简单混合检索基线比较连续任务，记录重复背景、纠错、完成时间和导入/维护/调用/存储成本；不能用单轮问答或来源召回替代。
