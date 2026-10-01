# Phase 2 独立最终适配观察验收

产品 `eeab123cc8649838a3bd72b28e19a1e842673489`，harness `6f498263e3f70b960beb82aba07c7dbde4a520cf`，2026-10-01 UTC；一次 `go test ./internal/ai ./internal/ai/siwc -run '^TestPhase2Context' -count=1 -json`。

**退出0，3顶层 / 10叶级全PASS，0SKIP；80事件、10份原始观察。** Root新增授权C独占 `internal/ai/phase2_context_test.go`、`internal/ai/siwc/phase2_context_test.go`，产品未改。所有HTTP、OAuth、model目录、SSE及Codex app-server均为本地合成假服务，不使用真实账号/CLI/模型。

| 真实生产适配 + 假服务 | 证明 |
|---|---|
| openai/responses/anthropic，各正常+拒绝 | prepared→before_dispatch→dispatched 顺序；最终serialized_request bytes逐字等于实际fakeHTTP收到的body；model/provider/protocol/endpoint与请求相符；中文newline/quotes/backslash/<&>实际编码保留；before_dispatch拒绝后真实HTTP0、无成功输出。 |
| Codex，正常+拒绝 | 真实NewCodex/Registry对假app-server实际RPC；adapter_arguments中thread_start/turn_start对象与实际参数逐项相等；模型/层正确；正常只有1个turn/start，拒绝后turn/start=0且无成功输出。 |
| SIWC model未指定，正常+拒绝 | 复用siwc原newFixture/signIn/models/SSE handler并包内只包rawbody记录，原检查未关闭；真正Manager.Generate model=""选择目录first；event.Model=first=request.model，finalbytes与fakeHTTP正文逐字相等；拒绝后responses HTTP0、无成功输出。 |

## 证据与具体限制

[完整JSONL](acceptance.jsonl.gz)、[10份观察/实际正文base64/实际RPC](observations.json)、[计数及文件SHA](summary.json)。JSON中的Payload与actual_http_bodies为原始bytes的base64，保存独立actual body SHA256；未记录OAuth凭据或HTTP Authorization。Go JSONL长日志按同test的Output片段完整重组后解析观察，无测试重跑。

Codex固定instructions的thread/start发生在barrier前，正常和拒绝例都已经创建thread；测试的‘拒发0’只指首次含受控prompt的turn/start。不声称所有RPC为0，不声称remote HTTP序列化或隐藏模型上下文可见。该层是adapter_arguments，对实际RPC对象比较，不把它冒称wire bytes逐字相等。HTTP/SIWC原正文的byteequal是本地合成接收观察，不能证明真实第三方隐藏上下文。

SIWC在包内证明model空时实际first可正常推理与最终observer；**PostgreSQL真实SIWC秘书未指定model的ordinary/纯claim兼容、未知recipient不能raw授权尚缺独立闭环**，没有为测试新增公开修改官方endpoint能力。未证明秘书/Run/manual完整生命周期、State/Export gate、wholequota或会话保序。线上11题、真机、一天用户试用未完成。
