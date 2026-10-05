# 第 2.5 阶段第 1 批：分组和类型（契约）

2026-10-04。所有执行者先读完本页，再读自己的任务包。本页和任务包冲突时以本页为准，并立刻告诉协调者。上位文档：[阶段入口](../README.md)、[记忆架构](../../../memory-architecture.md) 第 3、4 节。

## 1 这一批做什么

给每条记忆补上三样标签，由后台自动完成：

- **分组**：这条记忆关于哪个项目、哪个主题、哪个生活领域。
- **类型**：身份、口味、对助手的要求、目标、进展、一次性的事、看法、关于别人。
- **是否长期成立**：半年后大概率还成立，还是当时的情况。

不做：合并重复、判断新旧、现状卡、改秘书的上下文。那些是后面几批。

## 2 数据约定（冻结，要改先找协调者）

### 2.1 迁移 035（O1 写，别人不新增迁移）

`035_memory_organize.sql`，只加不改已有数据：

| 对象 | 内容 |
|---|---|
| `claim_revisions` 加两列 | `category text NOT NULL DEFAULT 'unknown'`，取值 `identity`、`taste`、`rule`、`goal`、`progress`、`event`、`opinion`、`other_person`、`unknown`；`durable boolean`（可空，空表示还没判断） |
| `claims` 加两列 | `organized integer NOT NULL DEFAULT 0`（这条记忆被第几版整理规则处理过，0 是没处理过）；`organize_attempts smallint NOT NULL DEFAULT 0` |
| `claim_mentions.role` 的取值 | 在 `person`、`place`、`organization`、`thing` 之外增加 `project`、`topic`、`area` |
| `model_usage.purpose` 的取值 | 在现有取值（含 `vision`）之外增加 `organize` |
| 索引 | `claims (owner_id, organized)`；`claim_revisions (owner_id, category)` |

类型的中文叫法（只在界面和提示词里用）：identity 身份、taste 口味、rule 对助手的要求、goal 目标、progress 进展、event 一次性的事、opinion 看法、other_person 关于别人。

### 2.2 分组就是实体

项目、主题、领域和人一样，是 `entities` 里的对象，`entity_type` 分别是 `project`、`topic`、`area`。一条记忆通过 `claim_mentions` 连到它们，`role` 和实体类型相同。

- **同一个分组的判断**和现有实体一样：同一种类型里，名字去掉首尾空白、不分大小写后和某个已有实体的任一别名完全相同，就是同一个；否则新建。不做模糊合并。
- **分组的一句说明**放在 `entity_versions.disambiguation` 的 `description` 里，不超过 60 个字。
- **领域的初始值**（每个用户第一次整理时建好）：学业、工作、创业、技术、健康、财务、居住、饮食、出行、关系、兴趣。
- **项目的初始值**：这个用户所有没完成的事项项目（`work_items` 里 `kind='project'` 且状态不是完成），各建一个 `project` 实体，名字是项目标题，`disambiguation.work_item_id` 记事项的 ID。之后新建的事项项目，在下一次整理开始时补建。
- 整理**不写** `claim_revisions.scope` 里的 `project_id`。那个字段会让记忆在别的项目里查不到，这一批不碰它。

### 2.3 接口里的记忆对象

`workspace.Memory` 增加，旧字段不变（旧字段 `kind` 仍然是性质）：

```json
{
  "category": "rule",
  "durable": true,
  "groups": [ { "entityId": "…", "name": "季度汇报", "type": "project" }, { "entityId": "…", "name": "工作", "type": "area" } ]
}
```

接口照库里存的返回，不看 `organized`。从没整理过的记忆：`category` 是 `"unknown"`，没有 `durable` 字段，`groups` 是空数组。整理过、后来因为纠正（R8）或规则升级（R14）又变成落后的记忆，继续返回它保留着的旧类型、旧的长期与否和旧分组，直到新结果写入。人、地点、机构仍在原来的 `mentions` 里，不进 `groups`。

### 2.4 接口

| 接口 | 变化 |
|---|---|
| `GET /v1/workspace/memories` | 增加查询参数 `group`（分组的实体 ID）和 `category`；和已有参数是「并且」的关系 |
| `GET /v1/workspace/memory-facets` | 返回里增加 `groups`：`[{entityId, name, type, count}]`，按类型、再按条数从多到少；只列至少有一条当前有效记忆的分组 |
| `GET /v1/workspace` | 快照里增加 `organize: {done, total, version}`：当前有效的记忆里已按最新规则整理的条数、总条数、最新规则版本号 |

## 3 规则

**整理什么**

- R1 整理对象是每条当前有效的记忆（记录状态有效、取当前版本）。`claims.organized` 小于程序里的当前规则版本 `OrganizeVersion`（这一批是 1）的，叫**落后的记忆**。剩余工作就是全部落后的记忆，不另外记进度。
- R2 一次模型调用处理一批落后的记忆，最多 40 条。对每条输出：类型（一个）、是否长期成立、分组（项目最多 1 个、主题最多 2 个、领域最多 1 个，可以都没有）。
- R3 分组从现有词表里选（这个用户已有的 project、topic、area 实体的名字和说明）。都不合适时可以新建**主题**或**项目**，一批最多新建 3 个；名字 2–12 个字，不能是代词或「这个项目」这类指代；领域不新建，只在初始值里选或不选。模型给出词表里没有的领域名时，这一项丢弃，其余照写。
- R4 新建的分组名必须和这批记忆的文字有关：名字（或它去掉「项目」「计划」等后缀后的部分）要在这批里至少一条记忆的文字中出现。不满足的丢弃。这是为了不让模型自造概念。

**怎么写**

- R5 原地补写：更新当前版本那一行的 `category`、`durable`，替换这个版本上 role 为 project、topic、area 的提及（人、地点、机构的提及不动），把 `claims.organized` 设为当前规则版本。**不产生新修订，不改版本号，不让任何依赖这条记忆的回答、副手结果、摘要、训练样本变成过期。**
- R6 一批的结果和这批每条记忆的 `organized` 在同一个事务里写。写了一半失败等于没写。
- R7 模型调用期间，某条记忆被纠正（版本变了）、删除或撤回：这一条跳过，批里的其他条照写。被纠正的那条因为 `organized` 没更新，下次还会被整理。
- R8 纠正一条记忆产生新版本时，新版本先沿用旧版本的 `category`、`durable` 和分组提及，同时把 `claims.organized` 置 0，等下一次整理重新判断。
- R9 模型输出的处理：
  - 编号不在这一批里的条目：直接丢弃，不影响任何记忆的计数。
  - 编号对得上、但类型不在取值里，或「是否长期成立」不是布尔值的条目：整条丢弃，这条记忆 `organize_attempts` 加 1，仍然落后。分组里个别不合规的（R3、R4）只丢那个分组，不算这条失败。
  - 这一批里有、但输出里没出现的记忆：`organize_attempts` 加 1，仍然落后。
  - 整个输出不是合法 JSON：这一批每条都加 1。
  - 同一条记忆在输出里出现两次：用第一次的。

 一条记忆 `organize_attempts` 到 3 时，写成类型 `unknown`、无分组、`organized` 设为当前版本，不再重试，并记一条不含正文的日志（环节 `organize`，类型 `attempts_exhausted`）。

**什么时候做**

- R10 顺序：先整理不是从归档导入的记忆（对秘书说的、随手记的、Telegram 来的），再整理导入的；各自按新的在前。
- R11 新说的话抽出的记忆，在抽取完成后 10 分钟内被整理（模型通道可用的前提下）。
- R12 后台整理和抽取用同一个模型通道，一次只跑一批，优先级低于新输入的抽取和整段对话的整理，高于索引。每小时最多 120 批（2026-10-05 由 30 调高，和第 2 批的比较合计，见 [并行方案](../parallel.md) R2-6）。通道暂时不可用时等待重试，不计入 `organize_attempts`；没有配置模型时停住，不重试，快照里的 `organize` 照常显示进度。
- R13 每日额度不够时推迟到下一个额度周期，和抽取的做法相同。
- R14 把 `OrganizeVersion` 改大，所有记忆都变成落后的，按 R10–R13 重新整理；旧的类型和分组在新结果写入之前保持不变，不出现「全部变成未整理」的空窗。

**记录和删除**

- R15 每次模型调用成功返回就记一条 `model_usage`：用途 `organize`，`memory_refs` 是这批记忆的引用，不含正文。解析失败也记（已经花了钱）。同一次调用不记两条。
- R16 删除一条记忆，它的分组提及跟着没（外键级联）。一个分组下最后一条记忆被删掉后，分组实体留着，但不出现在 `facets` 里。删除不因为分组而多删任何东西。

**界面**

- R17 资料库的记忆列表可以按分组翻：项目、主题、领域各一组，显示名字和条数，点一个只看它下面的记忆。记忆卡片上显示它的分组。
- R18 还在整理时，显示一行「已整理 N / M」；全部整理完不显示。不出现「规则版本」「落后」「批」这些词。
- R19 类型这一批不做成筛选控件，只在卡片上用中文叫法显示（`unknown` 不显示）。卡片和详情里，一条记忆有了类型就只显示类型，不再同时显示原来的性质（事实、偏好……）；还没有类型的照旧显示性质。按性质筛选的那一行控件不变。

## 4 操作序列

T1 按这些序列写测试。模型用测试里的假模型，按序列需要返回内容。

| # | 操作 | 预期 |
|---|---|---|
| X1 | 三条落后的记忆 → 整理一次，模型给出类型、长期与否、已有分组 | 三条的 `category`、`durable`、分组提及写入；`organized=1`；版本号不变；没有新修订 |
| X2 | X1 之后查依赖这些记忆的一轮秘书回答和一次副手结果 | 不是「依据已更新」，不是过期 |
| X3 | 45 条落后的记忆 → 整理 | 分两次模型调用（40 + 5）；两次都完成后全部 `organized=1` |
| X4 | 模型为一条记忆给出词表里没有的主题「季度汇报」，这条记忆的文字里有「季度汇报」 | 新建 topic 实体并连上；`facets.groups` 里出现它，count 1 |
| X5 | 模型给出新主题「效率提升」，这批记忆的文字里都没有这几个字 | 不新建；这条记忆的其他标签照写 |
| X6 | 模型给出领域「宇宙探索」（不在初始值里） | 领域丢弃，其余照写 |
| X7 | 一批里模型新建了 5 个主题 | 只建前 3 个；后两个丢弃 |
| X8 | 模型调用期间，批里的一条记忆被用户纠正 | 这一条不写，仍然落后；其他条照写；纠正后的新版本沿用旧标签 |
| X9 | 模型调用期间，批里的一条记忆被删除 | 这一条不写，不报错；其他条照写 |
| X10 | 模型返回的不是 JSON，连续三次 | 第三次后这批每条：`category='unknown'`、无分组、`organized=1`；日志里有 `attempts_exhausted`；之后不再为它们调用模型 |
| X11 | 一批四条记忆 A、B、C、D。模型输出：A 正常；B 的类型不认识；没有 C；D 正常；另有一条编号不在这批里 | A、D 照写、`organized=1`。B、C 不写，`organize_attempts=1`，仍落后。不在这批里的那条丢弃，不影响任何计数 |
| X12 | 写结果的事务中途失败（模拟） | 这批没有任何一条被写；重跑后结果完整，没有重复的提及 |
| X13 | 同时有一条对秘书说的话抽出的记忆和 100 条导入的记忆落后 | 第一批里包含对秘书说的那条 |
| X14 | 全部整理完后把规则版本改成 2 | 快照 `organize.done` 变成 0、`total` 不变；每条记忆的旧类型和分组仍然读得到；重新整理后 `organized=2` |
| X15 | 删除一个主题下唯一的一条记忆 | 记忆和它的提及没了；该主题不在 `facets.groups` 里；其他记忆不受影响 |
| X16 | 没有配置抽取模型 | 不调用模型，不报错循环；快照显示 `done=0`；配置好之后自动开始 |
| X17 | 通道暂时不可用两次后恢复 | 这批记忆的 `organize_attempts` 仍是 0；恢复后正常整理 |
| X18 | 一次调用成功、一次调用返回坏 JSON | `model_usage` 各一条，用途 `organize`；`memory_refs` 是这批的引用，不含文字 |
| X19 | 用户有两个没完成的事项项目，第一次整理 | 建了两个 project 实体，带 `work_item_id`；11 个领域实体；重复整理不重复建 |
| X20 | 整理把一条记忆归到项目 A | 这条记忆的 `scope.project_id` 没有被写；在项目 B 的事项里照样查得到 |
| X21 | `GET /v1/workspace/memories?group=<主题>&category=rule` | 只返回同时满足的；分页和总数正确 |
| X22 | 两个 worker 同时整理同一个用户 | 没有一条记忆被整理两次（`model_usage` 里不出现同一条记忆属于两批的情况）；没有重复的分组实体 |

## 5 文件归属

| 任务 | 可以动的文件 |
|---|---|
| O1 | `internal/postgres/migrations/035_memory_organize.sql`（新）、`internal/postgres/organize.go`（新）、`organize_test.go`（新，自己的单元测试）、`internal/postgres/memories_read.go`、`internal/postgres/editing.go`（只为 R8）、`internal/postgres/entities.go`（只为让 `entityTx` 接受 project、topic、area）、`internal/postgres/workspace.go`（只为快照的 `organize`）、`internal/postgres/usage_log.go`（只为新用途）、`internal/workspace/model.go`、`internal/httpapi/workspace.go`、`cmd/pcas/main.go`（只注册后台任务） |
| T1 | `internal/postgres/phase2_5_b1_*_test.go`（新）；最终一轮的验收记录 `docs/evaluations/<日期>-phase2_5-batch1-acceptance.md`（新），以及在 [阶段入口](../README.md) 的文档表和 [文档总入口](../../../README.md) 里各加一行链接 |
| U1 | `web/src/` 下资料库记忆页相关的文件、对应的浏览器测试；`.github/workflows/browser-regression.yml` 里把自己的测试文件加进模拟数据那一行 |

表里没有的产品文件，要动先找协调者。已有测试的预期不要改；确实要改的单独列出来报告。

## 6 分支

集成分支 `phase2_5/batch1`，从 main 建。每个任务的分支从它建，Draft PR 的 base 是它。O1 分两个 PR：先交骨架（迁移、类型、接口字段，行为不变），合进集成分支后 T1 和 U1 再开工；再交整理本身。

## 7 契约的修订记录

| 日期 | 改了什么 | 起因 |
|---|---|---|
| 2026-10-04 | 第 5 节：O1 可以改 `entities.go`，只为扩展三种类型；第 8 节记下模型输出的格式 | O1b 的提问 |
| 2026-10-04 | 2.4 的分组统计接口路径更正为现有的 `/v1/workspace/memory-facets`；R19 写明卡片上有了类型就不再并排显示性质；U1 可以在浏览器回归的工作流里加上自己的测试文件 | U1 交付时的三个问题 |
| 2026-10-04 | 2.3 写明接口照库里存的返回，落后的记忆继续返回旧标签；R9 分清五种情况各怎么计数；X11 改成具体的四条；T1 可以新增验收记录并加链接；2.1 写明保留 `vision` | T1 的发现 F-B1-1、F-B1-2、F-B1-3；O1a 交付 |

## 8 模型输出的格式（O1b 定，2026-10-04）

T1 的假模型照这个返回：

```json
{
  "items": [
    { "n": 1, "category": "rule", "durable": true, "project": "", "topics": ["季度汇报"], "area": "工作" }
  ],
  "new": [
    { "type": "topic", "name": "季度汇报", "desc": "季度汇报的准备和提交" }
  ]
}
```

`n` 是批内序号，从 1 开始。`durable` 必须是布尔值。`project`、`topics`、`area` 和 `new` 都可以省略，表示没有对应的分组或没有新建。同一个 `n` 出现两次取第一次。
