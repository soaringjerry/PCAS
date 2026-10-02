# 任务 T：独立验收

执行者 6.1 Sol / high，**不能是做 A 或 B 的那个执行者**。分支 `phase2/b1-T-acceptance`，工作区 `/root/PCAS-wt/b1-T`，PR base `phase2/batch1`。

先读 [本批总览与契约](README.md)。你的依据只有总览里的规则（第 3 节）和操作序列（第 4 节）。**不要读 A、B、C 的实现来决定预期**；预期从契约来。原因见 [任务流程规则](../../process.md) 第 2 条：实现和测试照着同一份代码写，会有同样的盲区。上一次失败的经过见 [回滚记录](../../../evaluations/2026-10-01-phase2-0-rollback.md)。

## 要交付什么

总览第 4 节的 38 条序列（P1–P15、M1–M8、N1–N12、G1–G3），每条至少一个测试，测试名带编号。再加两组浏览器用例。最后一份验收报告。

## 你独占的文件

- `internal/postgres/phase2_b1_supply_test.go`、`phase2_b1_outdated_test.go`、`phase2_b1_undo_memory_test.go`、`phase2_b1_migration_test.go`、需要的 `phase2_b1_helpers_test.go`；
- `testdata/phase2/`（预期数据、迁移测试用的 2.0 建表语句副本）；
- `web/tests/phase2-batch1.spec.ts`（模拟后端）、`web/tests/phase2-batch1-backend.spec.ts`（真实后端）；
- `web/tests/support/` 和 CI 工作流里为了把这两个新用例跑起来必须加的那几行；
- `docs/evaluations/` 下本批的验收报告。

不要动任何产品代码。发现产品有问题，写进发现清单交给协调者。

## 顺序

1. **先冻结预期。** 在读任何实现之前，把合成资料和每条序列的预期写进 `testdata/phase2/b1-gold.json` 并提交。预期用「必须出现的片段」和「不许出现的片段」来写，例如 P1 的必须片段是那句话里的几个细节词。这个提交之后预期只增不改；确实写错了要改，先找协调者。
2. 写测试。此时 A、B、C 可能还没合进来，你的分支会是红的，这是正常的。
3. 协调者通知集成分支就绪后，把分支变基到 `phase2/batch1` 上，跑全部用例，写报告。

## 怎么测

**走真实的入口。** 不直接调内部函数来代替：

- 秘书：`Store.DeskTurn`，模型用 `httptest` 起的假服务，断言**假服务实际收到的请求正文**里有没有那些片段。`internal/postgres/desk_turn_test.go` 里的 `secretaryModel`、`secretaryModelReply`、`turnRequest`、`mustTurn` 可以直接用。
- 副手：`workspaceCommand` 发 `requestRun`，再让 `RunAgents` 或 `runAgentOnce` 实际把请求发给假服务，断言收到的正文。
- 手动转交：`requestRun` 给 `manual` 副手，断言返回状态里那次运行的 `brief`。
- 撤销：`workspaceCommand` 发 `undoAction`，或 `Store.Undo`；Telegram 那条走 `internal/telegram` 里现有的回调测试方式。
- 抽取：`mustIngest` 之后用 `leaseStage` 领任务、调 `ProcessExtraction`，抽取模型同样是假服务。
- 迁移：在一个空的测试库里先执行 2.0 的四个建表文件（从标签 `archive/phase2-0/merged` 的 `internal/postgres/migrations/022`–`025` 拷到 `testdata/phase2/leftover/`），往旧表里插几行别的数据作对照，再跑 `Migrate`。

2.0 时期有一套同样思路的捕获测试，在标签 `archive/phase2-0/independent-acceptance` 的 `internal/postgres/phase2_input_acceptance_test.go`，可以参考它抓请求正文的写法。它的预期里有「授权」相关的部分，那些已经作废，不要搬。

**用真实数据的样子。** 线上的资料绝大多数标题相同（十几份都叫「秘书原话」，好几份都叫「快速记录」），后台常常一条记忆也没抽出来。夹具要照这个样子造：多份同名资料、零陈述。2.0 的验收用了一份标题唯一的资料，所以全绿却不能用。

**每条序列的正反两面都要断言。** 例如 P1 不只断言原话出现了，还要有一条对照：把那份资料删掉之后同样的提问收不到它。N1 不只断言记忆被删了，还要断言原话资料和对话记录都还在。

**假模型的回答由你写死。** 需要模型「引用了 S1」时，假服务直接返回 `{"reply":"…","used":["S1"],…}`。这只能证明系统把东西交到了模型手里、并且正确处理了模型的引用，不能证明真实模型会怎么答；后者由协调者部署后在线上验证，报告里写明这个边界。

## 浏览器用例

- **模拟后端**（`phase2-batch1.spec.ts`）：依据卡片里的原话条目能看到、能点开原文（R8a：点开后不用再点就能看到原文全文，被引用的那一段有标记；面板上没有「第 N 版」和「摘要」字样；从资料库打开同一份资料时面板和现在一样）；带 `outdated` 的轮次显示「依据已更新」且回答、卡片、回执都在，【撤销】可点；不带的轮次没有这个标记；390px 宽度下不溢出。
- **真实后端**（`phase2-batch1-backend.spec.ts`）：在页面上对秘书说一句带细节的话，新开一段对话问细节，断言假模型收到的请求里有那句原话，页面上的依据卡片能点开原文。再加一条：上传或速记一份**带换行、超过 600 个字符**的长资料，问一个答案在资料中段的问题，点开依据卡片里的原话，断言面板里被引用的那一段有标记（`mark`）并且在可见区域内。这条用来确认后端给出的摘录能在原文里原样找到（前端只做一字不差的匹配）；找不到时面板从头显示、没有标记，那就是一条发现，写明摘录和原文差在哪。按 `web/tests/golden.spec.ts` 的方式用 `fixture` 控制假模型。日期相关的断言不能依赖今天是星期几。

## 纪律

- 失败的用例就是失败。不许 `t.Skip`，不许放宽断言，不许同一份代码反复重跑碰运气。
- 不改已有测试的预期。协调者批准要改的，会单独交给你，改了什么在报告里逐条写明。
- 测试库用自己起的临时 PostgreSQL，自己清理；不碰线上库和线上配置。

## 协调者已批准的旧测试预期变更

任务 A 合入后（集成分支 `30efc04`），`make check` 有三组旧测试因为本批规则而失败。下面两组已批准，由你改，改动只限于列出的断言，测试原来保护的意图要留着。第三组不用改：用户已决定 R2a，任务 A 的 R2a 实现合入后它应当原样通过。

| 测试 | 为什么失败 | 改成什么 |
|---|---|---|
| `desk_dependency_growth_test.go` 的 `TestSecretaryConversationDependenciesStayASet` | 原来固定期待 1 条依赖；R5 之后这一轮同时记了陈述和原话 | 期待的依赖集合是「供给的那条陈述 + 供给的那份原话」，各一条、`kind` 正确。保留原意：对话变长依赖不翻倍、没有重复项、旧结构的依赖照样能读 |
| `ux_regressions_test.go` 的 `TestDeskHistoryCannotBypassDestinationItemScope`（三个子用例） | 原来断言 Brief 里任何位置都不出现 `519823`；R7 之后提问保留，而夹具里提问本身就是这串数字；R2 之后原话不按项目过滤 | 把夹具里的提问、陈述、模型回答改成三段互不相同的标记文字，然后断言：模型的旧回答不在 Brief 里（换成了替换语）；那条陈述不在 Brief 的记忆部分，它的 id 不在 `ContextMemoryIDs` 里；提问在 Brief 里。`exclude` 子用例按 R2a 再断言：那条陈述出自的原话不在 Brief 的「相关原话」一节里；另外两个子用例（只是项目不同）断言它在 |
| `desk_turn_test.go` 的 `TestSecretaryStablePrefixAndVisibility` | 用户把「暗号 hunter2」这条记忆对所有副手隐藏后，旧断言要求提示词里不出现 `hunter2`；现在原话一节会给出这句话 | **不改。** 用户决定「跟着隐藏」（总览 R2a）。任务 A 的 R2a 实现合入之前这组保持失败；合入后应当不改一个字就通过，通不过就是任务 A 的发现 |

**写死的日期。** 另有一批旧测试因为把日期写死成 2026-10-02 而失败，和本批规则无关，已单独派给 [任务 D](D-date-fixtures.md)，从 main 修。你不用管它们，但你自己的测试不许写死「必须在未来」的日期：用相对今天的日期，不依赖今天是星期几。任务 D 合入 main、同步到集成分支之前，这几项失败在报告里单列，不算本批的发现。

## 验收报告

`docs/evaluations/<日期>-phase2-batch1-acceptance.md`：被测的集成分支提交号；38 条序列逐条的结果和对应测试名；浏览器用例结果；发现清单（现象、预期、你认为该谁修）；没有覆盖到的地方；改过预期的已有测试。报告里不放线上真实内容。
