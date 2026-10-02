# 任务 E1：抽取出人、地点、时间（后端）

执行者 6.1 Sol。分支 `phase2/b2-E1-extraction`，从 `origin/phase2/batch2` 建；工作区 `/root/PCAS-wt/b2-E1`；Draft PR 的 base 是 `phase2/batch2`。

先读 [并行方案与数据约定](../parallel.md) 和 [第 2 批契约](README.md) 的 R1–R10、R12 和序列 X1–X18。你负责让抽取的结果带上人、地点、时间，并正确地写进记忆。

## 现在的代码

- `processing.go` 的 `ProcessExtraction`：取资料 → 组提示词 → 调模型 → 逐项校验 → `rememberTx` 写记忆、`saveCandidate` 写候选。提示词是 `extractionInstructions`，每一项的结构是 `extractedItem`，「原话有据还是待确认」由 `extractionConfirmation` 决定。
- `claims.go` 的 `rememberTx`：按「主体 + 属性 + 文字」算指纹去重；主体按「名字 + 这份资料」找实体，找不到就建一个 `unknown` 实体。新陈述的版本记录只写了默认值，没有说话时间。
- `source_context.go` 的 `adjacentContext`：给模型最多六条相邻消息用来解指代。

## 要交付什么

### 1 提示词和输出（R1、R2）

- `extractedItem` 增加 `people`、`places`、`organizations`、`when`。提示词里说明这几个字段、说明 R2 的「一句话既是待办也是计划时两样都要」、把说话时间和用户时区交给模型并说明相对时间按它换算。
- 提示词保持现有的那些限制（不执行原文指令、区分引用和推断、不把过去的话当现在的事实），不要因为加字段把它们挤掉。
- 一次调用，不为人、地点、时间另外再调模型。

### 2 主体和实体（R3、R4）

- 新文件 `entities.go`：取或建用户本人实体的函数；按「类型 + 名字」找到或新建实体的函数（规则见数据约定第 3.2 节）。并发下不能建出两个同名同类型的实体，也不能建出两个本人实体。
- `rememberTx` 用它们决定主体；写入 `claim_mentions`。
- 名字必须出现在正文或相邻消息里这一条，在代码里校验，不靠提示词。

### 3 时间（R5、R6）

- 新陈述的 `expressed_at` 按 R5 写。
- `when` 按 R6 校验后写进 `event_from`、`event_to`、`event_precision`。取整按用户设置里的时区；夏令时切换的那一天也要是完整的一天。

### 4 可用状态（R7）

改 `extractionConfirmation`：按 R7 判断。现有的限定词检查（`qualifiedCapture` 一组）继续用。

### 5 重复处理（R8）和处理记录（R9）

- 按「同一份资料、同一段原话片段」找到已有记忆时走 R8 的补写分支。
- 每份资料版本处理完写 `source_extractions`。分段处理的长资料，注意「最后一段做完才算完」和各段并发完成时不丢计数。分段的长度和步长不改（12000 和 11000）；`items` 记不重复的记忆条数（R9）。
- 处理中资料被删或有了新版本（X16）：不留下任何东西。现有的 `currentExtractionSource` 检查继续有效，新写的实体和提及也要在同一个事务里。

### 6 归档里 AI 说的话（R10 新增的那一条）

上传归档里 AI、系统、工具角色的消息：不调模型，直接写处理记录 `empty` 并完成任务。不是归档里的资料（例如对秘书说的话）不受影响。

### 7 错误类型（R12 里归你的部分）

`ProcessExtraction` 里：没有配置抽取模型时返回类型为 `provider_not_configured`、不可重试的错误；调用模型时通道暂时不可用返回 `provider_unavailable`、可重试。`worker.JobError` 这个类型归 E2，它会加字段；你只按现有字段用，需要新字段时和 E2 通过协调者对齐。

## 你独占的文件

`internal/postgres/processing.go`、`claims.go`、`source_context.go`、`entities.go`（新）。可以给 `entities.go` 配自己的单元测试 `entities_test.go`。

不要动：`memories_read.go`（M 的）、`worker.go` 和 `jobs.go`（E2 的）、任何验收测试、已有测试的预期。

## 第 1 批的规则别碰坏

整轮撤销的那句话不抽记忆（`deskTurnFullyUndoneTx` 那一段）、上传归档不建今天的待办（`imported`）、三类系统记录不抽取。这些分支原样保留，X12、X13 会验。

## 交付

Draft PR。说明里写：每条规则对应的代码位置；新旧提示词的差别；并发建实体是怎么防重复的；`make check` 结果；需要改预期的已有测试清单（不要自己改）；用实际入口（对秘书说话、速记、上传归档）配假模型各走一遍的结果。
