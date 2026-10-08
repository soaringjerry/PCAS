# T4：独立验证四个已确认用例并汇总

> **Historical task record.** This text records an earlier phase or investigation.
> Use the [current documentation](../../README.md) and [project status](../../status.md) for new work.
> Its old assignments, limits, and precedence statements do not govern current development.

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

## 用户追加 UX 后的并行独立验收

2026-10-01 用户主动询问可否再并行工作。T4 后端已完成，现追加独立前端验收，不与 Opus 改页面：在自有 A1 候选新增 `web/tests/usability-acceptance.spec.ts`，不读 Opus 的新测试来照抄预期；只消费已定产品行为、现有公开类型/测试夹具与可见界面。当前产品写入只归 Opus。

独立验证保存成功/拒绝与重试、快速点击不重复提交、开关只写其对应字段而不改变默认/预算/授权、打开/收起高级配置不产生写入、待确认入口可达且 task/idea/memory 请求目标正确、未知类别不提交、忽略不伪装删除、无假撤销。覆盖桌面及390手机的键盘/焦点/可点击/溢出；不能把桌面模拟声称真实手机软键盘。优先准备契约驱动测试，U2 第一份可构建交接后再作有效性判断；不要把作者尚未完成的中间状态当回归。确认的真实失败交作者，自己不改产品。

独占 `.github/workflows/browser-regression.yml` 的 mock 用例列表，最终追加 U2 `settings-things-ux.spec.ts` 与 T4 `usability-acceptance.spec.ts`，保留既有5文件、runner、超时和业务断言。新专项开发/定位可以局部运行，全套mock和默认真实后端41例仍只在最终组合执行一次。自己的 mock 预览仍用18156；必要进程记录/清理。

U2获准三个真实后端spec的纯展开导航，以及golden对应结果按钮新名称的纯定位更新，T4要独立核对没有弱化服务端/授权/数据/错误断言。最终合入S1报告分支 `review/S1-settings-audit`（最终6ab5c6f，含df285fb）及最新协调docs；都不会改变后端产品。后端210144a的Go验证在后端未变时保留，不重复全量。

## 最终集成的等价测试加载适配

组合 `4aa2746` 的 make check 与完整Go/PG发现 `TestAdoptionMatchesFrontend` 的 Node oracle 报 `summaryInto is not defined`：U2 的实际 adoptAs 引用了真实页面文件内的 summaryInto 常量，旧 harness 只取两个函数体，没有装载新依赖。39个固定业务叶预期不能删或改，不能用测试内重写文案/逻辑代替真实前端代码。

2026-10-01 root授权T4独占 `internal/postgres/auto_adopt_test.go` 该函数的最小加载适配：从实际 ThingPage.tsx 取 summaryInto 声明、只擦除 TypeScript 语法，缺失/边界不符明确失败；把它和实际函数一起交Node执行。产品文件、其他测试和既有断言保持。原作者均已停止，无写入冲突。保留首轮失败与其余全量结果，适配后提交并跑 make check/真实Node等价专项；不为这一个测试加载修改重复已通过的整个PG套件，最终远端CI会执行完整新候选。真实后端浏览器继续执行。

同一最终真实runner的golden F7「正常command改名后撤销更早创建」仍预期旧changed_since与旧文案；实际409 newer_action符合用户已批准的正式契约。Opus已停止，root追加授权T4独占 `web/tests/golden.spec.ts` 该唯一场景的错误码/对应准确文案预期，保留HTTP409、原title不变与所有数据断言，不把正常command替换成未记录外部改动。当前首轮继续保留三次旧预期失败证据，结束后更新并在同样真实fixture补跑该场景三轮；不因局部旧预期修正重跑已通过的其他长等待场景，最终远端CI仍完整执行新候选。
