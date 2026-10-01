# 真实公开Task wire与旧JSON兼容预期

D真实serve准备run后发现公开 `Run.contextTask.Recipient` 大写而UI读取小写导致崩溃。root批准7f043ff契约：ownerId/recipient/purpose/scope/view/now/timezone/memoryBudget/totalInputTokens，desk_actions保持，嵌套标签不改。

C先冻结[固定旧大写完整Task及新输出字段](../../../testdata/phase2/task-json-gold.json)，再独占新 `internal/memory/context_task_json_test.go`（memory_test外部包，导入真实workspace，避免循环）。三叶分别核公开Run真实Marshal含完整小写recipient、旧literal大写JSON完整身份/视图/预算/时间/origins回读、新输出再读一致。无需UImock或第二套parser，不改D的浏览器用例。

动态等待root新完整组合；本预期和编译不代表真实serve/UI已通过。

[净harness编译记录与SHA](compile.json)：真实workspace.Run Marshal、固定literal旧JSON（非从新实现生成）及全部typed值比较、新输出roundtrip三叶。原207bd706产品上 `go test ./internal/memory ./internal/postgres -run '^$' -count=1` 退出0，无测试运行；旧输出大写若动态执行必须失败，不把编译计为wire通过。
