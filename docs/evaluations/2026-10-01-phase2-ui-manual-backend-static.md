# 真实 manual 浏览器补验：harness 与静态门

冻结预期：`3df4049972c88bab89f1d4bfa69e2dff257878e2`。本提交只新增独立 spec 与评测证据，未改 runner、产品、旧 spec、原 26 mocked 断言或三轮 gate。尚未动态运行，不报告真实闭环通过。

拟执行产品由 root 指定 `207bd7062f02cf95d4d29ebaba1391d25a7972f1`；当前编写 worktree 的旧产品 `fb1c3eae593527640de156fff806d8789ab5e282` 仅用于编辑/静态类型检查。正式运行前应另建隔离树、带入本 harness、独立 build 匹配产品 dist，并记录最终组合 SHA。

## 静态结果

| 检查 | 退出与结果 |
|---|---|
| ESLint 新 spec | exit 0 |
| 首次 targeted strict TypeScript | exit 2，TS2688：共享依赖没有 `@types/node`；原日志保留 |
| 同命令明确添加真实 Node typeRoots | exit 0，无类型诊断 |
| Playwright --list 新 spec | exit 0，仅 2 tests / 1 file，没有启动浏览器、API 或 DB |
| git diff --check | exit 0 |

Node `v24.18.0`，TypeScript `5.9.3`，Playwright `1.63.0`，ESLint `10.11.0`。工具依赖临时借用 `/tmp/pcas-phase2-ui/web/node_modules`；`@types/node 24.19.0` 仅安装在 `/tmp/pcas-phase2-manual-backend-tooling/node_modules/@types`，不改仓库 package/lock。临时 node_modules symlink 已移除。

命令（工作目录 `web`）：

```sh
node node_modules/eslint/bin/eslint.js tests/phase2_manual_backend.spec.ts
node node_modules/typescript/bin/tsc --noEmit --strict --target ES2022 --module ESNext --moduleResolution bundler --lib ES2023,DOM --types node --skipLibCheck tests/phase2_manual_backend.spec.ts
node node_modules/typescript/bin/tsc --noEmit --strict --target ES2022 --module ESNext --moduleResolution bundler --lib ES2023,DOM --typeRoots /tmp/pcas-phase2-manual-backend-tooling/node_modules/@types --types node --skipLibCheck tests/phase2_manual_backend.spec.ts
PCAS_TEST_BASE_URL=http://127.0.0.1:9 PCAS_TEST_FIXTURE_URL=http://127.0.0.1:9 node node_modules/@playwright/test/cli.js test tests/phase2_manual_backend.spec.ts --list
```

上述 node 均使用 `/root/.nvm/versions/node/v24.18.0/bin/node`。list 的 port 9 只用于加载要求显式环境的测试模块，无网络访问。日志见同前缀 `static-evidence/`；空日志代表工具无诊断，退出码由 exec 记录。

## 静态审阅调整

- catalog 等待在打开表单前注册，真正 await 该 UI 发起的 `/v1/models` GET 并断言；没有吞超时的悬挂 promise。
- handoff 行以 prompt 与真实 paste textbox 区分；当前行再要求 copy button enabled。撤权旧行固定在唯一行时获得；重新生成后断言两行、恰一 stale alert、恰一 enabled 新行，并以真实 run ID 的 GET 关联新包。未依赖 Activity 倒序的 last。
- source 授权只传 exact ref 三字段，不带 ingest duplicate 等响应附加字段。
- Clipboard 使用真实 Chromium permission/write/read，不覆盖方法；HTTP 仅被动 observation。失败会保留 trace，finally 附相关非秘密 JSON，不保存 token/headers/cookies。

## 文件 SHA256

| 文件 | SHA256 |
|---|---|
| new spec | `2073622d8bf778a05570a0d1ae6afa99da7f1e798db61f626efb8f61f81ec6c5` |
| unchanged support/real.ts | `1951564645a454fa11344ec189b5d4253447f06631f4b946610c999d4a19c332` |
| unchanged support/real-backend.sh | `f9d9575663b7d1b12ca42a76ec92af607e94fdbfd2e3d515ee0c8409067ca980` |
| unchanged playwright.config.ts | `3121f29046eccf990006b5c5dc303f7e13db3a72f1e4ad8ae1bc35005573ddd9` |

边界仍为冻结表的两例：真实来源授权、目标选择、每次新包、Chromium clipboard、真实 stale/精确 409 拒绝和恢复提交。没有真实外部接收证明，没有重跑原 mocked 矩阵。
