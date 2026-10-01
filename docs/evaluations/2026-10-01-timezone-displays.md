# F8：跨页面工作区时区显示验收

2026-10-01。基线 `c94b496`，分支 `fix/timezone-displays`。执行者接手并验证了协调者留下的 66 行测试草稿，新增独立回归后才修改产品实现。没有合并或部署。

## 观察与修复

浏览器 `Australia/Melbourne`、工作区 `Asia/Shanghai`、现在 `2026-10-03T14:30:00Z`、截止 `2026-10-03T15:00:00Z`。未修实现的实际值：

| 入口 | 预期 | 观察到的错误 |
|---|---|---|
| 首页 | `23:00` | 正确 |
| 秘书任务卡片 | `今天 23:00` | `今天 01:00` |
| 来源时间线 | `今天` | `昨天` |
| 事项信息行 | `今天 23:00 截止`、`22:45 提醒` | `今天 01:00 截止`、`00:45 提醒` |
| 项目任务列表 | `今天 23:00 截止` | `今天 01:00 截止` |
| 等待事项的项目分组 | `等对方回复 · 明天跟进` | `该跟进了：对方还没回` |
| 临近截止的项目分组 | `明天截止` | `今天 23:30 截止` |

独立扩展的修复前用例共 8 项，4 失败、4 通过。两种宽度都复现全部跨页面显示问题；另有上海跨年日期失败。修复前跨年检查通过浏览器直接加载时间模块，最终改为渲染秘书卡片的字面日期断言，使同一预期也能验证正式构建产物。失败日志保留在 `/tmp/pcas-f8/before-expanded.log`，含上述实际值与 trace；初次启动失败的环境错误没有计入回归证据。

修复集中在原有 `time.ts`：按传入时区计算日历日、日期字段、星期、年份和时钟；复用短日期、事项信息行和完整时间戳的现有文案风格。所有活跃入口显式传入 `state.settings.timezone ?? 'UTC'`，没有可变全局时区。项目 `urgentLine` / `ongoingLine` 的跟进和截止判断使用相同日历。

覆盖任务卡片、来源卡片/时间线、事项截止和提醒、项目任务列表、事项/首页/资料库相对时间的具体日期退路，以及来源原文记录时间、接入最近同步和账户额度重置时间。未改布局、后端、UTC 存储、提醒解释或历史回执。

## 验收序列

| 编号 | 已通过证据 |
|---|---|
| Z1 | 墨尔本浏览器、上海工作区：同一事项在首页、卡片、详情和项目列表一致；提醒 `22:45`；来源时间线 `今天`。 |
| Z2 | 切换墨尔本后各动态显示更新，刷新与逐页访问保持一致；mock 和真实后端均断言 UTC due / nextAt 不变、历史回执逐字不变。 |
| Z3 | 墨尔本春季 23 小时日、秋季 25 小时日；同一瞬间在上海为 12 月 31 日、墨尔本为 1 月 1 日；具体日期、星期、跨年年份正确。额外覆盖春季跳时两侧的提醒 `01:45` / 截止 `03:15`。 |
| Z4 | 项目等待跟进、临近截止与首页使用同一日历；切换设置前后分组和日期文案正确。 |
| Z5 | 桌面 1440 与手机 390 均重复 Z1/Z2；刷新后历史回执只有一条；前端 pageerror 数组为空；保存两时区截图。 |

## 验证命令与结果

Node 使用 `/root/.nvm/versions/node/v22.23.3/bin`，没有改变系统默认版本。

- `make check`：通过（fmt-check、go vet、race 单元测试、Go 构建）。无数据库产品改动，未另跑全量 PostgreSQL 集成测试。
- `cd web && npm run lint && npm run type-check && npm run build`：通过；最终新增测试后再次 lint / type-check 通过。
- 正式构建产物、临时 API 上运行 `npx playwright test tests/timezone.spec.ts tests/secretary.spec.ts tests/fixes.spec.ts tests/notify.spec.ts tests/buttons.spec.ts`：54/54 通过。之后新增三项跨日提醒/缺省 UTC 检查，最后在 Vite preview 正式构建产物上运行完整 `tests/timezone.spec.ts`：18/18 通过、零 skip。
- 在独有 tmpfs PostgreSQL 上，复制既有 `real-backend.sh` 到本任务临时目录，只调整固定工作目录、F8 容器名和端口，运行补强的 `tests/timezone-backend.spec.ts`：1/1 通过、零 skip。

真实后端覆盖首次登录时区、保存/校验、提示、秘书创建与读取真实卡片、四页面切换、刷新、UTC 字段和历史回执；外部模型及通知服务为本地假服务。mock 覆盖两种视口、夏令时/跨年/提醒跨日、项目跟进、来源及设置附属时间入口，并通过具体文案与错误计数断言。此次没有真实模型账户、设备推送、Telegram 或生产渠道验收，也未替代 T2/A1。

## 证据与资源

本机证据保留在 `/tmp/pcas-f8`：

- `before-expanded.log`：修复前独立失败。
- `mock-final.log`、`timezone-final.log`、`real-final.log`、`make-check.log`：通过日志。
- `timezone-final/timezone-workspace-timezon-e2fca-tails-and-project-at-1440px/{shanghai,melbourne}.png`：桌面两时区。
- `timezone-final/timezone-workspace-timezon-be507-etails-and-project-at-390px/{shanghai,melbourne}.png`：手机两时区。
- `real-final`：真实后端截图与附件。

临时 API 使用 18139，开发/preview 使用 18138。真实 runner 已删除自己的 `pcas-test-F8-*` tmpfs 容器、卷和运行目录，停止记录的 fixture/API/worker 进程；preview 已在完成最终测试后停止。保留约 12 MB 的日志、截图和失败 trace 供审查。生产容器、其他任务目录和默认模型账户均未连接或变更。

未使用的 DateTimePicker 按任务要求留在范围外。未发现需要扩大文件归属或新增后端契约的事项。
