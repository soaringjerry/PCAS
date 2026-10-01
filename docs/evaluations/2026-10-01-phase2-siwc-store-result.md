# 未知SIWC model的实际Store链路验收结果

首次正式执行3/3子例PASS，退出0，1.683s，count=1，0失败、0跳过、0重跑。Go报告1个顶层test与3个子test。compile-only gate也退出0；test-only导出桥没有import cycle。产品与旧测试没有修改。

## 版本与命令

- 产品基线：c29d7afd85b4e430fec5de1e0799f865086e346e（协调者组合产品，后端a5046d53ca2d9535a0c265096ff81447fcd4af35）。
- 冻结预期commit：3eb1611ec5a86cff6947931fea3f8ad22b97a4c8；harness执行HEAD：5cb28bc809a05c958265fbb1c0fba182a9efa4ff。
- 隔离分支phase2/siwc-store-acceptance，目录/tmp/pcas-phase2-siwc-store-acceptance。
- Go1.26.8 linux/amd64；真实合成PostgreSQL phase2_c（loopback 33273），每子例唯一schema，cleanup只删己schema。数据库URL须严格匹配指定合成目标才运行，无skip；未连接生产/真实模型/账号。

```sh
go test ./internal/ai/siwc -run '^$' -count=1
PCAS_TEST_DATABASE_URL='postgres://phase2_c:phase2-synthetic-only@127.0.0.1:33273/phase2_c?sslmode=disable' \
PCAS_PHASE2_SIWC_EVIDENCE_DIR='/tmp/pcas-phase2-siwc-store-acceptance/docs/evaluations/2026-10-01-phase2-siwc-store-evidence' \
go test ./internal/ai/siwc -run '^TestPhase2SIWCUnknownModelRealStore$' -count=1 -v
```

原日志：[compile gate](2026-10-01-phase2-siwc-store-compile-original.log)、[首次正式执行](2026-10-01-phase2-siwc-store-run1-original.log)。没有调整或降低既有断言。

## 实际链路与证据

测试导出桥仅封装已有newFixture/signIn，保留fake OIDC/catalog/SSE原handler的Bearer/PKCE/请求shape断言；在test内捕获真正到达loopback `/v1/responses` 的正文。外部siwc_test包调用真实postgres.Open/Migrate/SetModels/Snapshot/Execute/DeskTurn及ContextAttempts/ContextAttemptSnapshot。

每子例开始时provider.Model与登录账户.Model都为空、合成登录可用；每例真实请求数为1。Registry.ContextObserver使用产品generationContext现有链式机制，与Store内部bound observer同时工作，独立捕获prepared→before_dispatch→dispatched三个事件，每个的provider/protocol/endpoint/model都与实际请求一致；adapter最终选择catalog第一可见model `first`。

三个事件的Payload逐字节等于实际HTTP正文，Store snapshot也逐字节相等。Store持久attempt均completed/retained，具有dispatched_at/completed_at；manifest recipient使用first而非unknown，具有合法route fingerprint，observation_layer=serialized_request，InputBytes等于实际正文长度。external_receipt始终unknown。

| 子例 | 实际请求bytes | Input/Used | 独立结果 |
| --- | ---: | --- | --- |
| ordinary_question | 4381 | 0/0 | 原问题真实发送，返回exact合成reply，普通问答没有被未知model阻断。 |
| independent_claim | 4697 | 1/1 | public capture→acceptCandidate产生确切claim ref；实际body含claim文本，raw禁用marker不在body；fake明确used=[M1]，产品记录相同ref并标supported。 |
| raw_source_refused | 4393 | 0/0 | public Ingest创建零claim raw source；ContextRecipient与两种SetSourceAuthorization（最小selection/客户端猜first）均ErrUnavailable，SourceAuthorizations为空；普通问答仍发送，raw禁用正文marker不在实际body/snapshot，Input无source。 |

完整合成原始证据包括HTTP正文、字节数、SHA256、三个独立观察事件、持久attempt/manifest、exact snapshot相等判定与原始source/claim/request ref：

- [ordinary_question.json](2026-10-01-phase2-siwc-store-evidence/ordinary_question.json)
- [independent_claim.json](2026-10-01-phase2-siwc-store-evidence/independent_claim.json)
- [raw_source_refused.json](2026-10-01-phase2-siwc-store-evidence/raw_source_refused.json)

证据是实际Store→SIWC适配器→loopback HTTP fake链路。它不证明真实OAuth端点、真实账号或第三方模型内部上下文；Used是测试fake的明确声明再经产品校验，不能当作模型内部实际使用证明。

## 只读记录的既有CI fixture风险

`internal/testsupport/golden/main.go:146`的默认deputy回复仍是plain checklist，secretary才改成JSON；新deputy产品协议要求output/used结构化输出。此为源码盘点，不是本次测试失败或本次改动。等待协调者所运行CI的具体失败再按授权迁移fixture；本任务没有修改该支持文件或旧测试。
