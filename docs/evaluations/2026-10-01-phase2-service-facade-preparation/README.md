# 可选memory.Service能力独立预期

D真实serve发现 `sources=memory.NewService(db)` 未实现两optional port，授权501使UI两例未抵达。C独立冻结[四方法facade预期](../../../testdata/phase2/service-facade-sequences.json)：精确context/Scope/请求/返回/错误透传、owner边界、旧Sources-only repo受控Unavailable。不改变原Ingest及其tests，不绕校验，不把这里的unit能力当真实serve已通过。

新文件只属于C：`internal/memory/context_service_test.go`。通过运行时接口断言在旧产品上也能编译；未实现能力必须Fatal，不skip。动态等待root新组合；D继续真实应用两例验收。

新harness三顶层20叶：四方法success/error精确透传8叶、nonowner/invalid owner8叶、旧Sources-only4叶。复用旧recordingRepo而不改它或原service tests。[编译记录与SHA](compile.json)：原207bd706产品 `go test ./internal/memory -run '^$' -count=1` 退出0，实际动态0；运行时接口缺失若发生则Fatal，不把编译计为能力通过。
