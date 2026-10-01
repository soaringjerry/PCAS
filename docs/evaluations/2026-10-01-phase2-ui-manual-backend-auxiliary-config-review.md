# 真实 manual auxiliary：只读共享配置核对

基线 current clean `2b3d9bd3891034a85f22e434a2806b4edab73633`，产品dc2705d、新spec84e75a62。root要求核对既有golden/auxiliary结束时是否污染golden name/model/protocol/route与agent enabled/available。本次仅只读与--list，不运行任何浏览器/API/服务/DB，不改产品/新spec/旧spec/runner。

## 具体结论

没有发现本范围内确定的golden route或agent污染，因此不增加无必要的恢复写操作。不是已执行整个追加suite的绿证明；最终由CI实际运行共享workspace验证。

| 先行spec | 实际写入范围与最终状态 |
|---|---|
| timezone-backend | workspace timezone/followUps及合成事项；最后恢复followUps=true，不写provider/agent |
| golden | beforeEach仅timezone=AsiaShanghai/followUps=true；其它事项/undo/notify push/telegram/fixture rules写入，不写model connection或golden/manual enabled |
| backend | capture/独立memory确认纠正/export；到settings只点击export，不配置模型/agent |
| chatgpt-direct | 仅展开两connection卡片/读官方account（empty/unready），没有点击Continue、没有登录模型或改agent |
| continuity | 合成connector/webhook/archive导入，不写provider/agent |
| model-api | 保存text/embedding synthetic BaseURL与fake keys、排队vector rebuild；只保存独立managed IDs openai-api/openai-embedding，不替换golden |

关键产品证据：

- `internal/testsupport/golden/main.go:234` 生成独立file provider：id/model=golden，name=验收假模型，protocol=openai，base_url=己fake URL，cost_mode=free，extraction_provider=golden；fixture(control)重置规则/事件，并不改此file。
- `internal/ai/settings.go:65` roleID(text)=openai-api，embedding=openai-embedding；Providers先复制Config.Providers，仅匹配同ID替换/否则append，因此model-api两次save不改golden route/name/model/protocol/endpoint。
- `internal/ai/provider.go:177` unmanaged openai且无KeyEnv的golden可用性不依赖后来设置的managed key；provider仍available。
- `internal/postgres/workspace.go:108` 首次manual Enabled=true；121为file golden创建Enabled=true api agent。ensureOwner用INSERT ON CONFLICT DO NOTHING；先行spec没有任何updateAgent/setAgent/agent toggle写入，故无确定禁用污染。snapshot从现registry算available/protocol。
- model-api只改URL/key，OpenAIConnection save保其draft.default；初始managed text并非golden extraction，原ConnectionStatus.default=false。即使managed default改变也不会改golden自身route，当前旧spec没有改default的动作。

原CLI explicit文件参数不决定显示顺序。保持config fullyParallel=false/workers1，真实加载同auxiliary清单的静态 `--list` exit0，6 tests/5 files，顺序：backend → chatgpt-direct → continuity → model-api → phase2 manual两例。golden是runner此前另一Playwright进程（同己workspace），顺序仍先golden再auxiliary；三轮选择/golden repeats/status累计未动。

```sh
PCAS_TEST_BASE_URL=http://127.0.0.1:9 PCAS_TEST_FIXTURE_URL=http://127.0.0.1:9 /root/.nvm/versions/node/v24.18.0/bin/node node_modules/@playwright/test/cli.js test tests/backend.spec.ts tests/continuity.spec.ts tests/chatgpt-direct.spec.ts tests/model-api.spec.ts tests/phase2_manual_backend.spec.ts --list
```

新spec本来从真实UI catalog选golden exact name且检查available，并核server boundmodel/protocol/fingerprint；新增openai-api不会被误选，也不需要禁止其他合法配置。新spec只重置goldenfake规则符合当前路由独立性。原新两例PASS来自独立runner，不冒称已经在旧auxiliary后执行。本核查没有启动追加suite，也没有恢复设置/删数据/改旧断言。

list原日志同前缀auxiliary-list-original.log。临时共享node_modules symlink已移除，记录仅本UI文档；root已接收旧action-lineage归档写权，本次未触碰那些证据/结果。
