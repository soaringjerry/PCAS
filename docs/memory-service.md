# PCAS 记忆基础服务

实现依据：[记忆架构定稿 1.0](memory-architecture.md)。本目录文档说明当前骨架的运行方式、模块边界和实现状态，不改变定稿规定的完整目标。

## 作为项目基底

自然对话、资料导入、行动模块、Prompt 交接和训练导出使用同一套记忆身份、证据和版本。它们通过 `internal/memory` 的应用接口或 HTTP 适配器访问记忆；不各自维护事实副本，也不直接读写记忆表。

```mermaid
flowchart TB
    Dialogue[自然对话与来源导入] --> API[统一记忆服务]
    Actions[任务、日程与提醒] --> API
    Handoffs[AI 交接] --> API
    Training[训练导出] --> API
    API --> Context[上下文：原文与版本]
    API --> Structured[结构化：实体、经历、陈述与证据]
    API --> Semantic[语义：可重建的向量索引]
    API --> Jobs[事务事件队列]
    Jobs --> Worker[后台 worker]
    Worker --> Context
    Worker --> Structured
    Worker --> Semantic
```

任务和提醒的执行状态由行动模块维护。记忆记录意向、约束、事实和执行证据；接口预留不代表任务引擎或提醒引擎已实现。当前浏览器原型仍使用 localStorage，前端模型不作为后端数据库定义。

## 代码布局

| 目录 | 职责 |
|---|---|
| `cmd/pcas` | 组合配置和适配器；`serve`、`worker`、`migrate` 三种进程入口 |
| `internal/memory` | 不依赖数据库或 HTTP 的领域类型、服务契约、输入校验、原文分块 |
| `internal/postgres` | 数据库迁移、原文仓储、版本约束、访问过滤、事务队列与分块持久化 |
| `internal/worker` | 阶段处理器注册、领取、超时、重试和阻塞状态 |
| `internal/httpapi` | 身份认证、HTTP/JSON 适配和错误映射 |
| `internal/config` | 环境配置校验；缺失凭证时拒绝启动 API |

`memory.API` 定义统一应用边界：`Sources`、`Retriever`、`Editor`、`Activity`。`Extractor`、`Embedder`、`BlobStore` 是可替换的外部能力接口。当前组合根只装配已实现的 `Sources`；检索适配器为空时返回明确的 501，不返回虚构的摘要或空的成功结果。

## 当前可运行的流程

1. 认证绑定 owner 和 principal。调用者不能通过 JSON 或 `X-User-ID` 改变所属用户。
2. `POST /v1/memory/sources` 保存原文。相同 owner、来源和外部 ID 共用稳定身份；外部版本决定幂等键。同版本内容变化返回 409，需要新的外部版本。
3. 原文、版本元数据和 `source.chunk` 任务在同一事务提交。事务失败时不会留下只写入一半的数据。
4. 写入返回后可立即读取原文，不等待后台处理。系统版本按导入顺序递增；它表示来源的保存版本，不代表陈述的现实有效时间。表达时间单独保留。
5. worker 按 Unicode 字符位置分块，在一个事务中保存所有块、提交本阶段完成状态，并派生 `source.extract`、`source.embed`、`source.tokenize` 阶段。
6. 当前未配置这三个处理器，因此它们进入 `blocked`，错误码为 `handler_not_configured`。原文始终可读，处理状态保留缺口。

队列使用带租约的领取和 `SKIP LOCKED`，支持多个 worker。进程中断后，超时任务可重新领取；每次领取产生新的租约令牌，旧 worker 无权提交或确认。失败最多尝试 5 次，采用指数退避；中断耗尽次数同样进入失败状态。输出与完成状态原子提交，避免重试产生重复块。

当前 `blocked` 表示缺少处理能力，不会自动反复重试。添加处理器后，需要配套实现按阶段恢复作业的管理入口；本骨架没有开放重试管理 API。

## 本地运行

需要 Docker 与 Docker Compose；本机直接运行 Go 命令时需要 Go 1.26，模块指定工具链 1.26.8。

```bash
cp .env.example .env
openssl rand -hex 32
# 将生成的值填入 .env 的 PCAS_API_TOKEN；示例占位值无法启动 API。
docker compose up --build -d
docker compose logs api worker migrate
```

Compose 启动 PostgreSQL 16 + pgvector 0.8.2，等待数据库就绪，再执行迁移；迁移成功后启动 API 和 worker。API 默认绑定宿主机 `127.0.0.1:8090`，数据库绑定 `127.0.0.1:54329`。密码示例仅用于本地开发。

数据库数据保存在 Compose 的 `memory-db` 卷。`docker compose down` 停止服务并保留数据。

也可以只用 Compose 启动数据库，在本机运行服务：

```bash
docker compose up -d db
set -a
. ./.env
set +a
go run ./cmd/pcas migrate
go run ./cmd/pcas serve
# 在另一个已加载同一 .env 的终端运行：
go run ./cmd/pcas worker
```

API 和 worker 启动前检查迁移完整性；迁移命令有独立锁，并记录 SQL 校验和。不要修改已经应用的迁移，新增后续迁移。服务收到终止信号后先结束请求或处理事务，再关闭数据库连接。

## HTTP 接口

除健康检查外，所有接口要求 `Authorization: Bearer <PCAS_API_TOKEN>`。当前凭证属于配置的 owner。多 AI 独立凭证的签发尚未实现；仓储已实现非 owner principal 的显式读取授权和撤销过滤，后续认证适配器必须绑定真实身份，不能把 owner 凭证发给外部 AI。

| 方法与路径 | 当前行为 |
|---|---|
| `GET /healthz` | 进程存活检查 |
| `GET /readyz` | 数据库连接就绪检查；启动时另校验迁移 |
| `GET /v1/memory/capabilities` | 返回已连接、待配置和未实现的能力 |
| `POST /v1/memory/sources` | 写入 UTF-8 纯文本或 Markdown；新版本返回 201，幂等重试返回 200 |
| `GET /v1/memory/sources/{id}` | 读取当前保存版本及各处理阶段状态 |
| `GET /v1/memory/sources/{id}?version=1` | 读取指定保存版本及其状态 |
| `POST /v1/memory/recall` | 契约已定义，检索管线未连接，返回 501 |
| `POST /v1/memory/expand` | 契约已定义，证据/历史展开未连接，返回 501 |

写入示例（先在当前终端加载 `.env`）：

```bash
curl -sS http://127.0.0.1:8090/v1/memory/sources \
  -H "Authorization: Bearer $PCAS_API_TOKEN" \
  -H 'Content-Type: application/json' \
  --data '{
    "connector": "manual",
    "external_id": "conversation-1/message-1",
    "external_version": "1",
    "title": "周末想到的事",
    "text": "下次路过河边，想去之前提过的那家旧书店。",
    "media_type": "text/plain",
    "expressed_at": "2026-09-29T09:00:00Z"
  }'
```

返回的 `id` 和 `version` 是记忆服务身份；读取时可查看 `processing` 数组。单条原文上限 1 MiB，HTTP JSON 请求上限 2 MiB。来源 ID、版本、标题有独立长度限制。未知 JSON 字段被拒绝，防止把尚不支持的元数据悄悄丢弃。

错误码包括 `invalid_input`（400）、`unauthorized`（401）、`forbidden`（403）、`not_found`（404）、`version_conflict`（409）、`reimport_blocked`（410）和 `capability_not_configured`（501）。未授权读取与不存在的资料均返回 404。处理错误不会向客户端或常规日志输出原文、数据库连接串或凭证。

## 存储约定与完整架构的对应关系

| 定稿中的职责 | 骨架落点 | 当前状态 |
|---|---|---|
| 统一身份、证据版本和三种时间 | `memory_records`、`record_versions`、`Ref`、`Revision` | 表及约束已建立；原文链路使用 |
| 上下文与相邻材料 | `sources`、`source_versions`、`chunks`、`episode_members` | 文本保存与分块可用；附件、OCR、转录、对话关联待实现 |
| 实体、别名与经历 | `entities`、`entity_versions`、`aliases`、`episodes` | 表和领域类型已建立；抽取、匹配与归并待实现 |
| 陈述、变化、纠错和证据 | `claims`、`claim_revisions`、`evidence`、`relations` | 表、类型和 `Editor` 契约已建立；事务操作与证据完整性规则待实现 |
| 三种检索与上下文组装 | `Retriever`、预算、覆盖缺口、继续查询游标 | 契约已建立；召回、重排、时间视图、展开和上下文组装待实现 |
| 可重建的向量与全文索引 | `embeddings`、`chunks.search_vector` | pgvector 与索引字段已建立；分词、模型、维度与检索策略待连接 |
| 摘要与缓存依赖 | `derived_views`、`derived_dependencies` | 表及反向依赖索引已建立；重建和读取校验待实现 |
| 用户实际使用与衰减 | `activity`、`use_events`、`Activity` | 表和契约已建立；打分与有限强化待实现 |
| 纠正与删除约束重导入 | 修订表、`reimport_blocks`、`Editor` | 来源阻断检查已接入；纠正传播、范围删除、清理与阻断写入待实现 |
| 主动唤醒与行动协作 | 记忆引用、关系和后台队列边界 | 唤醒策略、行动模块联动与通知去重待实现 |
| 多 AI 授权 | `Scope`、`record_grants`、`Authenticator` | 原文读取授权可用；完整检索授权与独立凭证待实现 |
| Prompt 与训练数据 | 版本化 `Ref` 和证据类型 | 下游模块待接入统一服务 |

`memory_records` 和 `record_versions` 只保存共同身份、版本和时间元数据；事实内容仍归属各类型的规范表。复合外键包含 owner，禁止跨用户建立引用。记录当前版本使用延迟外键，确保不能提交没有正文版本的身份。

陈述性质、获得方式、确认状态分别建列；这里没有任务完成状态。陈述允许按不同范围和有效时间并存，不能把“最后导入”当作“现实中最新”。JSONB 扩展字段当前仅建立对象类型约束；未来各写入适配器还需按业务类型验证字段。

向量维度和模型由 embedding 适配器决定。当前使用无固定维度的 `vector` 字段及维度校验；接入模型后按模型/维度建立对应 ANN 索引，检索必须同时约束 owner、授权和有效版本。全文分词尚未连接，因此不会把未经中文分词的正文标成已完成全文索引。

源资料的删除涉及证据、关系、派生依赖和附件清理，不能靠直接删除某一张表替代。当前删除 API 尚未开放，相关跨表外键会要求未来删除服务显式处理依赖。`reimport_blocks` 仅保存来源身份摘要，不保存被删正文。

## 验证

```bash
make check
# 运行真实数据库集成测试，需要独立的测试数据库：
export PCAS_TEST_DATABASE_URL='postgres://pcas:pcas-test@127.0.0.1:5432/pcas_test?sslmode=disable'
make test-integration
```

`make check` 包含格式、`go vet`、race 测试和编译。未配置测试数据库时，数据库集成测试会明确跳过；CI 提供独立 pgvector 数据库并运行这些测试。

集成测试在测试数据库中创建独立 schema，验证迁移重复执行与校验和、并发幂等写入、刚写即读、历史版本、授权撤销、跨 owner 引用拒绝、来源重导入阻断、worker 租约恢复与旧实例隔离、Unicode 原文定位及重试上限。

这些是基础设施检查。架构定稿中的五组语义回放仍需随抽取、检索、纠正和衰减实现逐项落地，不能用基础设施检查代替完整记忆系统验收。

## 依赖实现参考

- [PostgreSQL SELECT 与行锁](https://www.postgresql.org/docs/16/sql-select.html)：队列领取使用 `FOR UPDATE SKIP LOCKED`。
- [pgvector](https://github.com/pgvector/pgvector)：向量字段、维度及按模型建索引的约束。
- [pgx](https://github.com/jackc/pgx)：Go PostgreSQL 连接池与事务适配。
