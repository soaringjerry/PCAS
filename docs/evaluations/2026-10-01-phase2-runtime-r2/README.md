# Phase 2 完整组合有因复验 R2：尚未全部通过

## 版本与单次执行

产品与 harness 精确 HEAD **`a5046d53ca2d9535a0c265096ff81447fcd4af35`**，独立工作区 `/tmp/pcas-phase2-c`、分支 `phase2/runtime-full-r2`，2026-10-01 UTC。相对R1包含产品删除审计骨架/容量反馈修复、共享标签与旧Desk role，以及C按真实operation ID查询、按实际system输出协议响应的夹具修正。R1原失败报告完整保留。

**退出1；31顶层：30 PASS / 1 FAIL；69叶级：67 PASS / 2 FAIL；0 SKIP。** 一次执行36.467秒。逐例独立合成schema、本地假HTTP，无真实模型、私人数据或通知，未修改产品或原会话五组断言。

```sh
# PCAS_TEST_DATABASE_URL由专属合成DB环境提供
PCAS_PHASE2_EVIDENCE_DIR=/tmp/pcas-phase2-c-runtime-full-r2-evidence go test ./internal/postgres -run '^(TestPhase2Runtime.*|TestConversationOrderAcceptance_.*|TestDirectCaptureEntersDefaultRunAsSourceBacked)$' -count=1 -json
```

[精确31项选择](selection.json)同R1：25 runtime、原保序5组、指定旧manual claim fixture1组。

## 剩余失败：C采纳正对照装配

唯一失败顶层 `TestPhase2RuntimeStateExportAndDerivedBlocksRecheckCurrentRecipient` 的 `route`、`disabled` 两叶均在第46行 `adoptRun: version conflict` 终止。原始两份finished-run证明实际Run已完成且 `adopted.auto=true`、`as=doc`、真实actionID非空；测试又采纳同一Run，违反已有自动采纳状态。**这是C夹具重复采纳，不能归为产品读门故障。** source-derived输出正对照已建立，route/disabled变化尚未执行，State/Export/派生Doc/Sample读门及无关owner文档保护本次未抵达。

后续最小适配应读取真实自动采纳的Doc/Sample，保留正文/依赖/训练导出正对照及全部route/disabled拒供断言。需root指定有因复验；本报告不会改为PASS，不重复本批。

## 已通过的闭环及边界

- 原会话保序五组均通过，包括已删除HTTP replay审计骨架、真实自身backend终止与迟到输出隔离；指定旧manual独立claim正对照仍通过。
- E1零claim/pending来源：真实自然授权→DeskTurn、公开授权→DeskTurn/Execute(requestRun)→RunAgents、本地人工GET package四叶全部通过。三gold原文atoms抵达实际HTTP bytes或package；精确retained snapshot byteequal、Candidates/Input/Used=[]分离、exact source rune与最终payload byte映射、recipient/policy/scope stamps验证通过。人工交付保持DeliveredAt非空、无DispatchedAt、external receipt unknown。
- before_dispatch八叶：Desk/Run × replacement/delete/revoke/route，在最终可观察bytes已准备、HTTP尚为0时真实mutation提交，释放barrier后HTTP仍0且attempt终态无虚构dispatch。HTTP已收到并held response的六叶：Desk/Run × replacement/delete/revoke，mutation无需provider释放即可提交，旧结果不能采纳。另实际取消后独立attempt、同/新会话撤权历史测试通过。
- manual撤权/删除重取与旧结果提交、regrant ABA两叶通过；body7日/meta30日显式clock清理后诊断删除而durable deps保留，随后真实撤权禁止旧结果。owner初态64MiB回收最老body后实际外发及wholemetadata实测通过；10000条/wholemetadata仅余128B两叶HTTP0并受控record_capacity反馈通过。
- source API、exact kind/version/KnownAt、hard scope与独立claim/explicitdeny/undo、ancestor三控制通过。真实022→023、023→024/fresh/restart、固定startupCutoff/lease恢复八检查点通过。恢复检查点为必要合成SQL状态，不冒称真实杀进程/重启证据。
- 重复用户问题短句与中文JSON escaping实际位置映射、最终UTF8 bytes估算23999/24000允许、24001拒发HTTP0通过。token值是ceil(bytes/3) estimated，不是实际tokenizer、隐藏上下文或花费计量。合法legacy纯claim读兼容与explicitdeny后不重建通过。

这些通过不补足剩余route/disabled读门。完整Store SIWC、旧manual全面postgres回归、UI、真实进程崩溃重启、线上11题、真机及一天用户试用仍无完成证据。独立AI HTTP/Codex/SIWC适配观察仅沿此前精确版本报告，不当作完整秘书SIWC通过。

## 原始输入证据

[完整原始JSONL](acceptance.jsonl.gz)：11501事件，原JSONL SHA256 `57bba8a1912dd917a59edccf63c29dd5411c7a97c3c332faa5ddc1c6328f56b3`。
[228份合成HTTP/输入/attempt/状态/selection](synthetic-evidence.tar.gz)保留真实raw body bytes及SHA；[summary](summary.json)含全部状态、整数与逐文件SHA。未将Used=[]当未供给，未将间接依赖当实际输入，未将未知外部回执当已成功送达。
