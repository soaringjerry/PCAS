# 任务 L：记下每次调用用了哪些记忆、花了多少（后端）

> **Historical task record.** This text records an earlier phase or investigation.
> Use the [current documentation](../../../README.md) and [project status](../../../status.md) for new work.
> Its old assignments, limits, and precedence statements do not govern current development.

执行者 6.1 Sol。分支 `phase2/b4-L-usage`，从 `origin/phase2/batch4` 建；工作区 `/root/PCAS-wt/b4-L`；Draft PR 的 base 是 `phase2/batch4`。

先读 [并行方案与数据约定](../parallel.md) 第 3.1、3.5 节和 [第 4 批契约](README.md) 的 R12–R15 和序列 L1–L9。这是一个小任务。

## 现在的代码

S0 已经建好表 `model_usage`，在 `usage_log.go` 里放了空的 `recordUsageTx`，并在秘书、旧的导办台问答、副手运行、抽取四处拿到模型结果之后调用它。你把它填上，并提供读取接口。

## 要交付什么

### 1 写（R12、R13）

- `recordUsageTx` 往 `model_usage` 写一行。引用只存 id、版本、类型。
- 失败的调用和重放不产生记录：先确认 S0 放的四个调用点本来就只在成功返回、且不是重放时才走到；哪一处不满足，**不要自己去改那个文件**，告诉协调者，由那个文件的写入者改。
- 写这一行失败时让所在的事务失败，不要吞掉错误。

### 2 读（R14）

- 新文件 `internal/httpapi/usage.go`：两个接口。自然日按用户设置里的时区切，夏令时那一天也要对。
- 明细接口里引用的文字现查：记忆取当前版本的文字，原话取正文开头，各截 80 个字符；查不到的标成已删除。一次请求里不要对每条引用各查一遍库。

两个接口的参数和返回按契约第 6 节。

### 3 不随别的操作消失（R15）

确认撤销、清空对话轮、删除记忆都不会删到 `model_usage`。

## 你独占的文件

`internal/postgres/usage_log.go`、`internal/httpapi/usage.go`（新）。路由注册写在自己的文件里；非动 `server.go` 不可时只加一行并在 PR 里写明。

## 交付

Draft PR。说明里写：每条规则对应的代码位置；两个接口的请求和返回示例；四个调用点各自是否满足「只在成功且非重放时调用」的核对结果；`make check` 结果。
