# F8 补验：完整真实后端浏览器套件

执行者：6.1 Sol / high。尚未启动，等待执行位。接手已停止的 F8 工作区 `/root/PCAS` / `fix/timezone-displays`，PR #23，输入 `bddc14b`。本任务不能替换既有本地专项证据；必须单独说明完整套件的失败与修复。

## 已有证据

- #23 最新提交的 [真实后端 CI](https://github.com/soaringjerry/PCAS/actions/runs/36823487398/job/110243927563) 失败：timezone 1/1 和其余 backend 4/4 通过；golden 21/27 通过，G1/G3 各三次均在 arrange 的 projectId 断言失败。
- 同 main 基线、仅新增 Go 测试的 #24 对应完整 CI 通过。因此不能把 #23 失败归为共同的基础环境故障。
- F8 新增一个持久项目，后续 golden arrange 的假模型固定用 `P1`，却断言项目名 A。优先验证共享测试数据与别名假设的关系；这仍是调查线索，不代替根因复现。

## 归属与要求

先复现并定位；优先修复自己新增用例的数据生命周期，让完整 runner 通过。可修改 `web/tests/timezone-backend.spec.ts` 以及 F8 原报告和已授权的验证附件；不能降低 golden 项目匹配断言、关闭失败用例或直接扩大产品改动。若必须改 `web/tests/golden.spec.ts`、shared fixture 或 runner，先解释必要性并取得协调者的文件归属授权。

使用既有隔离 runner、自有 `pcas-test-F8-CI-*` 容器及 HTTP 18138/18139，只清理自建测试数据和已记录 PID。真实账户、生产、其他任务环境不可触碰。复用 Node 22，自己的 node_modules / build，不与其他前端任务并行写入。

修复后跑完整真实后端套件，保留精简日志；相关 lint/type-check，必要时 build。更新 #23 的报告/正文并推本分支，不合并、不部署。对最新提交查询远端 CI，尚未结束则明确等待状态，不能把专项 1/1 当作整套通过。
