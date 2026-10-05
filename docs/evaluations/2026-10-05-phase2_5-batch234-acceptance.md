# 第 2.5 阶段第 2–4 批独立验收：最终一轮

2026-10-05。契约：[并行方案与契约](../tasks/phase2_5/parallel.md)。验收 PR [#206](https://github.com/soaringjerry/PCAS/pull/206) 和 X4-8 修订 PR [#219](https://github.com/soaringjerry/PCAS/pull/219) 已合入；本次修复重验仍以 `phase2_5/batch234` 为 base。先 fetch、rebase，本次实际集成基线为 `0f13127`；首次最终一轮的基线为 `346ee0c099a82428c9bdec68eb9e42ae359e25a3`。

## 结论

**此前四项未关闭发现均已关闭。** PR #220 修复的 F-B2-6、F-B2-7，PR #221 修复的 F-B4-5 在 `0f13127` 上通过重验，已去掉 skip。X4-8 按修订契约测试“自查多建一个事项，多出的不执行，原有的照常”，关闭 F-B4-4；本次也重新通过。

新的小时限额已写成完整断言，但后端尚未在本次基线上实现，按协调者要求跳过三项：第 1 批整理 120 次、第 2 批整理／比较／实体比较共享 120 次、第 3 批 200 张过期卡分两小时重建 120／80 张。**这些是等待后端 PR 的预期跳过，不是新增实现发现；包含新限额的完整验收结论仍待补验。** 不能用旧限额的通过记录代替新限额验证。

本轮三批完整回归：**82 个顶层测试，80 通过、2 项等待限额 PR 跳过，35 个子用例通过，耗时 342.483 秒，无失败**。第 1 批的整理限额另有 1 项预期跳过，因此本次三项待补验均已明确列出，不计为通过。

| 批 | 顶层测试 | 通过 | 等待依赖跳过 | 按发现跳过 |
|---|---:|---:|---:|---:|
| 第 2 批 | 28 | 27 | 1（#223） | 0 |
| 第 3 批 | 29 | 28 | 1（#222） | 0 |
| 第 4 批 | 25 | 25 | 0 | 0 |

第 1 批真实整理夹具的 `-race` 烟测通过；三项新限额用例的编译／skip 专项通过，确认均显示对应依赖 PR。`go vet ./internal/postgres` 和 `git diff --check` 通过。

三项修复和 X4-8 的 `-race` 专项共 5 个顶层测试、8 个子用例通过。无现状层增加了强制 `heavy` 的子用例，验证退为 `medium` 后仍有主调用和自查，实际账目为 `tier=medium`、`secretary=1`、`selfcheck=1`；关键词升中、`missingKeyInfo` 升中、强制中档也全部通过。

首次最终一轮的历史结果为 82 个顶层测试：78 通过、4 按发现跳过；F-B3-7、F-B3-8 当时已通过并去掉 skip。下列序列表和发现清单已更新到本次复验状态。

## 独立性和运行边界

只依据 `docs/tasks/phase2_5/README.md`、`parallel.md` 写验收，没有打开三个后端实现、后端测试或后端 PR 差异。公共声明通过 `go doc` 确认；协议适配依据本地假模型实际收到的请求。本次只修改自有的 `internal/postgres/phase2_5_b{1,2,3,4}_*_test.go`、本记录和两个授权索引条目；第 1 批只涉及 R12 限额及一次性数据库夹具的超时／磁盘清理。没有修改产品代码、其他人的测试或其他文档。

每个测试使用独立随机名称、标签 `pcas.acceptance=phase2_5-b234-T234` 的一次性 pgvector/PostgreSQL 16 容器。数据目录是 `--tmpfs /var/lib/postgresql/data:rw,size=512m`，按精确名称执行 `docker rm --force --volumes`。只用虚构用户、记忆、事项、导入 ZIP、本地 HTTP 模型和假密钥，没有外部 DSN、真实模型调用或部署，没有操作 `pcas-db-1`，没有按进程名杀进程，也没有全局清理 Docker 卷。第 1 批夹具也已改成 tmpfs，并按自有容器精确名称带卷删除。

比较、建卡和交接说明经过真实 Schedule、Store.Claim 和 Process，不自行构造 worker.Job。副手经过真实 RunAgents 领取自有队列任务，事项通过公共 `Execute(addTask)` 创建。冻结派生数据只作为读接口和办事测试的前置条件；实际生成、比较、重建的测试使用假模型和真实写入事务。额度测试保留真实花费与后台预留记录，统一平移自有测试时间推进，不删除或伪造调用记录。

## 第 2 批：X2-1–X2-13

| 序列 | 验收内容 | 结果 |
|---|---|---|
| X2-1 | 同组三条真实比较；两条 duplicate；复制独立来源证据，trust=repeated、mergedFrom=2，无额外修订 | 通过 |
| X2-2 | 真实替代后旧期限 superseded，指向新期限；回忆和默认列表只用新期限 | 通过 |
| X2-3 | 真实替代使原回答 outdated，比较前正控制未过期 | 通过 |
| X2-4 | 真实 duplicate 不使原回答过期；纠正正控制仍能标更新 | 通过 |
| X2-5 | 用户确认、亲手纠正两种输入均 protected，模型试图退出它们的输出丢弃 | 通过 |
| X2-6 | A↔B 两项丢弃；增补 A→B→C 最终都指当前的 C | 通过 |
| X2-7 | 模型屏障期间纠正／删除；涉及变化项的结果丢弃，独立替代对照常落库 | 通过 |
| X2-8 | restoreMemory 后同版再次比较不退出恢复项，真实动作 Undo 成功；直接撤销正控制通过 | 通过，关闭 F-B2-6（PR #220） |
| X2-9 | 真实重复合并后删除保留项；并入项保留历史、不复活成当前 | 通过 |
| X2-10 | 同项目小陈／陈亮明确 same=true 后合并；两名字回忆取全；主体／提及改指；undoEntityMerge 后各归各 | 通过 |
| X2-11 | 同名老王明确 same=false 不合并；那个／这位等模糊称呼不进入候选 | 通过 |
| X2-12 | 虚构 ZIP 导入、公共解析器取本人消息、模拟直接提取结果；trust=stated、confirmation=unknown 单独即可进入 AnswerDesk 输入／Used；confirmed 对照也进入 | 通过，关闭 F-B2-7（PR #220） |
| X2-13 | 准备前版 compared 标记后重新比较；已退出项等待及新请求中均保持退出，无额外修订 | 通过 |

`CompareVersion` 是公共常量；X2-13 准备前版 `compared` 的持久状态，模拟新程序遇到旧规则数据，不修改产品源码。X2-12 的聊天解析和导入是真实入口；抽取不属于本批，直接提取结果用公共 Commit 准备。

增补通过：最近 200 条及剩余 5 条补批；整理调用期间比较不调用模型，真实队列延期后恢复；没配模型不排队；非法 JSON／缺任一数组不推进 compared；越界、零、负数、自指丢弃；trust 优先级、独立来源计数、兼容字段、当前／历史分页和分组统计。

## 第 3 批：X3-1–X3-13

| 序列 | 验收内容 | 结果 |
|---|---|---|
| X3-1 | 真实生成项目卡，原样记忆、每条只进一个栏目，不增修订 | 通过 |
| X3-2 | 不在本组的输出编号丢弃 | 通过 |
| X3-3 | 周一说的周五上午十点期限，按用户时区生成正确日期 | 通过 |
| X3-4 | 每周二晚上固定安排；每周二 7 点保留上午／下午不明 | 通过，关闭 F-B3-7 |
| X3-5 | 推不出的日期、已过去期限丢弃 | 通过 |
| X3-6 | 实际纠正使旧版本条目不返回，卡 stale；10 分钟后真实重建纳入新版本 | 通过 |
| X3-7 | 实际比较退出旧条目，然后读失效卡并真实重建纳入新记忆 | 通过 |
| X3-8 | 删除条目和关联期限，交接说明过期 | 通过 |
| X3-9 | 200 张过期卡，第一小时重建 120 张、同小时不再重建，下一小时完成余下 80 张；核对账目和进度 | 等待 PR #222，预期跳过 |
| X3-10 | 30 次实际重建，首次交接单独算，普通重写最多两次且间隔至少 6 小时 | 通过 |
| X3-11 | inferred 不进入交接说明模型输入 | 通过 |
| X3-12 | 临时提高串行公共规则版本，重建期间仍返回旧卡，测完恢复变量 | 通过 |
| X3-13 | 当前分组不足三条时卡和目录均不返回 | 通过 |

增补通过：首次本人四类优先，然后按条数排序（关闭 F-B3-8）；最近 300 条输入上限、普通卡 25 条、自身要求 60 条和 appliesTo；九节标题、空节、1800 字上限、trust 和花费；不配模型、读事务、接口目录／详情、构建进度、版本失效；建卡调用期间实际纠正／删除后重试重建。

## 第 4 批：X4-1–X4-11

| 序列 | 验收内容 | 结果 |
|---|---|---|
| X4-1 | 有现状卡时，下周期限和固定安排进入期限分节 | 通过 |
| X4-2 | 请求点名项目或别名，卡必选，plan.groups 留痕 | 通过 |
| X4-3 | 起草邮件时适用要求和不限范围要求常驻；六节按约定顺序 | 通过 |
| X4-4 | 无卡无交接时真实回退回答／检索成功 | 通过 |
| X4-5 | 所有输入卡片条目记进真实回答依赖；即使没被 used 点名，纠正后也标更新 | 通过 |
| X4-6 | 有卡时，“仔细”或 missingKeyInfo 升中，两次调用、同数据自查、tier=medium | 通过 |
| X4-7 | 有卡时自查失败，保持初稿，不重试 | 通过 |
| X4-8 | 有效 create_task 正控制；原始事项动作正常执行；自查增加的新事项不得执行，重复原动作也不多执行 | 通过，关闭 F-B4-4（契约修订、PR #219） |
| X4-9 | 默认副手重档，选五组，五读者并行，一组失败后用其余四组成功作答 | 通过 |
| X4-10 | 慢读者按共享 90 秒截止取消，保留快读者结果，整轮在 170 秒内完成 | 通过 |
| X4-11 | 默认检索、卡片、轻／中提示词均排除退出记忆 | 通过，关闭 F-B4-1 |

增补通过：点名七组最多六卡；重档只接受有效不同 key，上限十二，外组 ID／重复 ID 丢弃；主调用在 30 秒内至多两次（未要求失败一定重试）；自查按初稿实际耗时截止、保留初稿；有效 create_task 的正控制后，skip 撤掉原动作；十二条要求、十五条最近期限和补充记忆上限；实际花费用途、tier、plan.groups；四种可用 trust 实际进入卡片上下文，inferred 随代理设置排除或纳入。读者的 plan 可记当前读取的一组，最终作答／自查记录整个选择集。

另用真实 DeskTurn 作答后真实 ProcessCompare，验证 duplicate 不标更新、superseded 标更新；这不是只对预置回答做 SQL 退出检查。

## 随机序列和不变量

| 批 | 种子／步数 | 实际操作与不变量 | 结果 |
|---|---|---|---|
| 第 2 批 | 252502／32 步，另有 40 步读取序列 | 新增、纠正、删除、真实重复并入和替代；当前／历史分流正确；模型只拿当前版本；派生比较不增加修订 | 通过 |
| 第 3 批 | 252503／36 步 | 纠正、删除、退出、真实重建；卡片条目始终指向仍当前的正确版本，原文不改、每条唯一；卡和交接输入无退出或旧版本原文；无派生修订 | 通过 |
| 第 4 批 | 252504／24 步，另有 32 步回退检索序列 | 纠正、退出、删除、补充、轻／中交替；每个请求均排除已退出和旧版本原文；每轮调用数和实际 tier 正确；无派生修订 | 通过 |

首次最终一轮的三批随机序列、比较期间纠正／删除、建卡期间纠正／删除、整理／比较互斥、中档同数据自查和五组并行读者的 `-race` 检查均通过。按副手正文协议适配后的三项重档专项（含真实 90 秒截止）也通过 `-race`，无竞态报告。完整四类本人卡排序增强专项通过。

## 发现清单

| 编号 | 判断／当前状态 | 本次复验与关闭依据 |
|---|---|---|
| F-B2-6 | 实现问题，**已关闭** | PR #220：实际比较退出旧记忆、restoreMemory、加入同组新记忆、同版再次比较；恢复项仍当前，Undo 成功。完整断言和直接撤销正控制均通过，已删除 skip |
| F-B2-7 | 实现问题，**已关闭** | PR #220：本人直接导入消息提取出的记忆 trust=stated、confirmation=unknown；单独查询和加入相同原话的 confirmed 对照后，均按 trust 进入真实 AnswerDesk 输入／Used。已删除 skip |
| F-B4-4 | 契约问题，**已关闭** | 协调者修订 X4-8；目前没有删除动作。PR #219 已改用有效 create_task，证明原动作可执行后验证自查增加的动作不执行；本轮再次通过，无 skip。R4-10 的删除条件留待产品以后支持删除时验证 |
| F-B4-5 | 实现问题，**已关闭** | PR #221：无现状层时只回退取材，中档照常自查；关键词、missingKeyInfo、强制中档及新增的强制重档回退中档均通过，核对回复和真实调用账目，已删除 skip |

此前已关闭：F-B2-1–5、F-B3-1–8、F-B4-1–3。原 F-B2-6、F-B2-7、F-B4-5 的上线阻碍已消除；当前没有仍开放的行为失败发现。

## 等待后端的限额验收

| 测试 | 新契约断言 | 等待及状态 |
|---|---|---|
| `TestPhase25B1_R12_OneHundredTwentyBatchesPerHour` | 整理前 120 次成功，第 121 次延期且不加尝试；推进自有账目到下一小时后恢复，无新记忆修订 | 等待 [PR #223](https://github.com/soaringjerry/PCAS/pull/223)，已 skip |
| `TestPhase25B2_OrganizeCompareAndEntityShareOneHundredTwentyCallsPerHour` | 先真实整理 20 次，比较和实体比较继续占用同一额度，总共 120 次；三种处理入口在边界都不能再调用模型；下一小时恢复，记忆不增修订 | 等待 [PR #223](https://github.com/soaringjerry/PCAS/pull/223)，已 skip |
| `TestPhase25B3_X3_9_HourlyOneHundredTwentyAcrossTwoHundredStaleCards` | 200 张过期卡，120／80 分两小时完成；同小时无额外调用，最终 card 花费 200 次、进度 200／200，记忆不增修订 | 等待 [PR #222](https://github.com/soaringjerry/PCAS/pull/222)，已 skip |

三项新限额测试目前只验证编译和 skip 状态，没有声称新断言执行通过。后端合入并收到协调者通知后，去掉这三处 skip，执行全部限额断言并更新本记录。记录和代码明确区别“等待依赖”与 `finding F-B…`。

契约同步备注：`parallel.md` R2-6 和第 8 节已改为整理／比较合计 120 次，但第 7 节入口摘要仍写“两种合计每小时最多 30 次”。本次按用户明确裁定及 R2-6 的新数编写；未擅自改契约。

## 可复验命令和限制

在本 PR 工作区运行：

```sh
go test ./internal/postgres -run '^TestPhase25B[234]_' -count=1 -timeout=15m -json
go test -race ./internal/postgres -run '^TestPhase25B(2_(RandomActual|X2_7_|CompareDoesNotCall)|3_(RandomActual|Concurrent)|4_(RandomPrompt|X4_6_|X4_9_))' -count=1 -timeout=10m -json
go test -race ./internal/postgres -run '^TestPhase25B3_RandomCardReadSequence$' -count=1 -timeout=5m -json
go test -race ./internal/postgres -run '^TestPhase25B4_(X4_9_|X4_10_|SelectionValidates)' -count=1 -timeout=5m -json
go test ./internal/postgres -run '^TestPhase25B3_InitialSelfFirstThenLargestAndUsage$' -count=1 -timeout=3m -json
go test ./internal/postgres -run '^TestPhase25B4_CardContextUsesTrust' -count=1 -timeout=3m -json
go test -race ./internal/postgres -run 'TestPhase25B2_X2_(8_Restore|12_Imported)|TestPhase25B4_(MediumPromotionWithoutCards|X4_8_SelfcheckCannotAddActions)$' -count=1 -json
go test -race ./internal/postgres -run '^TestPhase25B(1_(X01_OrganizeInPlace|R12_OneHundredTwentyBatchesPerHour)|2_OrganizeCompareAndEntityShareOneHundredTwentyCallsPerHour|3_X3_9_HourlyOneHundredTwentyAcrossTwoHundredStaleCards)$' -count=1 -json
go vet ./internal/postgres
git diff --check
```

上列限额命令在依赖未合入时会显示三项 skip，不代表限额通过。模型协议适配来自假模型收到的公开输入，不能参考后端实现；不修改产品来迎合验收。

本轮验证的是契约行为、队列、事务、时间额度和模型上下文边界；只用确定性本地模型，**没有执行 R4-14 的真实模型 168 题三遍办事质量评测，也没有做前端视觉验收或部署**。这两项不能用本轮 Go 测试结果替代。


## 整理、比较的调度并发补验（main，第 3 批 PR #232 的后续）

按用户同一项并发验收要求，先交建卡 [PR #232](https://github.com/soaringjerry/PCAS/pull/232) 后，本次另从 `origin/main` 的 `acc6c3c` 建 `phase2_5/b12-concurrent-acceptance`，base 为 `main`，新增整理、比较各一条启用的验收测试。没有修改产品代码、建卡预算用例或 F-B2-8；F-B2-8 原 skip 保留，等待协调者通知。

两条共用的夹具都运行连续 Schedule 循环、真实 Claim→Process 及另一 Store 连接池的快照／普通 addTask 命令／真实秘书 DeskTurn。每类前台请求有无后台运行的正控制，之后各设 3 秒上下文反复请求；记录真实调度／处理与前台请求重叠次数。首个本地假模型请求待前台一轮完成后释放（最长 12 秒，另受后台上下文约束），使只有两批的整理积压也确实覆盖三类前台操作的重叠，不以 SQL 造锁。开始前准备／老化自有数据；并发期间使用真实墙钟，辅助流水线任务经公共 Claim／Defer 延期保留。

`go test -race` 在上述 main 基线通过两项，合计 20.196 秒，无数据库死锁、无前台请求错误、无 Go 数据竞态报告：

| 路径／测试 | 输入与完成断言 | 调度／处理重叠 | 成功后台任务 | 前台请求次数／处理期间启动 | 前台最长耗时 |
|---|---|---:|---:|---|---|
| `TestPhase25B1_ConcurrentOrganizeSchedulerWorkerAndUserRequests` | 80 条虚构原始记忆全为当前 OrganizeVersion；整理队列全 done；真实 organize 账目存在；不增加原记忆修订 | 24 次 | 2 | 每类 4 次，其中每类 2 次在处理中启动 | 快照 33.1 ms，普通命令 65.2 ms，秘书 325.1 ms |
| `TestPhase25B2_ConcurrentCompareSchedulerWorkerAndUserRequests` | 24 个虚构主题、72 条已整理原始记忆全为当前 CompareVersion；比较／实体比较队列全 done；真实 compare 账目存在；空比较结果不退出记忆，不增加修订 | 110 次 | 24 | 每类 13 次，均在处理中启动 | 快照 42.0 ms，普通命令 135.5 ms，秘书 355.1 ms |

这两条在当前 main 的并发场景通过，保留启用；没有人为制造失败或给它们添加 finding skip。建卡路径捕获的 SQLSTATE 40P01 及未完成现状工作由先交的 PR #232／F-B3-10 记录，此处没有用整理、比较的通过覆盖建卡失败结论，也没有将本次专项统计当成三批全套回归。

复验命令：

```sh
go test -race ./internal/postgres -run '^TestPhase25B[12]_Concurrent(Organize|Compare)SchedulerWorkerAndUserRequests$' -count=1 -timeout=3m -json
go vet ./internal/postgres
git diff --check
```

只读公开契约、公开方法声明及自有测试／本地假模型输入，没有打开后端实现、后端测试或后端 PR 差异。一次性数据库使用 tmpfs，按精确自有容器名称带卷删除；没有访问共享数据库或真实模型。
