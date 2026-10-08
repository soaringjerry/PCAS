# F8：各页面按工作区时区显示

> **Historical task record.** This text records an earlier phase or investigation.
> Use the [current documentation](../../README.md) and [project status](../../status.md) for new work.
> Its old assignments, limits, and precedence statements do not govern current development.

执行者：6.1 Sol。先读 [执行分工](dispatch.md)、[设计原则](../../design/principles.md) 与 F6 的 [任务要求](../phase1/postlaunch.md)。

## 问题和目标

浏览器为 Australia/Melbourne、工作区为 Asia/Shanghai，任务截止为 `2026-10-03T15:00:00Z`：首页显示 10 月 3 日 23:00，秘书任务卡片和事项详情却显示 10 月 4 日 01:00。修复所有仍在使用的同类显示入口，保持已有日期文案风格。

## 规则

- 统一使用 `state.settings.timezone ?? 'UTC'`；读取工作区的组件显式把时区传入公用日期函数，不使用可变的全局时区。
- UTC 存储、已有提醒时刻、历史回执字符串均不重写。动态卡片、来源日期、事项信息行、项目任务列表和后台作业时间随设置变化。
- 复用 `web/src/domain/time.ts`；按工作区的日历日期判断今天/明天/昨天、星期及年份，包含跨日、跨年和夏令时。不要把一天固定为 24 小时来推算日历日。
- 相对时间如“3 分钟前”不受时区影响；退回具体日期显示时使用工作区时区。
- 仅调整时间显示和日期分组；不重做布局、按钮、工作流、模型输出或后端时间解释。

## 文件归属

- `web/src/domain/time.ts`、`web/src/domain/lines.ts`。
- `web/src/pages/ThingPage.tsx`、`HallPage.tsx`、`LibraryPage.tsx`：仅日期/时间显示调用。
- `web/src/components/SecretaryCards.tsx`、`Marks.tsx`、`SourceSheet.tsx`、`ConnectorSettings.tsx`、`ChatGPTConnection.tsx`：仅日期/时间显示调用。
- `web/tests/timezone.spec.ts`、`web/tests/timezone-backend.spec.ts`。
- 报告 `docs/evaluations/2026-10-01-timezone-displays.md`。
- 报告附件 `docs/evaluations/2026-10-01-timezone-displays/`：仅必要截图和不含秘密的简短验证记录，不提交缓存或大体积日志。

其他文件只读。若有活跃入口必须改上述列表外文件，先报告协调者。未使用的 DateTimePicker 不在本次范围，不顺带重构。

## 已有草稿

`/root/PCAS` / `fix/timezone-displays` 上 `web/tests/timezone.spec.ts` 有协调者留下的未验证草稿。执行者完整接手，可修订或替换；不能把草稿当作测试通过的证据。其余已有修改先核对，发现外来修改不要覆盖。

## 验收序列

| 编号 | 操作 | 预期 |
|---|---|---|
| Z1 | 浏览器墨尔本、设置上海；同一任务依次看首页、秘书卡片、详情、项目列表 | 时间、今天/明天、提醒日期一致 |
| Z2 | 切换设置到墨尔本，依次访问并刷新 | 各动态显示更新；任务与提醒 UTC 值不变；历史回执逐字不变 |
| Z3 | 夏令时前后与跨年；包含同一瞬间在两时区分属两日/两年 | 具体日期、星期、相对日期正确 |
| Z4 | 事项等待跟进、截止临近；在项目列表查看 | 分组与首页按同一工作区日历判断 |
| Z5 | 桌面 1440 与手机 390 宽重复 Z1/Z2 | 不产生重复回执，无前端异常 |

先证明新增回归在未修实现上失败，再实现。完成 lint、type-check、build、相关 mock 回归；运行已有真实后端时区用例并补跨页面断言。不需要重复全量三轮黄金路径；完整集成在 A1 做一次。

完成后提交并开 PR 到 main，返回报告和截图。不得合并、部署或修改其他任务文档。
