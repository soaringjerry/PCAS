# 办事模式与本机流程

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

适配器不是 shell 字符串。stdin 是一个 JSON 对象：`request`、`task_id`、`as_of`、`database_url`、`owner_id`。DSN 指向刚装好数据的评测 schema（含 search_path），不指向线上。stdout 必须是 `{"context":"提供的上下文","model_calls":0}`；stderr 不进报告。接口不提供标准或 gold 标签；适配器须只读，禁止改变题目、原文、评分或调用模型出 gold。适配器内部调用数是它自行声明的；上下文构造耗时由驱动实测。接入带模型的读者时必须把其全部调用计入 `model_calls`。

## 固定办事和打分

指令在 `runner.go` 的 AnswerSystem / JudgeSystem；结果记录二者的 SHA256、整个题集 SHA256、模型、通道、修订、host date、并发数及固定 600 Unicode 字符上限。超过上限的回答照常逐项判，但不算可直接用；不截断回答以制造通过。所有方法及两次评分用同一模型；每个调用建立新线程，无工具、联网或读取本机文件。

逐题评判 must、bonus、forbidden，并另判是否按 reasonable_handling 处理完成条件。must / bonus 只有两次都 true 才计入；禁止项任一次 true 即计入。可直接用 = 所有 must 都满足 + 零禁止项 + 两次都认为处理正确 + 未超字数。空缺地址的草稿按该题 handling 判，不假装实际发送已完成。

评分员只看该题请求、标准、标准引到的原文和回答，不看完整记忆、不知道方法名、不看另一次评分。任何缺编号、重复编号、漏字段、null、额外字段、附加文本或 JSON 失败都停止该次运行。没有自动重试、更换模型或挑最好分数。

顺序在重复之间轮转；最多八个独立回答流程并发，捕获串行。各次重复间有完成屏障。报告有每题构造上下文、回答、双判、办事总耗时及含双判的总耗时；前期装库和一次通道预检不包含在每题延迟和调用数。真实调用总数为各行 model_calls 之和，另加一次预检。

每次分别按六类与全体输出必须率（按条件微平均）、加分率、禁止项触犯数、可用率（按题）、输入字符数、调用数、耗时及争议题。重复至少三次后，输出 min–max；不把三次重复说成显著性检验。每方法/类别的重复性下限 = max(必须率极差，可用率极差)，单位百分点；比较两种方法时，差距不超过两者较大的下限不能据此判断更好。禁止项单独看触犯数波动范围。

假模型不做语义评判，所有 must 均 false；它的分数只代表流水线固定输出，不能用于质量结论。

产品检索规划器使用主机当前时间，未注入模拟时钟。固定 2026-10-04 题集必须在同一 host date 对比；其他日期复跑需协调者冻结另一套整体平移日期的题集、复核时间关系，并记录新 SHA。跨日期旧分数不是无条件可比基线。

## 真实数据流程：只交脚本，由协调者在用户同意后执行

下面均为操作模板，V2 未执行线上导出。先取得用户同意，确认只读数据库身份、owner 和专用模型 home；真实文件只存在仓库之外。不得运行线上 worker、重启容器或把结果正文上传到 PR。

```sh
# 1. 协调者在自己的私有 shell 中设置，只读账号 DSN 不放命令行或日志
# export PCAS_V2_READONLY_DSN=<只读身份的本机DSN>
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

导出使用 `psql -X`，在 REPEATABLE READ READ ONLY 事务中读取给定 owner 的有效 claim 原文，同时以 session 配置强制只读、限时；结束 ROLLBACK。数据库地址不自动发现；不使用 Docker 或线上容器。PSQL 错误正文、凭据、owner 和记忆不打印。导出的仅是有效记忆，缺少历史修订、原对话拓扑和向量，不能用这个副本宣称完全重现线上原话检索；若要这种等价性，需要协调者制定只读的全拓扑快照流程。

候选题默认六类各四题。为避免超长输入，按表达时间从新到旧完整选择不超过 100,000 字符的记忆，不截断单条原文；候选文件保留全部导出，终端只报入选数量。这个样本不是全量记忆覆盖率，可调整 `-max-memory-chars`，先确认模型上下文容量。候选标准附依据编号，并经结构核查后保存；文件本身仍未获核对资格。

抽查表显示请求、必须、加分、不许、依据的记忆原文、能否不追问完成与合理处理。没有远程资源或发送功能。未核对是默认值；“改成…”可以修订完整题目 JSON。核对决定绑定候选文件指纹；候选变了必须重做核对。approve 拒绝不存在的依据、重复题号及非法标准，并为每道通过题设置 reviewed=true；驱动再次拒绝未核对的私有题。用户下载的决定文件也包含修改内容，须保持私有。

协调者结束后按本机留存要求处理导出、候选、HTML 和决定；不要清理共享目录或线上数据。所有真实结果均不提交，连题号对应表也留在本机。
