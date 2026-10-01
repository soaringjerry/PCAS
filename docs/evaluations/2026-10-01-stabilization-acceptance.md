# A1 稳定化集成审查与候选验收

2026-10-01；执行者 Astra / high。U1及后续浏览器夹具修复已交接，一次最终候选完整隔离检查已结束。下列静态分析、U1截图与本次实测分开记录。**隔离集成检查通过，但阶段验收仍有缺口：4个U叶pending、三轮发送顺序风险与真实线上/一天试用尚未验收。第二阶段没有开始产品实现。**

## 候选与范围

独立工作区 `/root/PCAS-wt/A1`、分支 `stabilization/acceptance-candidate`。从 F11 `e5841a2abc01319ffe3e06c08953ae20976c8665` 开始，依次无冲突合并 F12 `c1a0173629568c4588ed1f942af28e6010dd4f04`、F8 `2e6ebc1abe4fde92a7ceabdc41b7704ba56ac5de`、M0 `21faec7ff2019b2722e660751231ae7af92ba832`，初始集成 SHA `43192e4793d3a6c192f51a3cd97d11aef5c40ea9`。随后无冲突合入 F11 最终 `d38a0b4f375d0e2acc951de124554320ec1a9f45`、U1 `b0386f9b8186b6a87e5b19cb7c5a710b640626f3` 与 F12 最终 `ae9f69b3611e0600e7fce97a404e8f043f569958`，完整检查的产品候选为 **`7aae834d34b3749bdceaf0a2b704b79e837a391e`**。确认 G7 实际 Telegram messageId 与 G9 超时文案两处夹具修复同时保留。F7/F9/F10/T1/T2/T3 已在这些祖先中，不重复挑选提交。

F11 后续将 G9 旧 generic 文案更新为已批准的超时文案，原话保存/来源查询和三轮重复保留；F12 后续为假 Bot API补 getMe 的真实 User/is_bot 结构及送达 messageId，G7 使用服务实际返回的ID。这两项是既有真实后端夹具与已修产品契约对齐，不在A1再记为产品缺陷。

产品与共享测试只读；A1 仅写本报告，不改协调任务文档、不合并 main、不部署、不调用真实模型、Telegram 或通知。只允许候选分支推送及面向 main 的 draft PR。

## 按影响排序的待决与限制

### A1-L1：同一网页会话三轮连续修正可能按不同于发送的顺序执行

**性质：中等产品顺序风险，静态推断，未得到三轮业务结果失败的实测证据。** 正式契约说同一 conversationId 串行、后一轮看到前一轮结果；B2 另要求输入永远可用且各轮按发送顺序排列。后者明确约束展示，不直接规定跨实例执行协议；“后一轮”的执行顺序解释仍须协调裁定。

`web/src/components/Secretary.tsx:30` 的 deliver 每个请求立即 POST，`:256` 的 send 先按发送顺序追加显示再调用 deliver。`internal/postgres/desk_turn.go:504` 的锁保护完整上下文读取、生成和提交；同会话 try-lock 未得到锁时，事务归还连接与 slot，25ms 后重新竞争，没有先到先得队列。

具体触发条件：A“建15点会议”已进入模型；B“改成16点”和 C“再改成17点”先后发送并等待 A。A 提交后，C 若先获得锁，B 最后执行可把时间改回16点，而当前页面仍显示 A/B/C。F11 已披露非 FIFO；本项说明该限制与当前前端的实际交叉影响，不将已披露限制升级为高危安全问题。

现有 `TestStabilizationS1_ConcurrentConversationSeesPreviousObject` 用 barrier 确定 A 已开始，验证 B 等待并引用 A；跨 Store 的16等待者测试验证隔离、UUID 大小写及排空，所有排队输入均为 queued/创建动作，不检验修正顺序。因此其绿色不证明三轮按发送顺序执行。若要修复，先由 Sol 用独立预期复现，再收敛同一网页会话的递交、失败与重试顺序；不以本报告扩写跨客户端持久队列协议。

### A1-L2：事项页100px回执留白只覆盖空或单行草稿

**性质：低等级的未覆盖交互边界，静态审查，不是已实测失败。** U1 为事项页 `.sec-turn` 加100px滚动留白；截图和新增桌面/390px测试显示空输入时回执及撤销按钮在输入框上方。`secretary.css:534` 同时允许 textarea 长到200px，用户可在前一轮生成期间继续写多行草稿。U1报告“发送清空所以回执到达时单行”的理由不能推广到此情境；软键盘和真机亦未覆盖。后续最小范围是验证等待时续写多行草稿的点击命中与遮挡，确认问题后再决定是否随输入实际高度计算留白。

### A1-P1：四个 U 叶及真实使用入口尚未完成

U3、U4-user、U5 的 `newer_action`/`changed_since` 错误码契约冲突，以及 U10 当前轮新建对象引用协议空缺，仍为 pending。不得删 skip、降低随机序列预期或称阶段零 skip。现有50种子×20操作只是已明确契约的逆序回归。

线上11项、真实默认 Codex 通道、实际工作区时区、Telegram/手机录音、真机推送与用户试用一天没有本次证据。即使隔离检查通过，也不能宣布第1.5阶段完成或启动第二阶段产品实现。

### 已披露且不作新缺陷的边界

- F11 每 Store 最多9个 DeskTurn 事务，为 Recall/预算保留1条连接。覆盖本路径和跨 Store 互斥，不等于证明旧 AnswerDesk/任意 worker 混合占满连接时都无等待。
- F12 `sendMessage` 成功到本地关联/offset 落盘之间崩溃可能重发外部消息；业务 request 幂等与外部消息 exactly-once 不同。旧按钮没有可信关联时失效；旧格式请求仅能在首次确认时保存的 conversation 锚点内迁移。
- 同 bot token 轮换保留 bot 身份业务键及可验证回执；真正换 bot 清关联，返回旧 bot 可读原业务结果，不能恢复已清旧按钮关联。最近30天关联量随投递量增长，未做真实大流量负载验收。
- F7 迁移前只有旧全文 afterHash、没有 after snapshot 的已失配历史动作，不能承诺自动修复。

## 关键静态审查

| 边界 | 静态结论与证据 | 实测状态 |
|---|---|---|
| F11 锁与容量 | request/conversation 两把 PG 事务锁均取得后才读上下文；失败归还连接、取消退出；UUID 键转小写；整轮提交或回滚释放。首轮开始后下一轮不会同时生成。 | 本次DB通过 |
| F12 新增读取与历史一致性 | `telegram_turn.go` 限定 owner、验证 request UUID，使用与 DeskTurns 相同的依赖 verifyRunTx/erased 提示及卡片清理，刷新实时 undone；读取现存 response，不新增正文副本。 | 本次DB通过 |
| 删除与回调 | 现存删除闭包清 question/answer、text/reply/cards/ask 和收据正文；F12 送达绑定含 message/request/conversation/turn，无正文，回调重读并核对绑定和动作所属。D1 加强了真实 sources 卡及 dependencies 断言，并含生成中删除。 | 本次DB通过；原 D1 坏夹具 finding 已撤回 |
| F9 抑制与撤销链 | suppression 保存在既有 occurrence 账本，不改恢复的业务文档，因此不破坏内容指纹；新占位隐于 Notice/Job/Activity，已有真实记录只合并抑制标志，保留发送和关闭历史；新 occurrence 不继承旧抑制。 | 本次DB通过 |
| F10 过期优先级 | action_log 先判断 undone 再判断显式 expired/严格大于30天；精确30天可撤销。边界测试同一事务固定 DB now，避免睡眠误差。 | 本次DB通过 |
| F8 fixture 生命周期 | timezone-backend 的 finally 仅完成自己记录的 task/project，项目退出 active P1，恢复 followUps；历史回执文字保持原文、实时卡片随工作区时区改变。 | 本次真实runner 41/41通过 |
| U1 | 审阅全部五个产品文件差异及手机事项回执、手机多回执、桌面时区提示截图；等待行保留，历史回执仍原文，时区切换按钮/返回路径不变。详见A1-L2未覆盖情境。 | 62项mock通过；截图为U1交付证据 |
| M0 | 统一规划/数据用途治理/证据包/评测建议明确待审，无实现或新指标实测。 | 文档静态审查 |

## 集成检查记录

检查均针对产品候选 `7aae834d34b3749bdceaf0a2b704b79e837a391e`，未设置 finding/repro 开关。用 make check 的三个非测试目标加明确禁缓存的完整 Go race 命令执行同等检查，不声称另跑 `make test-integration`：

```sh
make fmt-check lint build
PCAS_TEST_DATABASE_URL='postgres://postgres:test@127.0.0.1:33262/postgres?sslmode=disable' \
  go test -race -count=1 -json ./cmd/... ./internal/...
# web目录，PATH首项为 /root/.nvm/versions/node/v22.23.3/bin
npm run lint
npm run type-check
npm run build
PCAS_TEST_BASE_URL=http://127.0.0.1:18144 npx playwright test \
  tests/secretary.spec.ts tests/fixes.spec.ts tests/notify.spec.ts \
  tests/buttons.spec.ts tests/timezone.spec.ts --output=test-results/A1-mock
PCAS_GOLDEN_PORT=18145 bash /tmp/pcas-test-A1/real-backend.sh
```

最后一条为仓库既有 default runner 的临时副本，仅替换工作目录、容器名前缀 A1、OAuth callback端口18144；原测试文件、执行顺序、golden三次重复及断言全部保留。先结束mock预览才使用callback端口。外部模型/通知/Telegram均由既有合成服务应答。

| 检查 | 本次结果 |
|---|---|
| Go fmt/vet/build | 退出0 |
| 全包Go race，带隔离DB且-count=1 | 退出0；11个有测试包全部通过，PostgreSQL127.322s，Telegram7.041s，notify4.772s |
| 编号及随机序列 | R系列21个顶层通过；PG S/D18个顶层通过，另有Telegram S6通过；Telegram T系列7个顶层通过；U16全部50种子×20操作通过。4个U叶pending仍保留 |
| Go skip口径 | 580个pass测试/子测试事件，7个skip事件；事件含父测试及子测试，不能当580个独立场景。4个pending与3个可选live测试明确分列如下 |
| 前端lint/type-check/build | 全部退出0；Node22.23.3，独有依赖 |
| 5文件mock浏览器 | 62/62通过，45.6s，零skip |
| 真实后端default runner | 退出0，41/41通过，零skip、零flaky、零失败：timezone-backend 1/1（4.924s），golden默认三轮36/36（625.245s），backend/continuity/chatgpt-direct/model-api 4/4（8.550s） |

7个skip的实际名称：

- **待决4叶**：`TestStabilizationUndoU3_SkipLaterEditPendingCode`、`TestStabilizationUndoU4_UserAndBackendEditPendingCode/user-command`、`TestStabilizationUndoU5_TwoEditsPendingCode`、`TestStabilizationUndoU10_ExistingItemSameTurnPartialCoverage`。相关安全断言执行，完整契约未验收。
- **可选真实环境3项**：`TestInstalledCodexHandshake`（未授权调用本机真实CLI）、`TestLiveCodexSecretaryAndLegacyFormats`（需专用PCAS_LIVE_CODEX_HOME）、`TestLiveContinuityReplay`（需专用CLI与PCAS_LIVE_EMBEDDING_URL）。这些不计作通过或用fake替代。

本机原始日志保留于 `/tmp/pcas-test-A1/go-check.log`、`go-race.jsonl`、`go-race.stderr`、`mock.log`、`real-backend.log`。运行结果与skip清单写入此版本化报告；浏览器结构化JSON/trace/服务日志保留在本工作区web/test-results。

## 第二阶段可并行准备与入口

可以审阅 M0、收敛主体/时间/范围/行动状态契约，设计带干扰资料和人工预期、冻结评测集及成本计量口径；这些是设计准备。正式实现仍等待稳定化序列全部通过、线上11/11、一天实际试用没有阻碍使用的问题。当前结论为“仍有缺口”。

## 资源与最终交付

Go使用自建 `pcas-test-A1-integration`、pgvector PostgreSQL16、1GiB tmpfs，动态localhost端口33262；全部包完成后先stop再rm -v，已确认不存在。浏览器runner使用自有 `pcas-test-A1-browser-4015899`、1GiB tmpfs、动态localhost端口33263，HTTP 18145、callback 18144；runner退出0后按自身PID列表终止fixture/serve/worker并删除自己的容器与临时目录。mock的18144预览提前按记录的精确PID停止。最终docker查询无A1容器，18144/18145无监听。

保留本工作区独有node_modules、构建产物及测试证据供审查；没有清理其他任务资源。没有连接真实账号、生产、真实通知，没有合并main或部署。

本报告是A1唯一新增版本化文件，最终报告提交位于已测试产品候选之上；报告不改产品或测试，因此产品树仍对应 `7aae834d34b3749bdceaf0a2b704b79e837a391e`。分支推送及面向main的draft汇总PR由交付消息记录。远端新汇总PR的CI状态须按其head SHA另查，本次本地绿色不冒称远端已通过。
