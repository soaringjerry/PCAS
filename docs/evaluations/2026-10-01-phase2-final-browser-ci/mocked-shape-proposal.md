# 两条旧mock原目标请求断言：独立修订提案（执行前冻结）

原CI精确头 c8adbaeb5c02daa9e83f90f74c216d8cf9ee22bf、run36887421678/attempt1正在运行。本提案先于实际终态，只记录只读发现；尚未据此记失败，尚未修改spec或运行本地测试。

原 `phase2_manual.spec.ts:179/201` 以 `toMatchObject(...manualRecipient: recipient)` 要求请求带完整scope/role/channel/model/protocol/fingerprint。其mock Run.ManualRecipient与可信ContextTask.recipient均含完整合成身份。产品UI原目标重建只发送provider，来源/v1/models与server Task正式绑定；UI domain ManualRecipientSelection及既有phase2契约不授权客户端生成/传回完整Task。真实80bcd46恢复原失败和03e5eee恢复单例通过已独立证明同provider-only选择由当前Task绑定同一完整recipient；此次不靠调整产品或当下失败来决定授权规则。

拟仅修package失败原目标重建及failed manual重试两处请求断言：

```ts
expect(recipient.provider).toBe('writer')
expect(backend.commands[0].manualRecipient).toEqual({ provider: recipient.provider })
expect(backend.commands[0].sourceRunId).toBe(runId)
```

其余type/agentId/kind/prompt等原match保留，只将manualRecipient从partial全身份匹配中移出，以exact对象禁止role/channel/model/fingerprint等无用或权威字段。为typed访问只在本文件Command类型增加sourceRunId?: string，不改变fixture的真实返回形状或可信Task。

保留每条原业务流程、新ID、复制次数/旧预览清除、unchanged prompt、same kind、原owner文字/notes、Brief不读、无pageerror/无unexpected请求、paste拒绝、换target、新请求provider过滤和所有其它26矩阵断言。新增sourceRunId精确等于原Run ID补强来源定位，不用toMatchObject接受额外客户端身份。旧测试名称暂保以能对照历史报告，complete recipient语义指同目标，不再误表示完整身份JSON。

只有当前CI实际失败证实同shape原因后才提交净spec；保原终态、原完整日志/产物和未抵达项。root指定下一精确组合再决定因果验收，不自行本地重跑或重触CI。
