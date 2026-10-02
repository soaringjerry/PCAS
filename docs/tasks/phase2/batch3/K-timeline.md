# 任务 K：时间轴卡片的内容和「后来怎样了」（后端）

执行者 6.1 Sol。分支 `phase2/b3-K-timeline`，从 `origin/phase2/batch3` 建；工作区 `/root/PCAS-wt/b3-K`；Draft PR 的 base 是 `phase2/batch3`。

先读 [并行方案与数据约定](../parallel.md) 第 3.4 节和 [第 3 批契约](README.md) 的 R14–R16 和序列 K1–K11。

## 现在的代码

`desk_cards.go` 的 `secretaryCardsTx`（S0 从 `desk_turn.go` 搬过来的）：按模型的 `used` 生成依据卡片；每条带日期的记忆同时进时间轴，日期取资料的记录时间；状态是到 `work_items` 里找「来源里有同一份资料」的第一个事项；两条以上才出时间轴卡片。

## 要交付什么

### 1 条目内容（R14）

`at` 用记忆的说话时间；记忆自己没有时按 R14 的办法从它出自的资料取，取不到就留空（K5、K11）；带上事件时间和提到的人、地点。排序按 R14。原话条目照旧不进时间轴。

### 2 状态（R15）

- 关联事项这样找：这条记忆的证据出自哪份秘书原话 → 那份原话对应哪一轮对话（`desk_turns.request_id` 等于资料的 `external_id`）→ 那一轮建了哪些事项。「建了哪些事项」以这一轮保存的创建回执（`response.turn.receipts` 里新建待办、想法、项目的那几条，带 `thingId`）为准，它们长期保留；`action_log.changes` 30 天后清空，只能用来辅助校验，不能作为唯一依据（K9）。
- 事项已经不存在的（被撤销删掉了）不算关联事项。
- `changed` 看这条记忆有没有「改变」或「纠正」类的新版本。
- 读这些不要对每个条目各查一遍库。

### 3 什么时候出卡片（R16）

`secretaryCardsTx` 需要知道这句话是不是回忆类的。给它加一个参数（或一个选项结构），并把 `desk_turn.go` 里调用它的那一行改成传占位值 `false`。**你只能动 `desk_turn.go` 的这一行**；真实的值由 Q2 传。

## 你独占的文件

`internal/postgres/desk_cards.go`、`internal/workspace/desk.go`。外加 `desk_turn.go` 里那一行调用。

## 交付

Draft PR。说明里写：每条规则对应的代码位置；找关联事项的查询；`make check` 结果；需要改预期的已有测试清单（时间轴日期从记录时间改成说话时间，可能影响旧断言，列出来，不要自己改）。
