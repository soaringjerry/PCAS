# 消费者 web CI：两个候选分别记录

## 最终候选 cf3015c

`cf3015cd67cf112933dfa51badbda8507de30834` 的 [web 检查](https://github.com/soaringjerry/PCAS/actions/runs/36888959215) 于 2026-10-01 16:03:03 UTC 成功，job `110459379396`。npm ci、lint、type-check、build 实际执行成功；npm 下载缓存命中不等于跳过这些命令。日志报告 0 vulnerabilities，Vite 输出仍为 `index-KRxlz-XR.js` / `index-Bn5qMKG9.css`。

实际 checkout `ba946b860dea2ef81b83c2c1ec3eb5c63f4d1562` 是 GitHub PR 检查合成提交，父项 `6fd9d173a738cddf1ee9f4ed7e8bcb7b07ed1973`、`cf3015cd67cf112933dfa51badbda8507de30834`；其 tree 与本地最终候选均为 `52b5a793517add9741df26a08ca64950aa05f034`。root 只读 GitHub 对象和完整日志，不在本地运行产品或测试，未合并 main。

[完整日志](cf3015c-original.log.gz) 解压 SHA256 `dd87dfb416d4e9a8fd4d70aecb41e1e5e6ef7ac86f39a7642ee8ed7ed9d13ae2`。只证明本版本 web 工程门；不替代浏览器、后端、真实模型或用户验收。

## 前一候选 c8adbae

`c8adbaeb5c02daa9e83f90f74c216d8cf9ee22bf` 的 [web 检查](https://github.com/soaringjerry/PCAS/actions/runs/36887421660)于 2026-10-01 15:51:21 UTC 成功；job `110454170789`，15:50:44 UTC 开始。npm ci、lint、type-check、build 均成功。本报告只表示 web 工程检查，不代表浏览器、真实模型或用户验收。

CI checkout 是 GitHub 为 PR 检查合成的 `de9906eeece777e5016d50b0d85a1d0b72a68b40`，父项为来源分支 `6fd9d17` 与候选 `c8adbae`，并未合入 main。协调者只读核对 GitHub Git 对象及本地候选，二者 tree 都是 `e615cd44aa99df110a03592d965afafb06a21c8b`，实际检查内容与候选完全相同。

[完整原日志 gzip](c8adbae-original.log.gz)解压后 SHA256：`fa0a9625cf04a37c8cc9f40e4be606fa42e85029a7818f37bf3a1fc7168b9b80`。日志由协调者只读下载，未本地执行产品构建或测试。Vite 输出 `index-KRxlz-XR.js`；构建成功不替代其它尚未完成的 CI 门。
