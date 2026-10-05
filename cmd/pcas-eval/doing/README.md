# 办事模式与本机流程

## 第 4 批：light / medium / heavy（V2c）

显式选择新方法时，doing 走新增的三档驱动；省略 `-methods` 仍走原来的 none/current/ideal，原模式、题目和标准不改。三档通过真实 `Store.DeskTurn(postgres.WithMemoryTier(ctx, tier), …)` 执行，产品负责上下文接力、选分组、并行读者、自查、校验、重试和超时回退。没有改 `internal/` 或新建产品迁移。

每次运行先装平铺的来源/原文 claim；只使用记忆原文、日期、来源标题，不装题集里的 tags、更新/重复关系、must/bonus/forbidden 等 oracle。随后依次调用产品 `ScheduleOrganize` / `ProcessOrganize`、`ScheduleCompare` / `ProcessCompare` / `ProcessEntityCompare`、`ScheduleStatus` / `ProcessCard` / `ProcessHandover`，经实际队列领取和确认任务。准备期间使用同一真实模型，保留产品的重试、预算、每小时限速和建卡等待规则。准备全部完成才答题；失败/中断的准备不能当作完整成绩。默认 `-prepare-timeout=12h`，可以显式延长；不会通过修改时间戳或标签赶跑限速。

准备只跑一次。关闭准备库的连接后，用 PostgreSQL `CREATE DATABASE … TEMPLATE …` 复制每个 worker 的答题库，因此三个方法/三次重复从同一份派生状态出发，不反复整理，也不串入别题的原话。所用角色须有 `CREATEDB`（一次性容器脚本创建的测试角色已有）；DSN 须为 PostgreSQL URI。复制库使用新的随机名字，结束后仅删除本次创建的这些库；原库仍由空库守卫和脚本的容器归属标签保护。答题前清掉该 worker 上次产生的对话、候选和非种子原话，不运行后台 worker，不让评测问答再次抽取或参与整理。

这是既有“供给上下文之后交付草稿”的评测口径：本机模型桥把真实秘书主调用转换为原样的 `AnswerSystem` / `AnswerPrompt`，保留题集 `as_of`、原回答指令和 600 字符验收上限；再把文本放进无动作、`remember=false`、`missingKeyInfo=false` 的秘书输出 envelope。重档选组/读者和中重档自查都使用产品真实提示词与模型调用，产品最终返回的回复才交给原来的双判。自查是产品原生 JSON 格式及截止时间，失败时保留原草稿，不在评测器另补自查。模型桥不启用工具或外发，不提供 gold；本模式不测完整秘书动作执行、自动升档或 Codex 原生 structured-output 的格式约束。代码不会在拿到产品回复之后再多答一次。

`-workers` 在三档模式中为 1–4，并且是**全部模型调用的全局上限**，包含准备、并行重档读者、回答和评分，不仅是题目并发数。排队计入产品读者/自查的截止时间，可能引起产品回退，这是当前资源配置下的真实结果。各重复有完成屏障，题目/方法次序轮转。每行报告产品调用数（含实际发起的失败/重试）、两次评分、有效档位、选组数量、各用途输入字符、读者/自查的失败及格式问题；排队到取消、尚未发起的产品调用另列 `not_started`。它们不会算成已经调用了模型。`evidence_ms` 在新三档中包含整个产品取上下文、答题、自查流程；办事/总耗时仍按端到端实测。主答复字符与读者/自查的输入字符分开，不能将主答复的字符数当成重档全部开销。

新产物仍只有数字、题号和判定，存在仓库外。`report.prepare.json` 单独记录后台准备三阶段的墙钟耗时、调用和失败数、各用途调用数、输入字符与完成任务数，以及当前 claim/退出/卡片计数和交接说明是否建好；完成报告也嵌入该记账。准备成本与一次通道预检不计入每题模型调用或延迟。题集/固定回答指令/双判指令的 SHA 继续保留。

本轮虚构测量：原 671 条记忆/旧 120 题不变；light/medium 全部三遍；heavy 只跑跨分组/外发 40 题三遍。下面一个命令只跑这些新对照，**不重跑 current/ideal**：

```sh
PCAS_EVAL_CODEX_HOME=/ABSOLUTE/DEDICATED/CODEX_HOME \
  scripts/p25-v2-run.sh -channel=codex -model=gpt-6.1-sol \
  -methods=light,medium,heavy -heavy-categories=cross_group,outgoing \
  -repeats=3 -workers=4

# 少量假模型冒烟：仅检验实际产品通道/报告，不代表质量
scripts/p25-v2-run.sh -fake -methods=light,medium,heavy \
  -tasks=CROSS-01,RECALL-01 -heavy-categories=cross_group,outgoing \
  -repeats=3 -workers=4
```

**协调者的真实题跑法**：先按下文流程取得同意并生成/核对 `approved.json`；本执行者不读真实题或线上库。先验证，再用自己的一次性 tmpfs 库跑。私有模式默认三档全跑，不继承本轮虚构题的 heavy 子集；如需子集必须明确指定 `-heavy-categories`。

```sh
go run ./cmd/pcas-eval -mode=doing -private -validate \
  -suite /var/tmp/pcas-v2-private/approved.json \
  -methods=light,medium,heavy

PCAS_EVAL_CODEX_HOME=/ABSOLUTE/DEDICATED/CODEX_HOME \
  scripts/p25-v2-run.sh -private \
  -suite /var/tmp/pcas-v2-private/approved.json \
  -methods=light,medium,heavy -repeats=3 -workers=4
```

这些是文本导出的平铺副本，缺少线上拓扑、历史修订和向量；三档的背景准备由副本重新推导，不能称作线上现状层的精确快照。若副本没有任何可建卡的分组，准备合法完成零张卡，产品按 R4-5 回退，报告 `effective=legacy-fallback`；不能当作三档已发挥作用。私有题、模型 home 和所有产物继续留在仓库外，不提交、不上传回复正文。

产品检索、期限校验和自查依赖主机当前时间，主答复与 gold 仍使用冻结 `as_of`。按本轮要求复用的 current/ideal 是历史数字：跨日期、不同并发，以及整理随机性都会影响结果，只报告数值验收和已知差异，不能把它们当成无条件的同日因果实验。heavy 未测直接回忆时，该条验收必须报告“未覆盖”，不能从 light/medium 或另两类代推。旧 baseline 和既有数字产物不修改。

虚构 V2c 如遇传输缺行，可在第一次自建容器退出前，用自己记下且核对过所有权标签的完整容器 ID，对**未答题的准备模板库**保存 `pg_dump -Fc`，文件留在仓库外并限制权限。正常脚本退出仍删除其容器。补跑时显式恢复到另一个新建的 tmpfs 容器：

```sh
PCAS_EVAL_PREPARED_DUMP=/var/tmp/OWN-FICTIONAL-PREPARATION.dump \
PCAS_EVAL_CODEX_HOME=/ABSOLUTE/DEDICATED/CODEX_HOME \
  scripts/p25-v2-run.sh -channel=codex -model=gpt-6.1-sol \
  -methods=light,medium,heavy -heavy-categories=cross_group,outgoing \
  -repeats=3 -workers=4 -resume-report=/var/tmp/OWN-RUN/report.partial.json
```

此修复入口只接受旧 671 条虚构记忆/120 题的固定 V2c 矩阵，拒绝私有模式、任务筛选和跨主机日期；核对题集、模型、指令、并发、完成双判和恢复库的准备标记、全部来源原文、状态计数及没有答题原话。所有已完成行原样保留，只补缺失的档位/题号/遍次；不重新准备，不按得分挑题。`resume_sources` 保留前次指纹和全部失败尝试，每次调用仍有自己的通道预检。快照不入仓库，不能将新的整理结果冒充原快照。正常及私有运行继续要求空库，真实流程不使用此入口。

完成后可用无模型、无数据库的复算命令，把历史基线与新三档分别在 old120 和共同的 cross/outgoing40 上汇总。它拒绝假模型、不完整矩阵、重复行和变动的量尺，从双判重算每行，再算范围；heavy 不进入 old120 的全体均分：

```sh
go run ./cmd/pcas-eval/doing-tiers-report \
  -tiers /var/tmp/OWN-RUN/report.json -output /var/tmp/OWN-RUN/comparison
```

新增 `-mode=doing` 和协调者专用的 `-mode=doing-propose`。已有 comparison / retrieval、phase2 题集、产品实现与迁移均未修改。模式分发只识别新名字。

## 虚构数据运行

在仓库根目录：

```sh
# 假模型：全量三次，供 CI 跑通流程
scripts/p25-v2-run.sh -fake -repeats=3 -workers=4

# 默认订阅通道：复用已登录的专用 home；不执行登录、退出或账户迁移
PCAS_EVAL_CODEX_HOME=/ABSOLUTE/DEDICATED/CODEX_HOME \
  scripts/p25-v2-run.sh -channel=codex -model=gpt-6.1-sol -repeats=3 -workers=8
```

脚本自己建一个 PostgreSQL/pgvector 容器，绑定随机 loopback 端口、用 tmpfs、不挂载线上卷；退出时检查所有权标签，只删除自己记下的完整容器 ID。没有 compose 操作、按名称批量杀进程或接触线上容器。Go 驱动先检查本机数据库完全为空，再迁移并装数据；不会在现有数据库中试着清理表。

默认输出 `/var/tmp/pcas-v2-run.<随机>/report.json` 和 `.md`，都是数字、题号、判定；没有记忆或回答正文。中途失败留下 `.partial.json`，不可当作完整成绩。目录和文件权限分别 0700 / 0600。驱动拒绝所有 Git 仓库内的结果路径，也检查符号链接；`.gitignore` 再加保险。环境中的 `/tmp` 可能自身带有 `.git`，所以采用 `/var/tmp`。

API 通道可显式选 `-channel=openai`，设置 `PCAS_EVAL_BASE_URL`、`PCAS_EVAL_API_KEY` 和 `-model`；不把订阅登录替换为 API key。真实评测应使用协调者确认的默认通道。此模式不估算货币成本；记录每题调用数、输入字符数、耗时。

不通过脚本也可以运行 `go run ./cmd/pcas-eval -mode=doing -database-url <本机空库DSN> ...`。装库代码复用旧评测的隔离、迁移和源/记忆种子工具，但不改旧工具。

## 三种对照和接入接口

| 方法 | 交给办事模型的内容 |
|---|---|
| none | 请求、冻结评测日期，无记忆 |
| current | 真正 `Store.DeskTurn` 所组装的召回记忆、相关原话及原对话上下文；用本机假响应捕获，随后统一办事 |
| ideal | 仅 must 引用的记忆原文、编号及表达时间；没有标准文本、加分、禁止项、更新/重复标签 |

current 使用目前产品的关键词回退，未配置向量模型；不是另写一个关键词检索器。与启用了向量的部署配置不能直接视为等同基线。新整理标签不作为 oracle 装库：每条独立原文有一条已采纳 claim，并保留对应 source/chunk。它衡量给模型的上下文，不衡量记忆提取或现状层实现。

每道 current 题前删除隔离库内之前问题/回答的新记录及对话；原始记忆不变。没有后台 worker、外发工具或产品动作。捕获响应固定为无动作、remember=false；必须恰好发生一次捕获调用、没有动作回执，否则停止。产物记录 `local_capture_calls`，不把它算成真实模型调用。

扩展第四/第五种做法：

```sh
scripts/p25-v2-run.sh -fake \
  -context-adapter=cards=/ABSOLUTE/EXECUTABLE \
  -methods=none,current,ideal,cards
```

适配器不是 shell 字符串。stdin 是一个 JSON 对象：`request`、`task_id`、`as_of`、`database_url`、`owner_id`。适配器要求 URI 格式的 PostgreSQL DSN（容器脚本默认就是这一格式）；search_path 通过标准 options 传递，兼容 pgx 和 libpq。DSN 指向刚装好数据的评测 schema（含 search_path，并默认强制只读），不指向线上。每次适配器读取前也会清掉评测问题/回答，和 current 共用串行锁，避免并发污染；单次适配器上限三分钟。stdout 必须是 `{"context":"提供的上下文","model_calls":0}`；stderr 不进报告。接口不提供标准或 gold 标签；适配器须只读，禁止改变题目、原文、评分或调用模型出 gold。适配器内部调用数是它自行声明的；上下文构造耗时由驱动实测。接入带模型的读者时必须把其全部调用计入 `model_calls`。

## 固定办事和打分

指令在 `runner.go` 的 AnswerSystem / JudgeSystem；结果记录二者的 SHA256、整个题集 SHA256、模型、通道、修订、host date、并发数及固定 600 Unicode 字符上限。超过上限的回答照常逐项判，但不算可直接用；不截断回答以制造通过。所有方法及两次评分用同一模型；每个调用建立新线程，无工具、联网或读取本机文件。

逐题评判 must、bonus、forbidden，并另判是否按 reasonable_handling 处理完成条件。must / bonus 只有两次都 true 才计入；禁止项任一次 true 即计入。可直接用 = 所有 must 都满足 + 零禁止项 + 两次都认为处理正确 + 未超字数。空缺地址的草稿按该题 handling 判，不假装实际发送已完成。

评分员只看该题请求、标准、标准引到的原文和回答，不看完整记忆、不知道方法名、不看另一次评分。任何缺编号、重复编号、漏字段、null、额外字段、附加文本或 JSON 失败都拒绝该行，保存题号、阶段、错误类型和已尝试调用数；不写回答正文。其他独立行继续跑；连续五行失败、登录/限流失败或结果写入失败才中止后续调用。有失败则退出码非零、仅留下 partial，不产出完整成绩。没有自动重试、更换模型或挑最好分数。

顺序在重复之间轮转；最多八个独立回答流程并发，捕获串行。各次重复间有完成屏障。报告有每题构造上下文、回答、双判、办事总耗时及含双判的总耗时；前期装库和一次通道预检不包含在每题延迟和调用数。成功流程的真实调用数为各行 model_calls 之和，另加每次运行的一次预检；失败尝试的 model_calls_attempted 须另计。旧版没有失败计数的 partial 不足以给出精确总调用数。

每次分别按六类与全体输出必须率（按条件微平均）、加分率、禁止项触犯数、可用率（按题）、输入字符数、调用数、耗时及争议题。重复至少三次后，输出 min–max；不把三次重复说成显著性检验。每方法/类别的重复性下限 = max(必须率极差，可用率极差)，单位百分点；比较两种方法时，差距不超过两者较大的下限不能据此判断更好。禁止项单独看触犯数波动范围。

假模型不做语义评判，所有 must 均 false；它的分数只代表流水线固定输出，不能用于质量结论。

产品检索规划器使用主机当前时间，未注入模拟时钟。固定 2026-10-04 题集必须在同一 host date 对比；其他日期复跑需协调者冻结另一套整体平移日期的题集、复核时间关系，并记录新 SHA。跨日期旧分数不是无条件可比基线。

## 真实数据流程：只交脚本，由协调者在用户同意后执行

下面均为操作模板，V2 未执行线上导出。先取得用户同意，确认只读数据库身份、owner 和专用模型 home；真实文件只存在仓库之外。不得运行线上 worker、重启容器或把结果正文上传到 PR。

```sh
# 1. 协调者在自己的私有 shell 中设置，只读账号 DSN 不放命令行或日志
# export PCAS_V2_READONLY_DSN=<含host、user、database的只读PostgreSQL URI>
python3 scripts/p25-v2-private.py export --consent-confirmed --owner <OWNER_UUID>

# 2. 生成待核对候选题（允许模型读真实导出，但这不是最终 gold）
go run ./cmd/pcas-eval -mode=doing-propose \
  -input /var/tmp/pcas-v2-private/export.json \
  -output /var/tmp/pcas-v2-private/proposals.json \
  -codex-home /ABSOLUTE/DEDICATED/CODEX_HOME -per-category=4

# 3. 生成本机 HTML 抽查表，用户逐题选“对 / 不对 / 改成…”
python3 scripts/p25-v2-private.py review
# 用户本机打开 review.html；下载决定到同目录 review-decisions.json

# 4. 只有核对通过的题可进入 approved.json
python3 scripts/p25-v2-private.py approve

go run ./cmd/pcas-eval -mode=doing -private -validate \
  -suite /var/tmp/pcas-v2-private/approved.json

# 5. 一次性库评测；默认仍只输出数字、题号、判定
PCAS_EVAL_CODEX_HOME=/ABSOLUTE/DEDICATED/CODEX_HOME \
  scripts/p25-v2-run.sh -private \
  -suite /var/tmp/pcas-v2-private/approved.json -repeats=3
```

导出使用 `psql -X`，在 REPEATABLE READ READ ONLY 事务中读取给定 owner 的有效 claim 原文，同时复制工作区时区和记忆的表达/记录时间，以 session 配置强制只读、限时；结束 ROLLBACK。URI 拆为 libpq 环境变量，不把含凭据的连接串作为进程参数；数据库地址不自动发现；不使用 Docker 或线上容器。PSQL 错误正文、凭据、owner 和记忆不打印。导出的仅是有效记忆，缺少历史修订、原对话拓扑和向量，不能用这个副本宣称完全重现线上原话检索；若要这种等价性，需要协调者制定只读的全拓扑快照流程。

候选题默认六类各四题。为避免超长输入，按表达时间从新到旧完整选择 JSON 序列化后（包括编号、日期和元数据）不超过 100,000 字符的记忆，不截断单条原文；候选文件保留全部导出，终端只报入选数量。这个样本不是全量记忆覆盖率，可调整 `-max-memory-chars`，先确认模型上下文容量。候选标准附依据编号，并经结构核查后保存；文件本身仍未获核对资格。

抽查表显示请求、必须、加分、不许、依据的记忆原文、能否不追问完成与合理处理。没有远程资源或发送功能。未核对是默认值；“改成…”可以修订完整题目 JSON。核对决定绑定候选文件指纹；候选变了必须重做核对。approve 拒绝不存在的依据、重复题号及非法标准，并为每道通过题设置 reviewed=true；驱动再次拒绝未核对的私有题。用户下载的决定文件也包含修改内容，须保持私有。

协调者结束后按本机留存要求处理导出、候选、HTML 和决定；不要清理共享目录或线上数据。所有真实结果均不提交，连题号对应表也留在本机。

## 本次虚构基线的中断与证据修订合并

本次原跑次完成前两轮，第三轮中断。补齐别名依据后，全部受影响题按三方法、三次重跑；其他题只补跑缺任一方法的第三轮任务（该任务三方法一起替换）。不能挑某种方法的高分行。

```sh
# 所有输入均为这次虚构实验的数字产物；不是私有数据合并器
go run ./cmd/pcas-eval/doing-merge \
  -original-suite /var/tmp/v2-original-suite.json \
  -original /var/tmp/v2-original.partial.json \
  -completion /var/tmp/v2-completion.json \
  -replacement /var/tmp/v2-gold-replacement.json \
  -output /var/tmp/v2-combined
```

工具自动比较题集：记忆与拓扑必须完全不变；改了标准的题全部替换，未改题仅按原报告缺失状态补第三轮。不接受私有题集、失败补跑、模型/指令/日期/并发变化、漏方法、重复行或与原判定不符的分数。产出 1,080 行、六类汇总、波动范围和来源文件 SHA/保留行数。合并调用和耗时只统计保留行；丢弃的已完成流程、预检和失败尝试另列在实验记录中。
