# 设置控件实际行为核对（S1，2026-10-01）

基线：`59e25daa67df7447f0a09b7f0bd9e5d65edab65e`。只读源码调查；未运行测试、浏览器、服务或模型请求，未读取部署配置值、秘密或真实账户。本报告的「有效」仅指找到可追溯的实际消费者，不代表部署可用或真实服务验收通过。默认值按新建工作区/未配置服务的源码回退值说明；已有保存值仍权威。

## 给 U2 的优先结论

1. **Jev 分流是当前主界面过时配置。** 保存密钥与「试分流」会执行，但当前秘书直接调用 `/v1/desk/turn`，Telegram 也直接调用 `DeskTurn`；整个 `web/src` 没有 `/v1/desk/route` 调用。`DeskTurn` 不引用 Router。现有「导办台现在由 Jev 判断去向」「每句原文都会发给 TypeSafe」「没有密钥时按本地规则」描述不适用于当前秘书。旧 route API 仍可被外部调用，不能把“主流程无消费者”扩大成“后端接口不存在”。交协调者决定是否移除旧配置入口，不实施功能或擅删凭据。
2. **「每日整理」只是定时记录待确认数量。** 到点数 `capture_candidates.state=pending`，写 `workspace_reviews`，在活动/后台历史中显示「有 N 条拿不准的等你确认」。它不触发新的模型抽取；新资料处理独立入队。可写「待确认资料每日汇总时间」，说明「到点记录还有多少线索待确认；资料收到后另行处理」。
3. **「跟进提醒」范围比名字大。** 它是任务 trigger 生成通知的全局开关；普通到时提醒也属于 task trigger。通知统一出口又检查 `followUps=true`，因此关闭还阻止事件线索唤醒生成的 idea notice 外发。可写「事项提醒」，说明「开启后按事项中已设置的时间和状态发提醒；关闭也停止向设备和 Telegram 发送事项通知」。它不删除任务提醒规则，也不取消通知通道订阅。
4. **「自动收下」只控制资料抽取候选。** 仅新资料抽取产生的 task、`explicit=true`、置信度 ≥0.98 自动转为任务。秘书直接执行创建命令不受此开关控制。可写「资料中的明确待办直接加入任务」，说明「后台整理资料时，识别十分明确的待办后直接加入；其余线索仍待你确认」。默认关闭。
5. **启用副手≠启用所有后台模型调用。** 每模型 `enabled` 拦秘书/副手执行和旧结果重用；`ProcessExtraction` 直接取 `Registry.ExtractionID()`，不读取该 agent 开关或记忆分类。不要把「关闭这个副手」解释成彻底停止此模型的后台资料处理，也不要承诺其筛选控制原文整理授权。
6. **「记忆曝光」是排名参数。** 控制继续交流检索的衰减加分，不删除或定时隐藏记忆。应放低频高级设置；没有“X天以后就忘掉”的保证。
7. **密钥已配置≠接入已验证。** 文本/向量保存只校验输入并写文件；可用状态检测主要是密钥存在。不要把「已配置密钥」改成未经验证的「已连接成功」。
8. **待确认候选在当前路由没有管理入口。** `UnsureSheet` 是保留组件，当前没有import/挂载；其「收下/不要/对/不对」并非当前候选可点击流程。pending Candidate仍会由导入/抽取/capture产生，每日汇总也会数它，但Hall、Library、Shell、CommandPalette与当前秘书均无候选接受/忽略入口。报告原附录只查按钮→命令，漏查可达性，此处明确更正。推测记忆在资料库仍能确认/编辑/删除，不应误判所有确认功能都不可达。

建议优先呈现时区/城市、通知设备状态、模型连接和费用；把资料同步协议、凭据环境变量、向量补建、曝光公式放有明确入口的高级配置。每模型记忆授权可以分组收起，但需保留当前权限概览与显式选择，不能自动增加记忆分类、推测可见性或外发范围。以下文案为事实说明建议，不替用户决定默认值。

## SettingsPage：工作区与副手

SettingsPage 中所有 `updateSettings`/`updateAgent` 都经服务端命令持久化；普通开关并非仅浏览器保存。

| 当前文案/控件 | 保存字段与初始默认 | 实际消费者与影响 | 依赖、范围及判断 |
| --- | --- | --- | --- |
| 时区 | `workspace_owners.settings.timezone`；首次浏览器创建取请求 `X-PCAS-Timezone`，无有效初始偏好则 UTC | 后端秘书当前时间与时间解析、每日预算分日、每日汇总；前端今天/时间展示；外发通知时间 | 常用有效。IANA 时区校验，自动处理夏令时；不等同城市。已有账户不随浏览器时区自动改 |
| 所在城市（比如 上海） | `settings.city`；空串 | `desk.go`/`desk_turn.go` 注入模型提示的默认城市 | 有效但属于提示，不是定位/独立天气服务。留空时旧问答提示可推断或询问；最多60字符，去首尾空格，禁止换行 |
| 明确要求的待办自动收下 / 自动收下 | `settings.autoAccept`；false | `ProcessExtraction` 自动 `acceptCandidate` 创建任务 | 有效，限资料抽取 explicit task、confidence ≥0.98；依赖资料处理 worker、抽取入口/预算。直接秘书创建命令不读它 |
| 有相关线索时唤醒放下的想法 / 自动唤醒 | `settings.wakeIdeas`；true | `pendingConditions` 向抽取模型提供搁置想法条件；`applySignalsTx` 处理新资料线索；`CheckReminders` 检查时间/已确认条件 | 有效，需想法 shelved、`remindersOn=true`、有对应条件；新资料事件线索需精确原文引用、≥0.98置信度，变 awakened 但标记「待核验」，并不把事件条件 `Met` 置true。时间条件到点才置true。不是任意关键词/想法放久了就唤醒 |
| 按规则发跟进提醒 / 跟进提醒 | `settings.followUps`；true | `CheckReminders` 生成所有 task 已启用且到期 trigger notice；`DispatchNotices` 每通道发送前再检查全局开关；ThingPage 用它决定是否显示下一提醒时间 | 有效，已有 trigger 的 `active`、`nextAt`、状态 guard 必须满足；任务完成/取消等不发。不是自动监测任意邮件是否回复。全局出口还影响 idea notice 外发。关闭不删除规则，也不取消浏览器/Telegram连接；首页任务时间线仍会按截止/trigger显示急迫程度 |
| 副手每天最多花 / 每日额度 | `settings.dailyBudget`；¥10 | `reserveModelCost`、`requestRun` 预留检查；API秘书/副手、抽取、向量、语音转录共享已用+预留金额 | 常用有效。按所选时区每天00:00计算；服务器允许0…1,000,000，当前Stepper每次±1。按填写单价估算，不是供应商硬账单限额；Codex/SIWC订阅内部预留与结果费用为0，订阅次数/套餐额度由账户限制。模型请求预算不足会拒绝；后台job可进入blocked，调高预算不自动重排已有blocked job，需后台重试 |
| 每日整理 / 每日整理时间 | `settings.dailyReviewAt`；09:00 | `CheckReminders` 到本地时间后写当日 `workspace_reviews` 的 pending_count | 有消费者但易误导，仅待确认数汇总，不模型整理计划。后端30秒检查；UI半小时选项且保留已保存的非整点值；不会发送单独的每日汇总Web Push/Telegram（汇总不写notice） |
| 启用 {模型名} | `workspace_agents.document.enabled`；所有初始 agent true，包括手动交接 | `secretaryContextTx`、`AnswerDesk`、`prepareRunContext`、`requestRun`、`verifyRunForItemTx`；前端选择器排除关闭模型 | 有效：控制交给此副手、秘书使用和结果重用，另需真实模型可用。不会登出账户/清除密钥、撤销record_grants，也不是后台抽取停用开关。手动交接仍保留复制上下文/贴回结果流程 |
| 能看到的记忆：事实、偏好、决定、意向、计划 | `agent.memoryKinds`；五类全选 | 秘书/副手上下文筛选；已有结果/派生材料依赖校验；前端 `memoriesFor` 统计 | 有效授权筛选，仍必须该记录已有 `record_grants`、版本/来源有效、项目范围适用，并遵守事项排除；勾分类不授予某条原本不可见记忆。不是后台原文抽取分类开关 |
| 也给没确认的推测 / 包含推测 | `agent.includeInferred`；false | 同上，决定是否允许 inferred 记忆进入上下文/旧结果重用 | 有效权限扩大项，不能默默改默认。false仍可看已确认、直接原话且adopted的记忆；true包括其他未确认推断/间接资料，不是“AI可随便推测”模式 |
| 现在能看到 N 条记忆（只读） | 无新字段 | 前端按 visibleTo、种类、推测过滤计数 | 是允许范围的概览，不是本次请求必定把N条全发模型；实际还受相关性、项目、有效版本、上下文长度限制 |
| 导出全部数据 / 导出 | 无偏好；GET `/v1/workspace/export?training=false&confirmedOnly=false` | 导出工作区snapshot、来源版本和列出的canonical记忆表到JSON | 有效。含任务、记忆、来源、样本等结构化数据；二进制原附件不在JSON内，导出自带附件需单独备份说明。「全部数据」不应暗示包括所有原附件/模型凭据 |
| 退出 PCAS / 退出 | DELETE `/v1/session` | 结束此浏览器会话并reload | 有效，不注销ChatGPT账户、不断开Telegram、不停服务器处理，不删除服务端资料 |

证据：[SettingsPage](../../web/src/pages/SettingsPage.tsx)，[初始化与snapshot/汇总/导出](../../internal/postgres/workspace.go)，[命令校验](../../internal/postgres/commands.go)，[抽取](../../internal/postgres/processing.go)，[想法事件信号](../../internal/postgres/signals.go)，[到点检查](../../internal/postgres/reminders.go)，[预算](../../internal/postgres/budget.go)，[副手运行/授权重检](../../internal/postgres/runs.go)，[准备继续上下文](../../internal/postgres/run_context.go)，[秘书上下文](../../internal/postgres/desk_turn.go)，[计数](../../web/src/domain/agent.ts)。

## MemoryActivitySettings：逐条记忆曝光

| 当前文案/控件 | 保存字段/默认 | 真实效果与依赖 |
| --- | --- | --- |
| 选择记忆 | 本地选中的记忆id；初始未选 | 从当前snapshot中选择claim，提交其 `recordVersion`；不是全局设置，保存只作用这一条记忆 |
| 基础曝光减半时间（天） | `/v1/memory/activity.half_life_days` → `activity.half_life_seconds`；默认30天；1…36500 | `Recall` 的 `continue` 模式为该记忆加衰减排名权重；不影响普通明确回忆remember/history的此项加分；不删除/屏蔽、无定时隐藏 |
| 采用后的最大强化倍数 | `reinforcement_limit` → `activity.reinforcement_limit`；默认8；1…100 | 用户明确mention/confirmation/adoption更新last_effective_use_at；跨至少1天的有效使用将stability×1.1至上限。系统检索/生成不自动增强；基础半衰期乘stability。配置降低上限会裁剪当前stability |
| 此记忆已固定保留（只读） | 提交现有 `selected.pinned`；默认false | pinned时排名加分不衰减；此嵌入组件没有固定保留开关，不应新增无授权预设或借保存更改pinned |
| 保存曝光设置 | owner-only且记录当前有效、版本相符；保存后显示局部消息 | 实际POST写activity；当前不直接刷新workspace/提升workspace revision，重新选同一记忆可能暂时读到旧snapshot参数，后续轮询会取新数据；不能把按钮成功说明成资料已经重新整理 |

排名加分为 `0.2 * exp(-ln(2) * 距上次有效使用秒数 / (half_life_seconds * stability))`，固定项为0.2；这是检索分数的一小部分，其他词匹配/项目/语义权重仍生效。副手继续运行准备上下文确实使用 `memory.Continue`，因而不是纯存字段。建议解释为「日常交流中优先想起这条记忆的时间」，并将两个数值放高级项。

证据：[组件](../../web/src/components/MemoryActivitySettings.tsx)，[配置写入](../../internal/postgres/summaries.go)，[有效使用强化](../../internal/postgres/editing.go)，[检索排名](../../internal/postgres/retrieval.go)，[默认与snapshot](../../internal/postgres/claims.go)，[继续运行](../../internal/postgres/run_context.go)。

## ConnectorSettings：导入和持续接入

| 当前文案/控件 | 保存字段/初始默认 | 真实效果、依赖与边界 |
| --- | --- | --- |
| ChatGPT、Claude或通用聊天归档（拖入文件） | 无连接开关；POST `/v1/connectors/archive` 文件；最大20MB | 支持ZIP/JSON/JSONL/TXT/MD，保留原文、会话分支与解析缺口；导入记录后资料处理worker继续索引/抽取。归档附件可记缺口，不等于所有附件已提取 |
| 接入名称 | `connector_configs.name`；空串 | 必填用于识别连接，最多200字节（UI maxLength200与后端字节校验不是完全相同） |
| 接入方式 | `kind`；webhook | webhook应用主动POST记录；poll由服务器GET结构化JSON批次；folder由服务器扫描自己的收件目录。不是通用网页抓取，也不是当前设备的文件夹选择器 |
| 数据源地址（poll） | `url`；空串 | `syncHTTP` GET，带cursor/If-None-Match；需要约定Batch格式、HTTP/HTTPS、不随重定向、最多8MB/批。不是任意网站URL保存后就懂如何抓取 |
| 凭据环境变量名（poll可选） | `token_env`；空串 | 必须 `PCAS_CONNECTOR_` 前缀；运行时读服务器对应环境变量，作为Bearer。此处填写的是名称，不能直接填写token；名称不存在同步access_denied |
| 同步间隔（秒，poll/folder） | `interval_seconds`；300；15…86400 | 每完成同步/失败后计算next_sync；serve每5秒尝试队列，一次选一个连接；HTTP分页has_more可马上继续。不是浏览器刷新间隔；webhook也提交300但不由轮询消费 |
| 添加接入 | `enabled=true`，`expected_version=0` 创建 | 只建配置；webhook返回一次性64字符token、后端存hash；folder需部署 `PCAS_INBOX_DIR`，该连接使用其UUID子目录；poll须数据源真正支持格式。保存不代表首次同步已成功 |
| 密钥只显示一次 / 已保存，隐藏密钥 | 新建webhook返回token只存在组件local state | 应用使用所示URL与Bearer发送资料；token只允许向绑定连接追加，不能读记忆。隐藏按钮清局部显示，不撤销连接/token |
| 暂停接入 / 恢复接入 | `enabled`，现有expected_version；创建时true | 轮询/文件扫描只选enabled，webhook鉴权及ImportBatch也检查enabled。暂停停止这个连接的新同步/追加，不删除已导入原文，也不停止已入队资料处理 |
| 立即同步（poll/folder） | `next_sync=now()`，`status=queued` | enabled且版本匹配才排队；服务器后台实际处理。不是立即完成，webhook没有此按钮 |
| 导入数量、最近同步、进度/错误/缺口（只读） | status/imported/error_code/gaps/last_sync；UI每5秒读 | 属连接接入状态；来源中的索引/抽取仍是独立后续阶段。不应将「同步完成」包装成「AI整理完成」 |

归档导入适合普通用户入口；webhook/HTTP/env/folder是技术高级项。无需用户在不知道目的的情况下任选协议。现有通用技术字段都有实际消费者，未发现纯存字段开关。

证据：[组件](../../web/src/components/ConnectorSettings.tsx)，[HTTP入口](../../internal/httpapi/connectors.go)，[配置/鉴权/批次导入](../../internal/postgres/connectors.go)，[实际同步循环](../../internal/postgres/connector_runner.go)，[serve组成](../../cmd/pcas/main.go)。

## NotifySettings：设备推送与 Telegram

| 当前文案/控件 | 持久数据/默认 | 真实消费者、依赖与边界 |
| --- | --- | --- |
| 在这台设备上接收提醒 | 浏览器PushSubscription + `push_subscriptions(owner_id,endpoint,p256dh,auth)`；新设备false | 点击请求Notification权限、service worker subscribe，再POST保存；关闭DELETE此endpoint并unsubscribe。仅此设备，不是工作区总通知开关；需HTTPS/localhost、浏览器推送能力、未拒绝权限。当前checked由本机订阅决定，不证明服务器仍有此endpoint或下一条必达 |
| Telegram Bot token | 私有notify设置文件 `telegramToken`；空串，输入框初始空/保存后清空 | PUT配置会校验并先真实发送测试消息，成功后保存；serve里的Telegram poller用此token接受文字/语音，通知通道也用它发送。不是个人Telegram登录。需要bot支持poll，已有webhook可能失败 |
| Chat ID（可选） | notify文件 `telegramChatId`；空串或已保存值 | 留空ResolveChat自动检测，需先给bot消息；填写则解析为int64并测试发送；不代表当前支持任意多人频道管理。变更bot/chat会重置会话/进度相关状态 |
| 保存 Telegram | PUT `/v1/notify/telegram` | 与只存密钥的模型配置不同，失败不写新配置；保存成功代表连接测试消息已发。没有token无法提交，即不能只改chat ID保留旧token |
| 断开 Telegram | 清token/chat ID | 停本应用对应接收/发送通道并清本地相关状态，不删除已存在事项/记忆 |
| 发一条测试提醒 | POST `/v1/notify/test`；不改偏好 | 直接向已配置channels测试，返回成功通道名；推送可能发到该工作区其他已订阅设备。绕过 `followUps`/任务trigger；测试成功不代表事项规则已启用或下一次提醒必然发送 |

普通事项通知必须有有效notice、`followUps=true`、未dismiss/未终止事项、通道配置与网络；idea时间唤醒仅更新事项状态，`CheckReminders` 没有为此路径创建notice，不能承诺所有唤醒都会手机推送。通知生成和通知订阅是两层状态，应让用户知道已订阅设备仍可能因为总提醒关闭而收不到事项通知。Telegram与模型设置是服务端文件级配置，不是只作用当前浏览器。

证据：[组件](../../web/src/components/NotifySettings.tsx)，[通知API](../../internal/httpapi/notify.go)，[配置/发送/测试](../../internal/postgres/notify.go)，[私有文件字段](../../internal/notify/settings.go)，[Telegram收件](../../internal/telegram/poller.go)，[想法时间检查](../../internal/postgres/reminders.go)。

## ChatGPTConnection：两种订阅通道

这是两个不同接入状态，不能用一个“ChatGPT已连接”掩盖分别登录/授权的状态；workspace agent启用状态又是另一层。

| 当前控件 | 持久字段/初始状态 | 实际影响和依赖 |
| --- | --- | --- |
| Codex App Server：登录 ChatGPT / 重新获取验证码 | Codex管理的账户凭据；初始未登录，无普通workspace偏好 | `/v1/chatgpt/login` 调Codex设备登录，4秒轮询account；服务端须先提供Codex binary/home通道。enabled在组件只是部署可用状态，不是可操作的功能开关 |
| Codex：退出 ChatGPT | Codex账户logout | 停此Codex订阅登录，不能等同退出PCAS或退出直接授权通道 |
| Codex：刷新、复制验证码、官方验证链接 | 无偏好写入 | 刷新读account/limits；复制写clipboard；额度窗口是账户服务提供的限额，不是dailyBudget |
| ChatGPT订阅：Continue with ChatGPT / 添加账户或工作区 | SIWC私有session文件accounts/active_client_id及OAuth凭据；默认仅服务端开启才显示 | `/direct/login` 启动官方授权；不是启用某条记忆权限的总开关。授权完poll+刷新workspace；该流程不得为了UX跳过套餐授权或扩大scope |
| 连接的 ChatGPT 账户（下拉） | `active_client_id` | `/direct/select` 选当前SIWC账户，应用接下来此通道请求使用它；不同于workspace agent种类选择 |
| ChatGPT可用模型（下拉） | 当前account `model`；无已保存模型时目录首项 | 只允许当前账户授权目录里的slug；Generate使用此模型；显示目录首项不等于已存字段。需plan_enabled且未paused才加载目录 |
| 开启套餐授权 | OAuth scope/PlanEnabled() | 额外显式授权 `chatgpt.tokens.use.direct` 与 `resource.invoke` 等才允许计划调用；不能把已登录与已允许使用套餐合并 |
| 恢复套餐请求 | `paused=false` | 只解除本地暂停，不修改供应商限额、不验证限制已经解除。按钮提示用户先在官方用量中检查 |
| 退出并撤销授权 | 清本地access/refresh等token并尝试远端撤销 | 返回remote_revocation_confirmed，失败需保留「本地已清、远端未确认，请到ChatGPT设置断开」；不能缩写成必定撤销成功 |
| 刷新连接 / 用量与权限链接 / 打开授权页面 | 无偏好 | 状态读取/打开用户授权或官方管理入口，保留可操作的未连接/未授权/暂停/验收未完成状态 |
| 默认套餐入口（只读） | account `verified`、`default_initialized`，Registry默认选择策略 | 只有完整生命周期验证后才自动作为隐式订阅默认；显式API默认优先。不得为“看起来能用”切默认或忽略未verified提示 |

初始Codex provider模型为源码值 `gpt-6.1-sol`（已有配置可覆盖）；直接授权模型来自账户目录。实际订阅额度由账户决定，内部费用计¥0，向量需独立接入，不可声称登录订阅同时开通语义索引。

证据：[组件](../../web/src/components/ChatGPTConnection.tsx)，[Codex API](../../internal/httpapi/workspace.go)，[直接授权API](../../internal/httpapi/chatgpt_direct.go)，[SIWC状态/选择/撤销](../../internal/ai/siwc/manager.go)，[SIWC保存字段](../../internal/ai/siwc/storage.go)，[模型目录/推理](../../internal/ai/siwc/inference.go)，[默认/费用策略](../../internal/ai/provider.go)。

## OpenAIConnection：文本、向量和旧 Jev 路由

下面默认只是未配置时表单回填，**不是核对过的供应商价格**；不应自动覆盖现有用户输入或拿它作实际账单保证。字段保存到服务端 `PCAS_MODEL_SETTINGS_FILE` 指定的私有文件，服务/worker每次读取；未配置该路径时不可在界面保存。

| 当前控件 | 保存字段/源码回退默认 | 真实影响和边界 |
| --- | --- | --- |
| 文本API Base URL / API Key / 模型 | `text.base_url/api_key/model`；https://api.openai.com/v1 / 空 / gpt-6.1-sol | 保存后provider `openai-api` 发Chat Completions请求。留空key仅同地址保留旧key；改地址必须重新给key。这里只校验格式、保存，不试调用，所以「已配置」不是模型已验证 |
| 文本API输入预算单价 / 输出预算单价 | `text.input_cny_per_million/output_cny_per_million`；1 / 4 | 参与请求前保守预留和结果token费用估算，输入>0、输出≥0。手工估算参数，不改服务端实际价格 |
| 作为默认副手与后台抽取入口 / API作为默认入口 | `text.default`；无配置则由Registry.ExtractionID与部署配置计算 | true使ExtractionID返回openai-api，影响新资料抽取及默认副手标记；前端有已选模型时仍优先已选，false恢复配置/订阅默认策略，不是彻底禁用API通道。开关只改表单draft，需「保存文本API接入」才持久化；取消勾选若部署extraction_provider本就openai-api仍可能返回它 |
| 保存文本API接入 | POST `/v1/models/openai/text` | 写配置并刷新workspace；不会立即用真实请求验证endpoint/key/model可生成 |
| 向量 Base URL / API Key / 模型 | `embedding.base_url/api_key/model`；https://api.openai.com/v1 / 空 / text-embedding-3-small | 保存后独立provider openai-embedding用于资料/记忆embedding、语义查询；不会替代文本模型。换模型不自动保证历史向量全部重建 |
| 向量输入预算单价 | `embedding.input_cny_per_million`；0.2（output为0，default为false） | 与dailyBudget共同预留向量费用；不影响文本输出单价 |
| 保存向量接入 | POST `/v1/models/openai/embedding` | 写文件，语义查询按当前模型/维度匹配；旧模型向量不冒充新向量，原文检索仍存在 |
| 补建现有资料向量 | POST `/v1/models/embeddings/rebuild` | 当前有效记录缺失当前模型向量则排队；只有queued数量，不是重建完成；需资料worker/可用向量服务/预算，受现有队列状态影响 |
| 导办台分流·Jev API Key / 保存并试分流 | `decision.api_key`；无保存key则服务器TYPESAFE_API_KEY可回退 | POST保存后真正调用一次Jev，working反映这个测试请求；失败密钥仍保存。旧 `/v1/desk/route` 接口实际可调用Jev，但**当前Web秘书/Telegram主流程均无消费者**。不能保留“现在秘书由Jev分流/每句原文都发TypeSafe”的承诺 |
| 移除密钥（Jev） | 删除文件内decision | 仅移除保存key；环境变量仍配置时旧路由继续使用环境key。现有「已移除，导办台按本地规则分流」不能保证。也不是删除部署环境密钥 |

文本/向量API是有消费者的技术配置；Jev当前是过时主界面入口，不是确认可工作的普通功能。其旧接口发送的是输入原文给TypeSafe、仅返回ask/record/delegate，不生成回答，且不走每日模型费用预留路径。若协调者决定保留技术入口，必须准确写「旧分流接口配置」，不能把它包装成当前秘书的可选改善。

证据：[组件](../../web/src/components/OpenAIConnection.tsx)，[保存与默认回填](../../internal/ai/settings.go)，[模型实际调用/选择](../../internal/ai/provider.go)，[设置API及试分流](../../internal/httpapi/model_settings.go)，[旧路由接口](../../internal/httpapi/workspace.go)，[Jev调用](../../internal/ai/jev.go)，[当前秘书请求](../../web/src/components/Secretary.tsx)，[前端默认选择](../../web/src/components/Shell.tsx)，[服务器环境回退](../../cmd/pcas/main.go)，[向量处理/补建](../../internal/postgres/processing.go)。

## 补充：用户提到的「收下」及事项结果术语

「收下」字面出现在 `UnsureSheet`（需要你确认），不是当前ThingPage字面按钮。**该Sheet是保留组件，基线59e25da的当前路由不可达；它没有任何import或挂载。** 下表前七行是保留组件的按钮→后端命令事实，不代表用户当前能点击；从「事项动态」起为当前可达ThingPage流程。用户要解决的是事项使用体验，不能要求其辨认内部页面。原报告未先核对Sheet可达性，此次纠正；按钮含义可供U2理解历史，但不能仅修改死组件就声称改善当前体验。

| 现有按钮/选择 | 实际命令与结果 | 费用/撤销/容易误解的边界 |
| --- | --- | --- |
| 作为待办 → 收下 | `acceptCandidate(kind=task)`：以候选文字新建todo任务，保留候选来源、projectId和due，候选accepted并记录resolvedInto | 无模型调用/新增模型费用。不是仅“看过”。没有自动创建提醒trigger或步骤。命令不在undoableCommand中，当前只有dispatch，无通用撤销 |
| 作为想法 → 收下 | `acceptCandidate(kind=idea)`：新建active想法，保留来源/项目；候选accepted | 无模型费用，无通用撤销；不是搁置或执行想法；没有自动添加唤醒条件 |
| 作为记忆 → 收下 | `acceptCandidate(kind=memory)`：rememberTx写入记忆（必要时复用对应记录）；默认memoryKind=fact，默认confirmed/direct，保留候选来源/项目 | 无模型调用/新增模型费用，无通用撤销。这是确认并记入可供副手使用的记忆，不只是归档原文；记录仍遵守agent权限筛选。来源有效性/版本冲突仍可失败 |
| 作为待分类 → 收下 | unknown时禁用按钮 | 需先选有效目标类型；下拉本身只改组件local state，点收下才落实 |
| 不要 | `ignoreCandidate`：pending→ignored，保留候选与原来源 | 不删除原资料、不阻止后续资料处理；无通用撤销。后端有restoreCandidate（ignored→pending），当前Sheet没有恢复入口。可解释「忽略这条线索」 |
| 对（资料推测） | `confirmMemory`：新记忆版本confirmation=confirmed、acquisition=direct，记录用户有效使用；相关旧依赖会重新校验 | 无模型调用/新增模型费用，无通用撤销；不是简单消除提示角标。建议「确认这条记忆」 |
| 不对（资料推测） | `deleteMemory`：删除该claim及依赖它的派生副本/运行结果/样本等，设置BlockReimport；该Sheet未传includeSources，所以默认不删原来源 | 无通用撤销，不只是标记“尚不确定”。建议结果明确的「删除这条记忆」；需要纠正文字应另走记忆编辑流程，不能把删除说成改正 |
| 事项动态：看看 / 收起 | RunRow local `shown`状态 | 只展开已生成结果，不费用、不采纳、不修改事项 |
| 事项动态：放回去 | `adoptRun`：输出含Markdown checklist则task追加子步骤；project/idea则新建相应关联待办；否则summary写进progress，其他结果新建该事项文档 | 不再发模型请求、无新增生成费用；记录采纳样本并强化实际上下文记忆。summary对项目是替换progress，对任务是追加notes，对想法是追加body，不能一律叫“写进进度”。当前按钮已用dispatchUndoable，可在30天/内容未改/资料未删等条件允许时撤销；不退还之前生成费用。适合改为实际目标「加入步骤/创建待办/保存文档/更新项目进度」 |
| 手动交接：复制给它的内容 | clipboard.writeText(run.brief) | PCAS不发模型请求；用户另行转交AI，此处不代表已经运行 |
| 手动交接：放回来 | `pasteRunResult`：waiting→done保存粘贴output；接着尝试autoAdoptRunTx，按checklist/summary/doc规则把结果写入事项 | 无PCAS生成费用；采用成功产生独立action，可在结果行撤销采用。paste本身不在undoableCommand，当前dispatch不会给粘贴动作撤销toast；自动采纳失败仍保留已完成结果，可再手动采用。应解释「保存回答并加入事项」而非只保存草稿 |
| 不转交了 | `discardRun`：手动waiting运行变failed「已弃用」 | 当前dispatchUndoable，可条件内撤销；不是删除全部事项或已生成资料 |
| 重试 / 换模型重试 / 重做 | `requestRun`新建一次运行，不覆盖旧运行 | 可产生新的API费用/预算预留，重新检查模型enabled/available和记忆权限；旧费用不退款。「重做」不是只是重新展示旧结果 |
| 交给副手（当前ThingPage无同名字面按钮，秘书委派流程） | Secretary POST `/v1/desk/turn`；模型批准的requestRun/delegateTask动作产生运行（delegateTask可同时新建任务） | 这是执行模型请求/预算动作，不等同acceptCandidate。副手产出后通常自动加入事项（autoAdoptRunTx），不是永远等用户点收下；撤销取决于该动作回执与工作是否开始，不能承诺已付费生成可退费 |
| 想法：再放放 | `ideaSnooze(days=7)`仅设置现有wake.snoozedUntil | 无模型费用，当前dispatchUndoable；源码没有在此命令把status改回shelved，所以不能在纯前端文案承诺“一周后一定重新唤醒/自动从眼前消失”。存在这项语义边界，U2勿借改文案修后台业务 |

补充证据：[待确认Sheet](../../web/src/components/UnsureSheet.tsx)，[事项页](../../web/src/pages/ThingPage.tsx)，[候选命令](../../internal/postgres/commands.go)，[记忆确认/删除](../../internal/postgres/editing.go)，[记忆默认确认](../../internal/postgres/claims.go)，[结果采用与自动采用](../../internal/postgres/runs.go)，[可撤销命令及边界](../../internal/postgres/actions_log.go)，[当前秘书](../../web/src/components/Secretary.tsx)。不建议仅把「收下」换成同样含糊的「确认」；按钮应直接描述选择类型带来的结果。

### 当前候选可达性补查

| 已核查位置 | 基线实际入口与限制 |
| --- | --- |
| App路由 | 仅Hall、Thing、Library、Settings；旧`/inbox`、today/upcoming/ideas/things均重定向Hall，没有候选页或Sheet路由 |
| Shell | 导航只有搜索、资料库、设置；只挂载CommandPalette，没有UnsureSheet/待确认计数入口 |
| CommandPalette | 搜索现有事项/记忆；可`capture`快速记下并让后台整理，或直接新建任务/想法/项目。不搜索/展示state.candidates，不能accept/ignore已有候选 |
| Hall + domain/hall | DecisionStrip的decisionQueue仅涵盖依据变化重做和手动交接；AwayLine可显示后台每日整理数量，但没有候选明细或管理链接。源码注释说captures去observatory，当前路由/资料库未实现此候选入口，不能把注释当可达证据 |
| Library | 仅memory/sources/training三tab；来源tab有后台job重试/每日汇总记录，但无capture_candidates列表。memory tab筛选「推测」后可打开MemorySheet，用「没错」confirmMemory、编辑、删除，因此推测记忆管理仍可达，与pending Candidate是不同记录类型 |
| Secretary/Telegram | 都直接DeskTurn；后端schema/actions有create/update/add_steps/delegate/remember等，没有接受/忽略候选命令；上下文也不提供候选列表。让秘书新建类似事项不等于把已有候选标accepted，不能将自然语言输入当现有候选收件箱 |

全量 `rg`：`UnsureSheet` 在 `web/src` 仅其定义；`state.candidates` 仅未挂载Sheet使用的 `unsure()` 辅助函数；`acceptCandidate/ignoreCandidate` 调用仅保留Sheet，其他匹配为类型定义，`bulkAccept/bulkIgnore/restoreCandidate`只有Action类型。结论为源码可达性审计，未进行运行时浏览器验收。

体验影响：用户通过导入/快速记录生成低把握候选后，当前UI不能查看逐条内容、转为任务/想法/记忆或忽略；每日整理可提示「N条等你确认」却不给完成这件事的入口。`autoAccept`默认false，只开它也不能处理想法/未知/低置信度待办；不能让用户切开关来绕过缺入口。需要由协调者审定最小的现有页面入口，U2处理，不在本调查实施新顶级面板或后端能力。

可达性证据：[App](../../web/src/App.tsx)，[Shell](../../web/src/components/Shell.tsx)，[CommandPalette](../../web/src/components/CommandPalette.tsx)，[Hall](../../web/src/pages/HallPage.tsx)，[队列/后台feed](../../web/src/domain/hall.ts)，[Library](../../web/src/pages/LibraryPage.tsx)，[候选筛选辅助](../../web/src/domain/lines.ts)，[秘书动作schema](../../internal/postgres/desk_schema.go)，[秘书动作执行](../../internal/postgres/desk_actions.go)。

## 交付范围与未验证项

已逐项核对设置页及Timezone/MemoryActivity/Connector/Notify/ChatGPT/OpenAI嵌入组件的用户可见开关、关键参数和相关动作；未改任何产品代码、测试、任务或契约。没有只凭可保存就认定有效；Jev已明确区分「旧接口存在/试调用存在」与「主用户流程不调用」。报告中运行时效果是源码推断，未声称真实连接、通知送达、账户套餐、归档质量或部署worker健康已验证。现有线上/二阶段验收门槛仍由相应任务完成。
