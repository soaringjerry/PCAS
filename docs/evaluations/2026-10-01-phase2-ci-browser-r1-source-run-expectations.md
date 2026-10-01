# UI重试来源定位补强：冻结预期

依据root批准的契约 `2fd719c784dd41d2f520ef1f001b2a1742126af4`。原0032883 CI三轮失败及实际body保持于独立报告，新的预期不改写原红。此提交只冻结UI验收；产品实现、动态四例及最终完整门均尚待指定头。

| UI入口 | 独立预期 | 本批验法 |
| --- | --- | --- |
| 旧golden G5实际API失败后“重试” | 从正式snapshot读取同事项唯一failed Run ID；实际requestRun的sourceRunId严格等于该ID，manualRecipient字段缺席；原同prompt/接收者保留 | 仅G5增加被动请求观察；原成功三步骤等待与副手实际model事件数2断言全部保留 |
| manual_backend撤权后的“向原接收者重新生成” | owner亲写prompt允许重新装配，但实际requestRun仍带原Run的sourceRunId；明确manual provider不变，新ID不同 | 仅现有恢复例加原ID严格断言；原package、clipboard、409、无撤权atom、提交/自动采纳全部保留 |
| 无initialRun的普通手动新请求 | 沿普通新请求，不带sourceRunId；manualRecipient仍仅provider | 两manual例共用既有新请求helper增加字段缺席断言；旧目标/自然prompt/assertions保留 |
| 自动regenerate；已有Run预填表单及其改字/标点、换目标 | 始终带原Run ID；改字不把模型生成prompt洗成owner独立来源；客户端不能上传origin/Task授权 | 产品净提交后静态逐callsite审阅；本批不新增动态叶，不假称现两manual例已覆盖编辑/换目标。C独立backend gold覆盖生成Prompt源失效及改字 |

F7连续撤销全部原断言及fixture不改。G5 fixture503-once、旧期待完成和model两次仍不改。只允许增加更强的真实请求观察，不拦截、不替换请求或包、不用fake/state setter制造旧Run ID。

计划root指定组合头后沿原custom-command真实runner四例单次/retries0：G5、F7连续撤销、两manual_backend。此前mocked26、0032883 sharedaux两manual PASS和旧三轮FAIL证据独立保留。冻结与compile/list不算动态通过。
