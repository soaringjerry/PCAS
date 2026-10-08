# A1：稳定化集成审查与进入二阶段的判定

> **Historical task record.** This text records an earlier phase or investigation.
> Use the [current documentation](../../README.md) and [project status](../../status.md) for new work.
> Its old assignments, limits, and precedence statements do not govern current development.

执行者：Astra / high。后端修复已交付，先审查明确提交的跨模块行为；U1 已完成交接，执行一次完整候选验收。四个待裁定 U 叶不因此获准实现或宣称通过。

## 本轮精确候选与工作区

使用 /root/PCAS-wt/A1，新分支 stabilization/acceptance-candidate。从 F11 d38a0b4f375d0e2acc951de124554320ec1a9f45 开始，仅集成 F12 ae9f69b3611e0600e7fce97a404e8f043f569958、F8 2e6ebc1abe4fde92a7ceabdc41b7704ba56ac5de 和 M0 文档 21faec7ff2019b2722e660751231ae7af92ba832。F11/F12 已含 F7、F9、F10 和 T1/T2/T3，不重复 cherry-pick。U1 已交付 b0386f9b8186b6a87e5b19cb7c5a710b640626f3 并停止执行，整合该 SHA；当前完整产品候选为 7aae834d34b3749bdceaf0a2b704b79e837a391e。允许创建/推候选分支和 draft PR（base main），不合并任何 PR/main。冲突先报告，不自行改产品解决。协调任务文档仅在协调工作区只读，不合入其分支。

报告可在该分支提交，产品和共享测试只读。独立预览/后端端口 18144/18145；自建 tmpfs PostgreSQL、动态 localhost DB 端口、自有 node_modules，按 PID 清理。完整检查之前等待 U1，先读关键 diff 和报告形成审查问题，不新建测试体系。

已有验证：#25/#26/#27 远端全部通过；#23/#28/#29/#30 部分浏览器仍运行。各报告区分最终与先前提交的本地证据，最终重新核验远端 SHA。

集中审查：

- F11 PG 对话锁、本地 pool 留余量、取消释放；前一已开始轮完整提交，但不保证多个等候请求严格 FIFO。结合 S1/前端发送行为判断是否真实缺陷，不发明新排队协议。
- F12 DeskTurnByRequest 与 F11 DeskTurns 的 owner/依赖/删除/实时撤销一致性；同 bot 换 token、换 bot、legacy 锚点、回调绑定及 send 成功到关联落盘窗口。
- F9 持久 suppression 与撤销指纹/连续撤销、显式关闭/未来 occurrence、历史 Notice/Job/Activity；F10 already_undone 与 expired 优先级及精确 30 天边界。
- F8 fixture 不污染 P1 等别名；U1 不破坏工作区时区、历史回执、返回路径和按钮语义。
- D1 原 finding 已撤回：原夹具没授权 claim，修正后既有删除实现通过；审查加强后的依赖断言及生成中删除测试。
- T1 U3/U4-user/U5 为错误码契约冲突；U10 为本轮新建对象引用协议空缺，4 叶继续 pending。50×20 随机序列及已有规则不得降低预期。线上 11/11 与一天试用仍无证据。

## 审查输入

- F7/F8 及后续修复的具体提交与 diff；U1 如已执行则包含截图和交互回归。
- T1/T2/T3 的编号覆盖、原始失败、对应修复及最终零 skip 结果。
- M0 对二阶段的方案、复用点、待裁定契约和评测计划。

## 工作

1. 先查跨模块一致性：时区/提醒、动作/撤销/采纳、同对话并发、删除/历史响应/训练副本、渠道幂等。
2. 对关键状态序列审查预期是否自洽；测试是否只重复实现；无法证明的明确标为缺口。
3. 在隔离实例执行一次完整集成检查，优先复用既有 runner，不重复建测试系统。
4. 输出按严重程度排序的发现和具体修复范围。普通修复退回 Sol；只有明确的高难阻塞由协调者另行授权 Astra 实现。

## 范围和发布边界

只写报告 `docs/evaluations/2026-10-01-stabilization-acceptance.md`，产品和共享测试只读。不得自行合并、部署、访问生产资料、调用真实账号或发送真实通知。

隔离环境通过后，真实 Codex、实际工作区时区、Telegram 与真机推送的 [11 项线上实测](../phase1/postlaunch.md)另行执行。线上实测和用户试用一天没有证据时，结论必须是“可进入线上验收”或“仍有缺口”，不能写成“第 1.5 阶段完成”。

交付包括检查命令与结果、未覆盖项、二阶段能并行准备的内容及仍须等待的入口条件。不要提前扩大到训练、手机采集、完整工作室或观测台实现。
