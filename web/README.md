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

- 首页和事项页：待办、想法、项目，副手执行与手动交接、结果采纳。
- 资料库：记忆确认/纠正/固定保留/授权/删除范围，三种召回模式，原文与附件展开，处理作业、提醒记录和训练数据。
- 设置：自动处理、预算、AI 可见范围、官方 ChatGPT 订阅登录、导出与退出。

`StoreProvider` 串行发送幂等命令，附带服务端修订号，冲突后刷新；只有服务端成功才反馈保存。`src/domain/agent.ts` 生成的文本是界面预览，实际交接和权限在服务端重新计算。

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

回放覆盖登录、记录、记忆确认/纠正、来源查看、创建项目、手动交接和重载持久化。真实模型调用需要独立的账户验收。
