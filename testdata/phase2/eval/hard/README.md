# 第二档：固定种子的压力题

基础档 `../corpus.json` 的资料、证据、事实、基线保持原样。压力档由 `cmd/pcas-eval/fixture/hard.go` 和本目录 `config.json` 装载时生成；不提交生成后的资料。固定种子 20261002，SHA-256 以种子、问法和竞争项序号索引，增加噪声不会改变标准资料、证据、问题、事实或日期。

默认 40 份标准资料 + 每个案例 64 份竞争资料（2560 份）+ 128 份无关资料 = 2728 份。40 道题，五种类型各 8 题：

| 类型 | 依据 | 干扰为何不应排前 |
|---|---|---|
| time-entity | 去年本人计划，明确人/地 | 其他年份本人的计划、他人计划、本人到访事实、AI建议 |
| time-only | 上周本人计划的完整集合，共 8 条 | 其他时间计划、同周他人计划或本人事实、AI建议 |
| entity-only | 只有人或地点，没有时间，本人明确的展陈决定 | 未决定的考虑、他人决定、比较事实、AI建议 |
| wrong-time | 请求去年，所有该人/地资料都不在去年；本人最新计划在前年 | 更早的计划、别人的话、不同性质、AI提案；考 R8 按实体放宽 |
| ordinary | 独有非实体话题的保养口令，问法不含日期或人/地别名 | 不同话题；此类是结构化规划跳过时的保留对照 |

人物、地点均为虚构，每案例一对，40 对互不复用也不相互包含。每对人/地都出现在 64 份竞争资料中；四种竞争模板各 16 份。时间表达靠 `expressed_at`，不把题目的「去年」「上周」额外注入标准正文；事件时间未知即空。导入时间为基准日，不能冒充说话时间。AI 资料没有标准抽取，原话仍在库中。

时间窗口用用户时区的日历年和周一开始的上周；跨年、闰年、每个星期几都重新算日历，避免固定 365 天/7 天误差。标准资料和所有答案在竞争资料产生前构造。问法、事实、来源下标不会根据检索输出修改。只允许协调者要求的难度校准调整竞争数量。

时间档的 8 个问法是同一「上周本人计划」完整集合的不同措辞，共用 8 项证据，不能当作 8 个独立事实事件。全档共有 96 个必需证据计数（该集合按题重复计）。报告同时按类型输出，避免该重复集合掩盖其他类型。

CI 在完整 2728 份压力资料上运行每类前 2 个问法（共 10 题）；只减少问法，不减少竞争资料或标准集合。全量 40 题由协调者手动跑。压力基线独立记在 `baseline.json`，初始 0 等待审核；基础档仍用上一级的基线。

四种竞争模板各 640 份，共 2560 份；其中 640 份 AI 原话没有标准记忆。加上标准资料和无关资料，装载 2728 份原话、2088 条标准记忆。基础档和压力档分别装入新的临时 schema，不混合两个题库。

从仓库根目录运行，`EVAL_TEMP_DSN` 必须指向空的本机临时 PostgreSQL。固定基准日用于复现实验；平时省略 `-anchor`，按用户时区的当天生成：

```bash
# 压力档全量 40 题：捕获真实秘书收到的内容，不调用真实模型。
go run ./cmd/pcas-eval -fake -tier hard -mode retrieval \
  -database-url "$EVAL_TEMP_DSN" -anchor 2026-10-03 \
  -revision "$(git rev-parse HEAD)" -output /tmp/hard-full
# 和 CI 相同的 10 题；仍装载全部资料。
go run ./cmd/pcas-eval -fake -tier hard -mode retrieval -ci-subset \
  -database-url "$EVAL_TEMP_DSN" -output /tmp/hard-ci
# 仅有原话的诊断对照，独立重建题库。
go run ./cmd/pcas-eval -fake -tier hard -mode retrieval -raw-only \
  -database-url "$EVAL_TEMP_DSN" -output /tmp/hard-raw
# 基础档全量 36 题，使用原来的独立基线。
go run ./cmd/pcas-eval -fake -tier basic -mode retrieval \
  -database-url "$EVAL_TEMP_DSN" -output /tmp/basic-full
# 同时运行两档的全量检索集成测试（默认 CI 只缩减压力档问法）。
PCAS_TEST_DATABASE_URL="$EVAL_TEMP_DSN" PCAS_EVAL_FULL=1 \
  go test -count=1 -v -run '^TestPhase2EvalRetrieval$' ./internal/postgres
```

默认 `-mode comparison` 保留三种做法的回答与全资料抽取比较，可搭配 `-tier hard` 或 `-ci-subset`；子集只减回答问法，抽取仍覆盖全部资料。真实模型由协调者使用原有通道环境变量运行。Markdown/JSON 均含档位、种子、基准日、渲染后数据摘要、被测提交、按类型计数和逐题送达数量；不输出生成资料正文。干扰率按标注的干扰—证据对计算，证据和干扰都未送达时分子为 0，因此需和召回率一起看。
