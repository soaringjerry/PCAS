# D4：排队副手的真实秘书动作来源

人工预期另存 `delegation-origin-sequences.json`，不改原D1–D3。两个子例为 delegate:new，以及 create_task→N1→delegate；都是同一原始零claim source v1、同配置的secretary/deputy独立canonical source policies。

关键区别：队列准备时还没有已提交action ID。测试必须在真实DeskTurn最终提交后，将run origin集合与该轮成功create/delegate receipt/action_log IDs核对，不能把预分配task ID、N alias、测试生成ID当来源。实际input与exact typed source依赖另核，不用Used=[]猜未供给。

每子例先真正完成一份来源派生结果并auto-adopt，再undo该adoption。随后新的真实DeskTurn走同action路径排队，记录真实queued状态（不将UI中logical running表示当已外发）并在副手provider外发前仅撤secretary policy。副手policy ID/revision/recipient仍allow，route不变，并用正式trusted deputy GetSource作许可正控。旧queued HTTP0、旧已完成结果拒读/拒采纳；采纳拒绝不依赖existing adoption或queued status。最后新独立deputy问原source，真实HTTP供给原atom，证明接收者自身没有失去许可。

本gold先于action-origin消费者动态执行冻结。runtime harness继续归C的 `phase2_runtime_delegation_test.go`；需A给稳定的server-origin读字段/方法，随后在root精确组合头验收。此计划没有动态结果，不声称D4通过，也不替代D其他动作字段矩阵。
