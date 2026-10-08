# 任务 E3：对秘书说的话，整理时带上这段对话的上文（后端）

> **Historical task record.** This text records an earlier phase or investigation.
> Use the [current documentation](../../../README.md) and [project status](../../../status.md) for new work.
> Its old assignments, limits, and precedence statements do not govern current development.

执行者 6.1 Sol（做第 2 批后端的执行者）。分支 `phase2/b2-E3-conversation-context`，**从 `origin/main` 建**（第 2 批已经在 main 上）；工作区 `/root/PCAS-wt/b2-E3`；Draft PR 的 base 是 `main`。

先读 [第 2 批契约](README.md) 第 13 节（R23 和序列 X19–X22）。

## 现在的问题

用户对秘书说：「下周去成都。」秘书答了之后，用户接着说：「改到下下周吧，把老王也叫上。」

秘书回答第二句时看得到上文。但后台把第二句整理成记忆时，只把这一句话单独交给模型：模型不知道「改」的是什么、去哪里。结果要么抽不出东西，要么抽出一条没头没尾的记忆。

原因：给模型解指代用的「相邻消息」（`source_context.go` 的 `adjacentContext`）只对导入的聊天记录生效，它靠 `source_contexts` 里的对话归属找上下文；对秘书说的话没有写这张表，所以拿到的相邻消息永远是空的。

## 要交付什么

按 R23：整理一句对秘书说的话（来源类型 `desk`、`desk-incomplete`）时，把同一段对话里它**之前**最多 6 轮的问和答交给模型当相邻消息。

- 上文从 `desk_turns` 取：这句话的 `external_id` 就是那一轮的请求 id，由它找到所在的对话和更早的轮次。不要为此往 `source_contexts` 里补写数据，也不改秘书那条路。
- 每一轮给出用户说的话和秘书的回答，各取前 1200 个字符，标明是谁说的。
- 被清空的轮次不给。标了「依据已更新」的轮次：用户说的话照给，秘书的回答换成第 1 批的那句替换语。
- 相邻消息只用来理解这句话：记忆的依据（`quote`）仍然必须逐字出自这句话本身；人名、地名可以出自相邻消息（R4 本来就允许）。
- 补做旧资料时同样带上文。
- 不增加模型调用次数。提示词里现有的各项限制不变。

## 你独占的文件

`internal/postgres/source_context.go`、`processing.go`。可以给自己新写的函数配单元测试。

不要动：`desk_turn.go`、`desk.go`（第 3 批的），验收测试，已有测试的预期。

## 交付

Draft PR 进 `main`。说明里写：R23 对应的代码位置；取上文的查询；用实际入口加假模型验证「下周去成都」「改到下下周吧，把老王也叫上」两轮的结果（第二句的整理请求里带着第一轮）；`make check` 结果；需要改预期的已有测试清单。
