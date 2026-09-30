# 任务 D1：副手结果自动采纳

执行者：**6.1 Sol** · 分支：`phase1/D1-auto-adopt` · 依赖：**B1 已合并**（需要用到它的动作记录收集器）
开工前读：[README.md](README.md)、[contracts.md](contracts.md) 第 1、5 节。

**现状**：副手做完之后，结果停在卡片上，要用户手动点「加进子任务 / 存成文档 / 写进进度」。
**目标**：结果直接放到该去的地方，同时可以撤销。这是白皮书「先做后报」原则在副手这里的落实。

## 要做什么

1. **去向判断移到服务端**：新增 `adoptionFor(item, run, output) string`，返回 `subtasks`、`progress` 或 `doc`。规则和前端现有逻辑完全一致：
   - `web/src/domain/agent.ts` 的 `parseChecklist`：匹配形如 `- [ ] 文本` 或 `- [x] 文本` 的行，能解析出至少一行就是 `subtasks`；
   - `web/src/pages/ThingPage.tsx` 的 `adoptAs`：`run.kind == "summary"` 是 `progress`，其他情况是 `doc`。

   用表驱动测试确认 Go 版和前端版对同一批样例给出相同结果，样例至少包括：纯清单、清单夹在正文里、summary、普通长文、空行、`- [x]`。

2. **自动采纳**：在 `internal/postgres/runs.go` 里，run 变为 `done` 的两个位置：
   - worker 完成（`runAgentOnce` 里设置 `current.Status = "done"` 的地方）；
   - 手动交接贴回结果（`pasteRunResult`）。

   在同一个事务里：
   - 如果 `run.StaleContext` 为 true，或者输出为空，就不自动采纳，保持原样；
   - 否则挂上动作记录收集器（`source = "worker"`，id 为新生成的 UUID，summary 为「副手结果：加了 N 个子任务」「副手结果：存成文档」或「副手结果：写进进度」），然后复用现有的 `adoptRun` 分支逻辑来执行，**不要复制一份**。可以把 `adoptRun` 的实际逻辑抽成一个函数，让命令和自动采纳都调用它。
   - `run.Adopted = { as, at, edited: false, auto: true, actionId }`。

3. **类型**：`internal/workspace/model.go` 的 `Adoption` 加两个字段：`ActionID string \`json:"actionId,omitempty"\`` 和 `Auto bool \`json:"auto,omitempty"\``。

4. **手动 `adoptRun` 保持可用**：用户撤销自动采纳之后，还可以手动再采纳一次，D2 会把这个做成「放回去」。手动采纳时 `auto` 为 false，`actionId` 等于该命令的 `requestId`（B1 已经为 `adoptRun` 记录了动作）。

5. **训练样本**：现在 `adoptRun` 会写训练样本，自动采纳也照原样写，不另外加逻辑。样本以后怎么用，第 6 阶段再定。

## 不要做

- 不要改前端（D2 负责）。
- 不要改 run 的执行逻辑、预算、上下文校验。

## 验收（集成测试 `internal/postgres/auto_adopt_test.go`，用 httptest 假模型）

- [ ] 任务上的 run 输出 3 行清单：完成后任务多了 3 个子任务，`run.adopted.auto` 为 true，`actionId` 不为空。
- [ ] 项目上 `kind = summary` 的 run：完成后写进了项目进度。
- [ ] 普通输出：生成一篇 `by = "ai"` 的文档。
- [ ] `staleContext` 为 true 的 run 完成后不会被自动采纳。
- [ ] `undoAction { id: actionId }` 之后，子任务、文档或进度都被恢复，run 回到「已完成、未采纳」；再手动 `adoptRun` 能成功。
- [ ] 手动交接的流程：`pasteRunResult` 之后同样被自动采纳。
- [ ] 采纳之后用户又改了事项，这时撤销返回 `changed_since`。
- [ ] 原有的 run、采纳、所有权块相关测试（`artifacts_test.go`、`ux_regressions_test.go` 等）全部通过。
- [ ] `make check` 和 `make test-integration` 通过。
