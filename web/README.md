# PCAS Web

React/TypeScript 前端，所有业务数据由 Go API 持久化到 PostgreSQL。登录使用 HttpOnly 会话 Cookie；前端没有内置凭据、示例数据或模拟 AI 回复。

## 开发

需要 Node.js 22.12+。Go API 在 `127.0.0.1:8090` 运行后：

```sh
npm ci
npm run dev
```

Vite 将 `/v1` 代理到 Go 服务。生产环境由 Go 直接提供 `dist`。登录密码、模型和订阅配置见 [部署说明](../docs/deployment.md)。

## 页面与数据

- 首页：左「今天」（到点置顶、在等你、按时间、这几天），中间秘书对话（`Secretary`，调用 `/v1/desk/turn`），右侧项目和想法。手机宽度下「今天」在前，秘书只显示最近一轮。
- 事项页：只读信息行（点击即让秘书修改）、说明、子任务、文档、统一「动态」（修改历史 + 副手结果），底部是带 `thingId` 的秘书。
- 按钮原则：完成打勾、撤销 / 改、外发删除花钱前的确认；可撤销的操作走 `dispatchUndoable`，toast 附【撤销】。
- 资料库：记忆确认/纠正/固定保留/授权/删除范围，三种召回模式，原文与附件展开，处理作业、提醒记录和训练数据。
- 设置：时区（支持搜索）、自动处理、预算、AI 可见范围、官方 ChatGPT 订阅登录、提醒通道（本设备推送、Telegram）、导出与退出。
- `public/sw.js` 只处理推送和点击，不缓存页面；`manifest.webmanifest` 让站点可以添加到主屏幕。

`StoreProvider` 串行发送幂等命令，附带 `requestId` 和服务端修订号 `expectedRevision`。只有切换类命令（`toggleCheck`、`toggleTrigger`、`toggleContextMemory`）及 `bulk*` 批量命令要求修订号与当前值一致；后台更新修订号不会阻止其他命令。撤销仍校验动作的 `afterHash`，新建仍检查 ID 是否重复；相同 `requestId` 和请求体的重放不会重复执行，复用 `requestId` 但修改请求体（包括修订号）仍会冲突。遇到冲突时前端刷新状态；只有服务端成功才反馈保存。`src/domain/agent.ts` 生成的文本是界面预览，实际交接和权限在服务端重新计算。

## 验证

```sh
npm run lint
npm run type-check
npm run build
```

浏览器回放会创建资料，务必使用临时测试数据库和测试实例：

```sh
npx playwright install chromium
PCAS_TEST_BASE_URL=http://127.0.0.1:18090 PCAS_TEST_API_TOKEN=test-only-secret-at-least-32-characters npx playwright test
```

新工作区在首次登录加载时使用浏览器的 IANA 时区；非浏览器客户端未提供时区时默认 UTC。已有设置不会被浏览器覆盖，首页可一键切换；关闭提示后同一浏览器不再提示。首页分组、日期和时间线按工作区时区计算，包含夏令时。

不需要后端的 mock 用例：`secretary`、`fixes`、`notify`、`buttons`、`timezone`（CI 的 browser-mocked）。

接真实后端的 `timezone-backend.spec.ts`（新工作区首次登录）、黄金路径 `golden.spec.ts` 和旧回放 `backend`、`continuity`、`chatgpt-direct`、`model-api` 由脚本自动起临时数据库、`pcas serve/worker` 和假的外部服务（模型、Telegram、Web Push），结束后自行清理（CI 的 browser-real-backend）：

```sh
bash tests/support/real-backend.sh   # 在仓库根目录执行，说明见 tests/support/README.md
```

真实模型调用需要独立的账户验收。
