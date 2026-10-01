# Phase 2 独立手动交接 UI 验收预期（冻结）

产品基线：46db0a2。验收工作区：/tmp/pcas-phase2-ui-acceptance。
框架：现有 web/playwright.config.ts，新增 web/tests/phase2_manual.spec.ts。
所有内容均为合成资料；通过浏览器截获 /v1 API，仅证明前端行为。预览站点只提供此产品构建产物，不连接生产、账号或真实模型。禁止把浏览器 mocked API 结果与另一个后端矩阵拼成真实全链路证据。

| 场景 | 独立预期 |
| --- | --- |
| 接收者筛选 | /v1/models 中可用文字生成配置与 enabled/available 非 manual workspace agents 交集；排除 disabled、unavailable、embedding、transcription、不支持 protocol、无 model、仅模型目录存在者。展示友好名。 |
| 新请求/换接收者 | 明确选择接收者才可提交；新 requestRun id；manualRecipient 只有 provider。不得发送 fingerprint/scope；保留原用户 prompt、事项 notes 与独立 owner 文本。 |
| 预览/复制 | 每次操作新 GET /v1/workspace/runs/{id}/package，fetch cache:no-store；只显示/复制 package，绝不可读 brief 作为交接正文。 |
| 交付表述 | PCAS 交付与外部收到分开；external_receipt unknown 时文字明确外部未知，不能声称外部已收到。 |
| 晚响应 | 请求中 revision、run状态、stale、route或授权改变时，旧 package 不显示也不复制。已显示的旧预览随这些状态变化清除。 |
| 响应绑定 | package run_id、attempt交付时间与当前绑定一致才可使用；manifest接收者错误或未交付必须拒绝。 |
| 失败/原目标重试 | package失败或 paste失败清预览，给出重新生成入口；向原接收者重试保留完整原 ManualRecipient。 |
| 换目标 | 选另一接收者建立新 run，旧 run不改绑定。 |
| 窄视口 | 390px 浏览器视口下交接与选择表单无水平溢出，主要按钮可用；称窄视口，不称真机。 |

执行纪律：冻结表先提交；矩阵首次执行保存完整原日志、退出码、总数和失败。无 skip、无弱化断言、无靠偶然重跑获取绿色。产品问题交实现者修复后，以新产品 SHA 另记复验。保存桌面与390px截图、精确产品及harness SHA。业务实现与旧测试不修改。
