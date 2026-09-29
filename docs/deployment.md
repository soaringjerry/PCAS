# PCAS 部署与模型接入

服务由 `api`、`worker`、一次性 `migrate` 和 PostgreSQL/pgvector 组成。默认使用 OpenAI 向量 API，本地 CPU 向量服务作为可选 profile 保留。镜像包含前端、Go 程序、官方 Codex CLI 0.159.0、PDF 文本解析、中文/英文 OCR 与音频时长读取工具。服务使用非 root 用户运行。

## 域名与鉴权

本机部署地址为 `pcas.coyumelabs.com`，源站端口为 `12352`。对应私有 `.env` 配置：

```dotenv
PCAS_BIND_ADDRESS=0.0.0.0
PCAS_PORT=12352
PCAS_PUBLIC_URL=https://pcas.coyumelabs.com
```

域名代理负责 HTTPS。`PCAS_PUBLIC_URL` 必须与浏览器实际访问的 origin 完全一致；认证不会信任任意 `X-Forwarded-Host`。登录成功后使用 HttpOnly、SameSite=Strict 的七天 Cookie；HTTPS 部署始终设置 Secure。退出立即撤销会话，API 重启后需重新登录。所有 `/v1` 业务接口需要认证；静态登录页和健康检查公开。

浏览器输入 `PCAS_API_TOKEN` 登录，它不写入前端配置、localStorage 或 Git。外部调用使用 `Authorization: Bearer ...`。原生部署可设置 `PCAS_AGENT_TOKENS_FILE` 为 JSON 数组 `[{"principal":"external-ai","token_env":"MY_AGENT_TOKEN"}]`；令牌引用环境变量，owner 由服务端固定。外部 AI 只能读取授予其 principal 的记录，不能调用工作台管理接口。

初始化与更新：

```sh
cp .env.example .env
# 编辑并生成独立秘密：openssl rand -hex 32；owner：cat /proc/sys/kernel/random/uuid
chmod 600 .env
docker compose build api
docker compose up -d --no-build
docker compose ps
```

迁移带校验和且可重复执行；不要修改已经部署的 migration 文件。镜像更新后先完成迁移，再启动 API/worker。`docker compose logs --tail=50 api worker migrate` 可检查服务状态，日志不输出原文、令牌或模型请求正文。

## ChatGPT 订阅

目前继续使用 Codex App Server 订阅通道。新增的 [Sign in with ChatGPT 套餐授权](chatgpt-plan-auth.md) 默认关闭；只有设置 `PCAS_CHATGPT_DIRECT_ENABLED=true` 才开启开发验证入口。该通道直接调用公开 Responses API，完成真实生命周期验收并重新连接后才成为默认，旧凭据不自动迁移。个人远程 Docker／VM 按上述文档在本机 OAuth 后安全转移凭据。以下为当前使用的 Codex App Server 通道。

暂缓直连的原因是远程授权步骤过于繁琐，当前 Codex 设备登录更方便；详见[暂缓原因与当前决策](chatgpt-plan-auth.md#暂缓原因与当前决策2026-09-29)。

1. 登录 PCAS，进入「设置 → Codex App Server」。
2. 点击登录，在官方验证页面输入设备验证码。必要时在 ChatGPT 安全设置启用设备登录。
3. 页面显示账户后可选择「ChatGPT 订阅」副手。项目固定使用 `gpt-6.1-sol`，默认抽取也使用该模型，不再随 Codex 的默认模型变化；之前因为未登录而阻塞的作业可在资料库中重试。

PCAS 使用 [官方 Codex app-server](https://learn.chatgpt.com/docs/app-server) 管理登录、额度查询和文本生成。认证资料位于独立的 `/var/lib/pcas/codex`，不会借用开发者已有的 Codex 账户或复制浏览器 Cookie。文本线程临时、只读，工具执行、网络搜索和审批请求均关闭。后台 worker 每次调用后释放进程，下次调用重新读取共享登录状态。

[ChatGPT 登录与 API 密钥](https://learn.chatgpt.com/docs/auth) 是不同的使用方式，账户可用模型与额度以官方返回为准。这里通过官方 Codex 使用订阅，不把订阅当作通用 OpenAI API 密钥。订阅不提供本服务所需的独立向量或音频转录端点。

## 通用 API 模型

「设置 → API 与向量接入」可直接填写文本 API 的 Base URL（包含 `/v1`）、API Key、模型和预算单价。默认模型是 `gpt-6.1-sol`，采用 OpenAI 兼容的 Chat Completions 协议。勾选「作为默认副手与后台抽取入口」后，默认入口改为该 API；不勾选则继续使用 ChatGPT 订阅。

向量 API 独立配置，默认是 `https://api.openai.com/v1` / `text-embedding-3-small`。文本供应商与向量供应商可分别使用不同的地址和密钥。空白密钥只在地址不变时保留旧值，更换地址需填写新密钥。设置页价格是每日预算估算的初始值，不代表供应商报价，使用前按实际价格调整。

Compose 将设置保存在 `memory-files` 卷的 `/var/lib/pcas/model-api.json`，文件权限为 `0600`。原生部署设置 `PCAS_MODEL_SETTINGS_FILE=data/model-api.json`。密钥不返回前端，不写入浏览器存储；API 与 worker 每次读取最新设置，保存后无需重启。文件配置仍可使用下述方式，设置页只覆盖 `openai-api`、`openai-embedding` 两个固定入口，不修改其他供应商。

复制 `config/models.example.json` 为被 Git 忽略的 `config/models.json`，在 `.env` 设置 `PCAS_MODELS_PATH=./config/models.json`。密钥只通过环境变量提供。Compose 已传入 `OPENAI_API_KEY`、`ANTHROPIC_API_KEY`、`PCAS_MODEL_API_KEY`；其他命名需自行增加服务环境映射。

下例展示配置结构，`YOUR_*` 和价格须按实际服务填写；示例价格不代表报价：

```json
{
  "providers": [
    {
      "id": "general", "name": "通用模型", "protocol": "openai",
      "base_url": "https://YOUR_PROVIDER/v1", "key_env": "PCAS_MODEL_API_KEY",
      "model": "YOUR_TEXT_MODEL", "max_output_tokens": 4096,
      "input_cny_per_million": 1, "output_cny_per_million": 4
    },
    {
      "id": "vector", "name": "向量模型", "protocol": "openai", "embedding": true,
      "base_url": "https://YOUR_PROVIDER/v1", "key_env": "PCAS_MODEL_API_KEY",
      "model": "YOUR_EMBEDDING_MODEL", "input_cny_per_million": 1
    },
    {
      "id": "speech", "name": "音频转录", "protocol": "openai", "transcription": true,
      "base_url": "https://YOUR_PROVIDER/v1", "key_env": "PCAS_MODEL_API_KEY",
      "model": "YOUR_TRANSCRIPTION_MODEL", "audio_cny_per_minute": 0.1
    }
  ],
  "extraction_provider": "general",
  "embedding_provider": "vector",
  "transcription_provider": "speech"
}
```

文本协议可设 `openai`（`/chat/completions`）、`responses`（`/responses`）或 `anthropic`（`/messages`）。本地 HTTP 服务也可接入。确实免费的服务显式设置 `cost_mode:"free"`。每个副手可以独立设置可见的记忆种类与是否包含推断；服务端再次校验实际授权。

配置更新后重建服务容器，使配置与密钥生效：`docker compose up -d --force-recreate api worker`。模型凭据不会发给前端。API 执行、后台抽取、向量查询/索引、音频转录共同受每日预算约束；订阅按账户限额使用。价格为运营者配置的预算估算，不替代供应商账单。未知用量保留预留额；失败调用不会自动反复付费重试。

## OpenAI 向量与旧资料补建

默认使用 `text-embedding-3-small`，1536 维，通过 `/embeddings` 请求浮点向量。密钥可在设置页填写，也可在 `.env` 设置 `OPENAI_API_KEY` 后重建 API/worker 容器。订阅登录不能代替向量 API Key。参考 [OpenAI 向量文档](https://developers.openai.com/api/docs/guides/embeddings)。

切换后点击「补建现有资料向量」，系统为当前有效的记忆和已有分段补建新模型缺失的向量。任务按每日预算处理，不重复排队已经排队或处理中的同模型任务；已完成向量不重复收费重建。旧向量保留以支持回滚，查询仅比较同一提供者、模型和维度；无需更改数据库列维度。未配置密钥或尚未完成补建时，原文检索仍可使用。

本地 FastEmbed / `BAAI/bge-small-zh-v1.5` 服务仍可选：用 `docker compose --profile local-embeddings up -d embeddings` 启动，再将私有模型配置的向量提供者改为该服务。默认部署不再下载本地模型权重。

通用连接器配置和格式见 [资料接入](connectors.md)。

## 附件与备份

附件上限 20 MiB。PDF 最多 100 页，优先逐页提取文字，扫描页使用 OCR；图片使用中文/英文 OCR；音频需要已配置的转录服务。原件和解析文本分开保存，通过版本化来源关联。超出限制、缺少模型、解析失败都会保留原件并显示缺口。

`memory-db` 保存数据库，`memory-files` 保存原件和私有 Codex 登录。资料库的 JSON 导出包含规范记录、版本、证据、授权和删除阻断标记，但不包含二进制附件或模型凭据。完整灾难恢复须同时备份数据库、文件卷、私有模型配置、`model-api.json` 和 `.env`，并将备份置于单独的受保护位置。训练导出只包括用户选中的非过期样本，记录对应清单。

登录密码轮换：生成新的 `PCAS_API_TOKEN` 后重建 API 容器，旧登录会话失效。不要执行 `docker compose down -v`，除非明确要销毁数据库与附件。
