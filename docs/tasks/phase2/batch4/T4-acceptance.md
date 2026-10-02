# 任务 T4：第 4 批独立验收

执行者 6.1 Sol，**不能是做 I、L 的执行者**（可以是做 V 的执行者）。分支 `phase2/b4-T4-acceptance`，从 `origin/phase2/batch4` 建（集成分支建好之前先从 `origin/main` 建，之后变基）；工作区 `/root/PCAS-wt/b4-T4`；Draft PR 的 base 是 `phase2/batch4`。

先读 [并行方案与数据约定](../parallel.md) 和 [第 4 批契约](README.md)。你只看契约写测试，不看实现的做法。

## 要交付什么

契约第 4 节的全部序列（I1–I14、L1–L7），每条至少一个测试，测试名带编号（`TestPhase2B4_…`）。再加浏览器用例 W1–W4。评测（R16–R19）由任务 V 负责，不在你的范围里。

写法沿用第 1 批验收（`phase2_b1_*_test.go` 里的辅助函数可以直接用，不要改）：

- **导出文件自己合成。** 按 ChatGPT 真实导出的结构（zip 里一个 `conversations.json`，每段对话有 `mapping`、父子节点、`create_time`、角色、分支），由测试生成，放几张假图片当干扰。大小上限相关的用例不要真的生成几百兆：让实现把上限做成测试里可以调小的变量（契约 R2），你调小了测。
- **走真实入口。** 预览、导入、暂停、继续、进度、删除都走 HTTP 接口；I2、I10 断言假模型实际收到的内容。
- **中断和并发来真的。** I3、I9、I12 需要后台真的在跑、真的被暂停或被杀，不要用顺序执行冒充。
- **L5 要直接查表。** 除了看接口返回，还要直接扫 `model_usage` 的每一列，确认没有任何一段测试用的正文。
- **时间。** 不写死必须在未来的日期；I13、L4 至少覆盖悉尼和上海，L4 加一个夏令时切换日。
- **冻结预期。** `testdata/phase2/b4-gold.json`，先提交再写测试，之后只增不改。

## 文件

`internal/postgres/phase2_b4_*_test.go`、`testdata/phase2/b4-gold.json`、`web/tests/phase2-batch4.spec.ts`、`web/tests/phase2-batch4-backend.spec.ts`、CI 里加新浏览器用例的那一行、`docs/evaluations/<日期>-phase2-batch4-acceptance.md`。

不改产品代码。不改已有测试的预期；协调者批准要改的会单独交给你。

## 纪律和报告

和 [T2](../batch2/T2-acceptance.md) 的「纪律」「验收报告」两节相同。
