# U1：首页、事项页与记忆回执的体验打磨

执行者：Opus 5.5 / high，经本机已登录 Claude CLI 执行。`opus` 别名的只读探测实际返回 `claude-opus-5-5`，可调用；不使用 Opus 承担普通后端循环。先读 [白皮书](../../whitepaper.md) 的产品/界面部分、[设计原则](../../design/principles.md)、[执行分工](dispatch.md)。

F8 已交付最终 `2e6ebc1abe4fde92a7ceabdc41b7704ba56ac5de` 并停止写入，U1 可在此候选上开始，不等待 main 合并。工作区 `/root/PCAS-wt/U1`，分支 `stabilization/U1-ux-polish`，PR base `fix/timezone-displays`，明确依赖 #23；不向 main 合并。每批只处理少量有证据的高影响界面问题，复用 F8 截图避免重读全仓库。

## 目标

让用户能连续说话、看清已办事项、在原处修改或撤销、顺畅进入项目与原文。保持三栏办事大厅、统一秘书、三类按钮和少长文的既定方向。

## 先走查，再做一批有依据的小改动

| 路径 | 检查重点 |
|---|---|
| 登录 → 首页 → 说一句安排 | 输入入口清楚；桌面与手机均能看到结果；键盘不遮住关键操作 |
| 回答中顺手记事 → 继续追问 | 连续发送不卡住；消息顺序、处理中、失败与重试可理解；草稿保留 |
| 回执 → 改 → 撤销 → 刷新 | 操作就地发生；撤销状态明确；失败有可执行原因；历史回执与当前事项不混淆 |
| 项目 → 事项 → 子任务/文档 → 返回 | 层级与返回位置明确；时间一致；读写状态有反馈；滚动与焦点合理 |
| 带来源的回答 → 原话 → 返回 | 能看见依据，来源缺失/删除说清楚；无需读内部术语才能判断答案 |
| 空白、等待、失败、长标题、长文档 | 不抖动、不溢出、不重复反馈；必要信息能展开 |

先提交按影响排序的发现清单和截图，再只实现确有复现证据的界面缺陷。不要以“打磨”为由重做导航、工作室、观测台，或添加模式选择与管理按钮。

已有走查线索：F8 的 [390px 上海工作区截图](https://github.com/soaringjerry/PCAS/blob/bddc14b849330e3c6ead9e0c461ea7172eaac6ee/docs/evaluations/2026-10-01-timezone-displays/mobile-shanghai.png) 中，时区提示因重复长时区名称占据多行。当前文字来自 F6 已规定的文案，不能直接当成本次 F8 回归。U1 可提出更紧凑、区分当前设置与检测位置的呈现方案；若改变约定文案，先交协调者核对 F6 要求，避免测试按旧文字与新设计冲突。

## 文件归属

本次锁定范围：`web/src/pages/HallPage.tsx`、`ThingPage.tsx`，`web/src/components/Secretary.tsx`、`SecretaryCards.tsx`、`SourceSheet.tsx`、`TimezoneHint.tsx`，`web/src/styles/hall.css`、`thing.css`、`secretary.css`，及对应 `web/tests/secretary.spec.ts`、`buttons.spec.ts`、`timezone.spec.ts`。没有修改的文件不为格式化而触碰。

时区提示可以调整紧凑呈现和措辞，保留“设备时区与工作区不同、用户点击才切换、可关闭”的 F6 行为；报告列旧→新文案及截图，保留原时区/历史回执断言。其他正式契约的状态/权限行为不改。

日期工具、StoreProvider、API 类型、后端和提醒规则属于其他任务；需要变动先报告协调者。复用现有 CSS tokens、控件和 API，不能自己模拟功能或建立浏览器事实库。

## 交付

- 报告 `docs/evaluations/2026-10-01-ux-polish.md`：每项“问题 → 调整 → 证据”，区分前端可修与后端依赖。
- 截图附件归属 `docs/evaluations/2026-10-01-ux-polish/`，提交精简前后对照；临时原图可留自有目录。自有预览端口登记 18142，不使用其他执行者端口或 node_modules。
- 桌面 1440 和手机 390 的调整前后截图；以真实构建为准。
- lint、type-check、build、对应 Playwright 回归。状态规则由已有契约决定，不由截图测试重新定义。
- 独立分支/PR；等待协调者审查，不自行合并或部署。把额度集中在用户能感知的差异上。
