# 既有浏览器手动路径盘点（只读）

盘点基线产品 46db0a2，保留所有旧测试和断言。

- `web/tests/settings-things-ux.spec.ts:27` 的 workspace 包含 manual agent，但无 waiting manual run，也没有手动转交/预览/复制/贴回操作；`/v1/models` mock 目前只有 ChatGPT enabled 信息（约70行），没有 providers。仅当未来增加手动选择场景时，须给该 fixture 增加 enabled/available 文字配置 providers，构造明确 manualRecipient/contextTask，并新增 package GET fixture；现有非手动断言没有授权或必要迁移。
- `web/tests/fixes.spec.ts` 测 failed requestRun 重试，但 agent channel 为 api，保持原用途/请求的旧断言不变；没有旧 manualRecipient 或 brief复制断言可迁移。
- 其余 mocked 文件（secretary、buttons、notify、timezone、usability-acceptance）未发现 manual运行路径；source.status=manual 属于资料导入状态，不能误当成交接路径。
- `web/tests/support/real-backend.sh` 现有套件：timezone-backend、golden、backend、continuity、model-api、chatgpt-direct。只读全文搜索未发现手动交接、package端点、waiting manual 或贴回操作。当前 real-backend 浏览器套件没有手动交接全链路证据，也没有需要修订的旧manual断言。

本次新增独立 mocked `phase2_manual.spec.ts`；CI仅追加此文件到 browser-mocked 现有7文件显式清单。既有real-backend三轮、gate、重试策略完全不变。
