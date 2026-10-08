# 任务 T1：Telegram 里的回复不再缺斤少两

> **Historical task record.** This text records an earlier phase or investigation.
> Use the [current documentation](../../README.md) and [project status](../../status.md) for new work.
> Its old assignments, limits, and precedence statements do not govern current development.

2026-10-03 用户反馈（附了一段真实对话），由协调者直接实现。

## 之前的问题

用户在 Telegram 里说「你仔细研究一下 pcas 呢」，秘书把研究交给了副手。机器人回的是：

- 一条「✓ 交给 ChatGPT 订阅：……」，后面跟着秘书写给副手的整段要求，写到 240 个字被截断成「…」。
- 两行光秃秃的 `github.com`，点不了，也看不出是什么。
- 副手做完之后，Telegram 里什么都没有。要打开网页找到那件事才看得到结果。

## 规则

**R1 交办的回执。** Telegram 里只说「✓ 交给某某，做完会发到这里」。写给副手的那段要求不贴出来（网页上照旧能看到）。没交成的回执照原样说明原因。

**R2 链接。** 显示完整地址，可以直接点。没有地址只有站点名的，才显示站点名。

**R3 做完了发回来。** 副手每做完一次（不管是从哪里交办的），产生一条通知，走提醒的同一套通道（首页置顶、浏览器推送、Telegram）：

- Telegram 收到「📄 这件事的标题」、结果的前 600 个字、打开这件事的链接。没做成时收到「副手没做成：原因」。
- 首页置顶那一行写「副手做完了」和时间，不写「到点」；只有这类通知时，这一组的标题是「做完了，等你看」。点叉关掉，和关提醒一样。
- 同一次只发一遍。这类通知不算进「你不在的时候」里的「提醒了你」。
- 受同一个开关管：设置里「事项到时间提醒我」关着时不发；这件事已经做完或取消时不发。

## 数据

- 副手一次运行结束时写一行 `workspace_notices`，`trigger_id` 是 `run:<运行编号>`，`reason` 是「副手做完了：」加结果的前 600 个字，或「副手没做成：」加原因。没有迁移。
- 快照里的通知多一个 `result: true`；发给各通道的消息多一个 `Result`。

## 测试

- `internal/telegram/format_test.go`：交办回执不带要求原文、不出现截断号；链接是完整地址。
- `internal/postgres/run_notice_test.go`：运行结束后有一条结果通知；只发一次；消息里有标题、结果正文和链接；不出现在「提醒了你」里。
- `web/tests/settings-things-ux.spec.ts`：首页置顶行的写法。

## 还没做的

Telegram 里的时间轴卡片只有日期和文字，没有「后来怎样了」和提到的人、地点；待办卡片没有状态。这些留到做 Telegram 完整体验时一起处理。

## 上线记录

2026-10-03 上线（main `ccc3150`）。自动测试通过；还没有在真实的 Telegram 对话里交办一次并等结果发回来，等用户下次使用时确认。
