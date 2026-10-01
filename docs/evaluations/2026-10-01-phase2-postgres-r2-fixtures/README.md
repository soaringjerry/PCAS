# 全PG R2：经root审定的独立夹具迁移

原始全批红报告固定在 `ca57cac37ad7fcf66784273366af634f166d8a88`，实际执行HEAD `7706baa4f7c66fb7415c294f2bd703df62503154`。本补丁不更改其JSONL、计数、报告、gold或产品，只落实已审定的精确协议与入口迁移。尚未动态复验。

| 用例 | 旧→新及保留的严格断言 |
| --- | --- |
| D3快照 | 原err!=nil误报可读→**必须ErrUnavailable且body长度0**；attempt invalidated、撤权/ABA旧读与采纳拒绝、新run当前policy全部不变。 |
| 31k容量 | 正式wire是error字段→解析JSON并必须HTTP507/error=record_capacity；原31000 padding、ownerNotes字节一致、间接claim依赖、全部attempt无DeliveredAt/DispatchedAt、manual fake实际HTTP0仍硬断言。正常容量indirect-only成功叶不改。 |
| worker已stale | 2.0最终fence旧done与现failed冲突→精确failed/stale/无Adopted/Docs0，新增Output/Brief空；真实HTTP恰1，按原100 input/20 output及1/2每百万成本精确0.00014，reserved_cost与失败状态一致。不能释放已经发生的成本。 |
| deleted secretary history | 删除与改版/撤权不同→精确Reply/Text空+Cards空、原turnID/一条turn/跨owner隔离；其他更正、撤权placeholder断言未改。 |
| Codex legacy AnswerDesk | fake过去禁止outputSchema→校正式answerDeskSchema的answer/used/links、required全字段、noextra，仍web=true；秘书schema与Run web=false及原plain/draft两个业务输出不改。 |
| timeout | 保原timeout叶，用合法loopback URL加私密path实际HTTP恰1、实际200ms client timeout；保WARN model/timeout、超时反馈、原文保存、无私密URL/key/错误日志。另新增invalid-userinfo叶保原敏感userinfo/密码/path，必须HTTP0/context-invalid_input/安全capture+原文保存；响应更严格检查任何secret-T3均不出现。 |
| scoped history | 无scope的legacy AnswerDesk正控读project claim不合法→同project真实ThingID的Scoped DeskTurn。初始用户query固定`Private reference 的安排是什么？`，无519823；actual secretary bytes和manifest exact claim payload spans必须证实519823，reply仍原Private derived answer 519823。先同studio真实requestRun/RunAgents actual HTTP正控含该历史answer与exact claim IDs，再原excluded/不同studio/new unscoped三路径，真实RunAgents请求必须无519823且普通工作仍done。排除查询本身/缺历史/只blank Brief的伪正控。 |

scoped fake仅按真实秘书/副手协议返回原私密reply或普通output/Used=[]；请求收集加mutex，不调用parser stub，不补raw许可、不改source global、不SQL造deps。其他原failures（delegate公开undo日志、parse INFO、最终错误码/删除反馈、普通source-free continuation）仍交产品作者，由root串行安排。

验证仅 `go test ./internal/postgres -run '^$' -count=1`：编译退出0，**零测试执行**。动态必须等root新精确组合头，一次有因批次；本目录不宣称七项迁移已通过。
