# 第 4 批独立验收：原轮结果与先存原话补充（2026-10-03）

产品基线：`origin/phase2/batch4` 的 `b526328c6529056513bf533d370d799f09430b33`。T4 已变基；CI 冲突处理保留 batch2 和 batch4 的步骤。

本轮被测提交：`c719783`（该产品基线 + 原有 T4 测试与 CI 接线）。所有原轮 Go、模拟浏览器、真实后端 W4 均在 `/root/PCAS-wt/b4-T4-original-run` 的同一固定提交执行，没有加入 I15–I20、W5。没有改产品代码，没有读取 I/L/U4 的新实现，没有修改原有验收文件的预期。

## 新增序列：已交付、未验收

第 11 节和 T4 末尾补充的预期先在 `631ab49` 追加冻结到 [b4-gold.json](../../testdata/phase2/b4-gold.json)，所有旧条目不变。第 12 节 C 序列属于补充批 4b，未编写。

| 序列 | 新测试 | 当前状态 |
|---|---|---|
| I15 | `TestPhase2B4_I15_LaterStoresIndexedOriginalsWithoutExtracting`；`TestPhase2B4_I15_NowPositiveControlCallsExtraction` | 编译通过，未运行 |
| I16 | `TestPhase2B4_I16_HeldOriginalReachesSecretaryInAnotherConversation` | 编译通过，未运行 |
| I17 | `TestPhase2B4_I17_StartOrganizingReleasesRealPriorityTenExtraction` | 编译通过，未运行 |
| I18 | `TestPhase2B4_I18_NewSecretaryUtteranceExtractsWhileImportHeld` | 编译通过，未运行 |
| I19 | `TestPhase2B4_I19_HeldImportPausesResumesAndDeletesItsClosure` | 编译通过，未运行 |
| I20 | `TestPhase2B4_I20_OmittedOrganizeDefaultsToHeldIndexedOriginals` | 编译通过，未运行 |
| W5 | `W5 默认先存着；存好后开始整理，进度随接口返回增长`；`W5 确认前可以改为现在整理，表单实际发送 now` | lint、TypeScript 检查通过，未运行 |

后端测试在 [phase2_b4_deferred_test.go](../../internal/postgres/phase2_b4_deferred_test.go)。分段、分词、向量和抽取都由真实 `Claim` 与处理函数运行，不强制领取被 hold 的抽取任务；直接检查每条原话的 chunk、词索引、向量、未领取任务和 `hold_organizing`，向量只用本地免费的确定性服务，单独抓请求，不混入抽取模型调用数。

I16 抓另一段秘书对话的真实 HTTP 请求；I17 通过 HTTP 释放 hold，并验证真实抽取及优先级 10；I18 用真实秘书入口记录新原话；I19 在解析 goroutine 正在逐批提交时真实暂停、继续，再走归档删除闭包。W5 在既有模拟浏览器文件中，随已有 CI 文件入口自动包含。

## 原轮统一运行

使用自有 pgvector 16 容器、tmpfs、随机本机端口；只调用本地假模型/向量/通知服务。不读线上 `.env` 或 `config/`。产品 `make build` 通过。前端 `npm ci`、lint、type-check、build 均通过（本机 Node 20.19.5；package 要求 >=22.12，npm 给出 engine 警告）。

命令：`GOFLAGS=-v make check`，其中数据库测试通过显式 `PCAS_TEST_DATABASE_URL` 指向自有临时库；浏览器两个文件分别 `--retries=0 --reporter=list,json`；W4 使用共享真实后端 runner、`PCAS_IMPORT_CHUNK_SIZE=1`，并设置随机 HTTP 端口。

Go 全量：正在运行，完成后追加逐序列结果。

| 浏览器序列 | 实际结果 | 到达的断言 / 限制 |
|---|---|---|
| W1 | 两个用例均失败 | 构建产物上预览请求恰好一次，数量/日期/禁止项说明/实际新存数量的检查已到达；失败在整页禁止词检查读到其他设置内容里的「来源」，确认导入后续断言未到达。待协调者确认检查范围 |
| W2 | 四种状态均失败 | 进度检查之后，整页禁止词检查读到已有导出说明里的「来源」；暂停/继续等后续断言未到达。已询问是否限于导入区域 |
| W3 | 通过 | 构建产物上四种错误各有不同说明、无内部错误码、确认前无导入，390px 无溢出、无 pageerror |
| W4 | 失败（120 秒超时） | 轨迹只有 `POST /v1/connectors/archive → 202`，没有 preview。旧合成文件名是唯一前缀 `.json`，走了通用 JSON 上传；后续 pause/resume/资料库断言未到达 |

构建产物的模拟运行：1 个通过、6 个失败、0 跳过、0 重试、0 flaky；W4：1 个失败、0 跳过、0 重试。没有宣布本批通过。

首次模拟服务启动从仓库根目录运行 Vite，未提供应用入口；已终止自有进程、保存 invalid-fixture 日志并修正启动目录。该启动不计产品结果。随后开发模式的模拟尝试为 7 个失败：W1 出现两次 preview；W3 的轨迹明确出现样式模块 `ERR_NETWORK_CHANGED`，重载后应用未挂载。已改用同一提交的 dist 构建产物，再作一次明确改变服务方式的核对；没有改断言或设置 Playwright 重试。构建模式 W1 次数为 1、W3 通过，开发模式失败日志/JSON仍保留。这两处开发夹具现象不归为产品失败。

## 发现与处理归属

1. W1/W2 的禁止词范围：待协调者确认。保持旧冻结预期和旧断言，不自行决定放宽或改产品。W1 次数在构建产物上为 1，原开发模式次数疑问已由环境对照消除。
2. W3 开发模式在重载时遇到资源 `ERR_NETWORK_CHANGED`，属夹具/环境；构建产物上完整通过。保留开发失败日志，不把改变服务方式后的核对称为重试碰运气。
3. W4 的合成文件名使入口与预期不符：归属 T4 夹具。交付版本已改为官方 `conversations.json`；没有改预览、暂停、继续和查看原话的预期，本轮原失败仍保留，修正版本未重跑。

构建模式截图、轨迹和原始输出保留在固定验收工作区的 `web/test-results/phase2-b4-original-mocked`、`web/test-results/phase2-b4-original-real`，以及各自临时运行目录；全是合成资料。后续测试修改或产品修复需要在新的明确提交上重新统一验收，不能把本轮未到达的断言当作通过。
