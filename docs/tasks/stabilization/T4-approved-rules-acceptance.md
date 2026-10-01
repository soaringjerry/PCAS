# T4：独立验证四个已确认用例并汇总

执行者 Sol / high，不写产品实现。用户 2026-10-01 已确认撤销从最后一步往回退，以及同轮新建后继续加步骤。此前 U3、U4-user、U5、U10 从待裁定转为待实现/验证，不是直接标为通过。

基线 `59e25daa67df7447f0a09b7f0bd9e5d65edab65e`；工作区 `/root/PCAS-wt/T4`，分支 `stabilization/T4-approved-rules`。先读协调者工作区 [正式契约](../phase1/contracts.md) 与 F13/F14 任务。本轮新引用编码由协调者在 F14 技术审查后固定，未收到固定协议前只做已明确的撤销测试和验收准备。

## 独立覆盖

- 独占 `internal/postgres/stabilization_undo_test.go`，恢复 U3/U4-user/U5 的确定断言，未记录外部修改仍为 changed_since。保留 50 种子 × 20 操作及全部原业务状态断言。
- 用真实 DeskTurn 入口测试 U10：假模型输出新建任务 + 引用刚建任务加步骤；先撤销新建得到 newer_action，再撤销步骤、撤销创建成功。不可用既有 THIS 替代本用例；既有部分覆盖保留成单独回归。
- 可新增 `internal/postgres/approved_rules_acceptance_test.go` 验证多对象、失败绑定、别名隔离、重放和下一轮使用。按契约写 oracle，不读算法反推断言。F13/F14 各自的实现自测不能代替此任务。
- 发现真实失败先保留证据交作者，不擅自降预期或改产品。旧报告保留历史事实，新报告解释关闭了哪些待决项。

## 汇总与验证

先交独立测试提交，等待 F13/F14 最终交接。协调者明确集成输入后，可接手 `/root/PCAS-wt/A1` / `stabilization/acceptance-candidate`，无冲突合入 F13/F14/T4 和协调契约文档；发生冲突先报告，产品冲突由所属作者处理。不可合入 main。更新现有 Draft PR #32 描述/报告，不重复开汇总 PR。最终报告新增 `docs/evaluations/2026-10-01-approved-rules-acceptance.md`；A1 旧报告仅可补一段指向新证据的历史说明。

最终产品候选跑 make check、完整隔离数据库 race 集成、前端检查、62 个 mock 与默认真实后端浏览器 runner；已跑且候选未变的检查不反复重跑。记录准确 SHA、命令、失败/skip/计数。四个 U pending 必须归零；三项可选真实模型测试没有授权，继续明确未运行。线上 11 项、真机、一天试用不因假模型全通过而宣称完成。

隔离 tmpfs PostgreSQL、假模型/通知，Docker 分配数据库端口；HTTP 如需要用 18156/18157。只清理自己的资源。不访问生产/真实账户、秘密，不部署、不开正式二阶段实现、不合 main。推自己的测试分支和获指定的候选分支，更新 Draft PR #32 后交还所有写入权。
