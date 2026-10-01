# T3：独立检验秘书、Telegram 与删除传播

执行者：6.1 Sol，只写测试。依据 [执行分工](dispatch.md)、[序列表 §2.3–2.5](README.md) 与 [秘书契约](../phase1/contracts.md)。当前排队，启动时由协调者给出包含 F7 的基线。

## 覆盖

- S1–S9：同对话快速连续输入，后一轮能修改前一轮对象；无 THIS 时不误改；非法别名或 UUID 不影响其他合法动作；幂等键重放；ask 与 actions 同时存在；追问续接；事项页对象；预算/超时/500/纯文本区分；超长回复截断且动作仍执行。
- T1–T6：重复 update、换 bot 后进度/对话重置、非授权 chat/群聊/伪造 callback、超长回复、无转录服务时保留音频并说明、处理中断后恢复。
- D1–D4：删除引用或原话后清理整轮缓存，撤销快照失效，重放不恢复已删除正文。
- 不能只检验页面提示：要断言数据库没有重复事项、请求没有重复执行、秘密和被删正文没有出现在响应/日志。
- 并发测试用 barrier 控制请求顺序，明确区分用户发送顺序与模型返回顺序；不要靠短 sleep 碰运气。

## 文件归属

只新增 `internal/postgres/stabilization_secretary_test.go`、`internal/postgres/stabilization_deletion_test.go`、`internal/telegram/stabilization_secretary_test.go` 与报告 `docs/evaluations/2026-10-01-stabilization-secretary.md`。helper 前缀 `stabilizationSecretary` 或 `stabilizationDeletion`。

不改被测实现、公共 helpers、前端、配置、迁移或他人报告。必要的测试能力缺口先报告协调者。T1 的跨渠道撤销与本任务的重复 callback 各自引用测试编号，不重复制造同一套夹具。

## 验收和发现

每个编号有可单独运行的测试和覆盖矩阵。失败先记录复现和实际状态，再按阶段要求暂存 skip。真实外部渠道、真机通知、真实 Codex 账户未测就明确标注。完成 make check、自己的数据库集成测试和资源清理，提交测试 PR，把发现交由其他 Sol 修复。
