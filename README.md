# PCAS

PCAS 把待办、想法、项目和 AI 工作放在同一套个人连续记忆之上。前端通过 Go 服务读写 PostgreSQL；浏览器只保留未发送的草稿和界面偏好。记忆的原文、陈述、版本、证据与授权由统一服务维护。

- [记忆架构定稿 1.0](docs/memory-architecture.md)
- [后端接口、配置与验证边界](docs/memory-service.md)
- [部署与模型接入](docs/deployment.md)
- [Sign in with ChatGPT 套餐授权](docs/chatgpt-plan-auth.md)
- [跨应用接入、归档与摘要](docs/connectors.md)
- [固定场景真实模型验收](docs/evaluations/2026-09-29.md)
- [产品需求](docs/prd.md)
- [前端](web/README.md)

## 运行

需要 Docker Compose。复制 `.env.example` 为 `.env`，设置独立的随机数据库密码、所有者 UUID 和至少 32 字符的随机 `PCAS_API_TOKEN`。不要提交 `.env`。

```sh
chmod 600 .env
docker compose build api
docker compose up -d --no-build
```

默认入口 `http://127.0.0.1:12352`。登录密码是服务端 `PCAS_API_TOKEN`。公网部署设置 `PCAS_BIND_ADDRESS=0.0.0.0` 和精确的 HTTPS `PCAS_PUBLIC_URL`。数据库不发布端口。

设置页通过 Codex 设备登录接入 ChatGPT 订阅。官方 Sign in with ChatGPT 直连通道暂时默认关闭；可通过 `PCAS_CHATGPT_DIRECT_ENABLED=true` 开启开发验证入口，且须完成真实登录、生成、刷新及撤销验收并重新连接后才成为默认。默认文本模型固定为 `gpt-6.1-sol`。设置页支持填写 OpenAI 兼容 API 地址、密钥与模型；服务端 JSON 配置继续支持 OpenAI Chat Completions、Responses 和 Anthropic Messages。手动交接无需模型账户。默认向量模型为 OpenAI `text-embedding-3-small`，需配置独立 API Key；设置页可补建旧资料向量。本地中文向量服务作为可选 profile 保留；音频转录仍使用独立的通用模型配置。设置页可管理 Webhook、定时拉取、文件夹同步和 ChatGPT/Claude 归档导入。

## 检查

Go 版本由 `go.mod` 指定，前端需要 Node.js 22.12+。

```sh
make check
# PostgreSQL 必须支持 vector；测试在独立临时 schema 中执行。
PCAS_TEST_DATABASE_URL='postgres://user:password@localhost/test?sslmode=disable' make test-integration
cd web
npm ci
npm run lint
npm run type-check
npm run build
```

真实浏览器回放需要一个使用临时数据库的服务，见 [前端测试说明](web/README.md)。验证范围与未完成的架构评测列在 [服务文档](docs/memory-service.md)，不将适配器测试等同于真实账户调用或完整召回率验收。
