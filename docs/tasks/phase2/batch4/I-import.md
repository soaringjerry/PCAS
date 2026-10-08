# 任务 I：ChatGPT 历史导入（后端）

> **Historical task record.** This text records an earlier phase or investigation.
> Use the [current documentation](../../../README.md) and [project status](../../../status.md) for new work.
> Its old assignments, limits, and precedence statements do not govern current development.

执行者 6.1 Sol。分支 `phase2/b4-I-import`，从 `origin/phase2/batch4` 建；工作区 `/root/PCAS-wt/b4-I`；Draft PR 的 base 是 `phase2/batch4`。

先读 [并行方案与数据约定](../parallel.md) 和 [第 4 批契约](README.md) 的 R1–R11 和序列 I1–I14。

## 现在的代码

- `internal/connectors/archive.go`：`DecodeArchive` 把整份文件读进内存，解 zip、解 JSON，认得 ChatGPT 的 `conversations` 结构，产出一批记录。上限写成常量。
- `internal/httpapi/connectors.go`：`POST /v1/connectors/archive`，上传上限 20 MB。
- `internal/postgres/connectors.go`：`ImportArchive` 把原始文件存成一份附件资料并排一个 `source.parse` 任务；后台解析时 `importBatchTx` 把每条记录存成资料，写 `archive_entries`、对话归属、说话时间、角色、分支。
- 每条资料存下后照常排分段、分词、向量、抽取任务。抽取任务的优先级和暂停时不领取由第 2 批的 E2 负责，你不用管。

这两件事在第 2 批的集成分支合进本批之前还没有：这期间 I3、I6 里「暂停后不领取」「新资料先处理」的断言不过是正常的，协调者合入后会通知你变基。

## 要交付什么

### 1 预览（R1、R2）

新接口只读不写。解析和正式导入用同一套解析代码，这样预览的数字和实际导入的一致。

### 2 吃得下大文件（R2）

- 上传上限和解压上限按契约。
- `conversations.json` 流式解析：一段对话一段对话地读，内存占用不随文件大小线性增长。zip 里别的文件跳过并记进 `gaps`。
- 条数超限时取最新的，其余计入 `leftOut`。上限写成可以在测试里调小的变量。

### 3 批次和进度（R3、R4）

- 新文件 `import_batches.go`：建批次、更新进度、读批次列表。`organized` 在读的时候按 `source_extractions` 现算。
- 后台分小批存原话（每批几百条），每批一个事务，存完更新 `stored`。从最新的对话开始。
- 做完状态变 `done`。

### 4 暂停、继续、中断（R5、R9、R10）

- 两个接口改批次状态。存原话的循环每一小批开始前看一眼状态，暂停了就停下并让出任务（任务本身不算失败）。
- 「做到哪了」要记在数据库里，进程被杀后重启能接着做，不从头来、不重复存。
- 中途出错：状态 `failed`、写明错误类型、已存的保留；`resume` 可以从 `failed` 接着做。

### 5 重复导入（R6）和删除（R11）

- 重复靠现有的「连接器 + 外部 id + 外部版本」去重。预览里的 `alreadyImported` 要按同一个规则算。
- 删除沿用现有的 `POST /v1/memory/delete`，目标是原始归档资料（契约第 6 节），不新增删除接口；批次记录靠外键级联。批次列表的每一项要带 `archiveId`、`archiveVersion`，前端和测试靠它们发起删除。确认一遍：删完之后 `import_batches`、`archive_entries`、消息资料、抽出的记忆都没有残留。

### 6 固定的形状

接口返回的字段、错误类型、四个可调的变量和环境变量 `PCAS_IMPORT_CHUNK_SIZE` 按契约第 6 节，T4 的测试会直接用这些名字。

### 7 错误（R10）

每种错误一个类型，接口返回时带一句给用户看的说明。不要所有失败都返回同一个错误。

## 你独占的文件

`internal/connectors/`、`internal/postgres/connectors.go`、`connector_runner.go`、`attachments.go`、`import_batches.go`（新）、`internal/httpapi/connectors.go`。可以给解析代码配自己的单元测试。

不要动：`jobs.go`、`worker.go`（第 2 批 E2 的）、`processing.go`（E1 的）、`editing.go`（M 的）。导入需要它们配合的地方已经写进第 2 批的契约（R10、R14）；发现不够，告诉协调者。

## 交付

Draft PR。说明里写：每条规则对应的代码位置；流式解析的做法，以及解析一份 300 MB 合成文件时的内存峰值和耗时；进度是怎么持久化的；四个接口的请求和返回示例；`make check` 结果；需要改预期的已有测试清单（上限变了，旧的上限测试会受影响，列出来，不要自己改）。

## 第二轮补充（2026-10-02）

你的两个问题的答案在契约第 7 节：`internal/blob/files.go` 批准你改，只为原始归档放开上限，普通附件不变；被禁止重新导入的消息跳过、不计入 `total` 和 `stored`、批次照常做完，已有测试的预期不变。

## 跟进任务 I2：导入时先只存原话（2026-10-03）

规则在契约第 11 节（R21，序列 I15–I20）。另开分支 `phase2/b4-I2-organize-later`，从 `origin/phase2/batch4` 建，Draft PR 的 base 是 `phase2/batch4`。

- 迁移 029（`029_import_hold_organizing.sql`）分配给你：`import_batches` 加 `hold_organizing boolean NOT NULL DEFAULT false`。
- 批准你改 `internal/postgres/jobs.go` 里领取任务的那一个条件：跳过 `hold_organizing` 为真的批次的**抽取**任务（`source.extract` 及其子段），别的环节不跳过。只改这一处。
- 导入接口的 `organize` 字段、新接口 `organize`、列表项的 `organizeLater`。
- 不带 `organize` 时按 `later`。已有的导入测试如果因此要改预期，列给协调者，不要自己改。
