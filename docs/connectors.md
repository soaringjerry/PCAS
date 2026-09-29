# 通用资料接入

在「设置 → 资料接入」创建 Webhook、HTTP 定时读取或服务器文件夹接入，也可以直接上传聊天归档。每条资料保留来源 ID、版本、原文、表达时间、角色、父消息和分支；相同来源版本重复发送不会重复创建记忆。正文立即可搜索，抽取和索引异步执行。

## 应用推送

创建 Webhook 后，界面显示一次独立 Bearer 密钥。它只能写入对应接入，不能读取记忆或管理工作台。暂停接入立即停止接受写入。调用：

```http
POST /v1/connectors/{id}/records
Authorization: Bearer CONNECTOR_TOKEN
Content-Type: application/json
```

```json
{
  "records": [{
    "id": "conversation-42/message-7",
    "version": "3",
    "title": "PCAS 方案讨论",
    "text": "我决定采用统一记忆服务，图数据库先作为备选。",
    "media_type": "text/plain",
    "expressed_at": "2026-09-29T09:00:00Z",
    "conversation_id": "conversation-42",
    "parent_id": "message-6",
    "role": "user",
    "branch": "active",
    "episode_key": "PCAS",
    "episode_title": "PCAS 架构讨论",
    "missing_attachments": []
  }],
  "gaps": []
}
```

`id` 与非空 `text` 必填；省略 `version` 时根据完整记录内容生成稳定哈希。显式版本号对应的正文不得更改。每次最多 100 条，HTTP 请求体最多 2 MiB，单条正文最多 1 MiB。`episode_key` 是用户范围内明确共享的经历键，可跨接入关联；省略时按接入内的会话组织，避免同名会话被自动合并。

返回 `refs`、`imported`、`duplicates`、`blocked` 和 `gaps`。删除过且禁止重导入的记录会计入 `blocked`，其他记录可以继续导入。

## 定时读取

数据源响应沿用上述 JSON，额外支持：

```json
{"records": [], "next_cursor": "page-2", "has_more": true, "gaps": []}
```

PCAS 使用 `GET`，后续请求带 `?cursor=...`。`has_more=true` 必须提供与当前不同的非空游标。最多每页 100 条、响应 8 MiB；游标只在资料事务提交成功后推进。最后一页的 `ETag` 用于后续 `If-None-Match`，支持 `304`。数据源应提供增量游标语义，不能在同一游标上永久重复旧页面。失败保留游标和明确状态，按配置间隔重试。

需要凭据时，填写以 `PCAS_CONNECTOR_` 开头的环境变量名，在 API 服务环境中注入其值。凭据不会发给浏览器或记录到日志。HTTP 重定向不自动跟随，以免凭据被转发。间隔为 15 秒至 24 小时，调度器每 5 秒检查到期接入；租约避免重复调度，设置修改带版本检查。

## 文件夹

目录为 `PCAS_INBOX_DIR/{接入 ID}`。Compose 内位于 `/var/lib/pcas/inbox/{接入 ID}`，属于持久化文件卷。把受支持文件放到此目录即可同步：TXT、Markdown、JSON、JSONL、ZIP。仅扫描该目录的普通文件，不递归子目录、不跟随符号链接，不接受任意服务器路径。

每个文件最多 8 MiB，目录最多 1,000 个条目；按内容指纹处理新增和修改。TXT/Markdown 原文直接入库；聊天归档先保存原件，再异步整理。不会因上游文件消失而自动删除已有记忆。连接器只读取文件，不删除或移动原文件。

## 聊天归档

支持 ChatGPT `conversations.json`/ZIP 的消息树、Claude `chat_messages` 格式，以及通用 `records` JSON、JSONL、TXT/Markdown。上传最多 20 MiB；ZIP 解压文本总量最多 64 MiB、最多 10,000 条记录，超出时明确拒绝。ZIP 在内存中读取，不向文件系统解压路径。

ChatGPT 的活跃分支和历史分支分别保存；AI、用户与工具角色分别保存。非文本消息、原图和未解析附件显式标为缺口。原始归档可在来源页面鉴权下载，文本记录可逐条展开。归档中的独立图片和音频不会假装已经完成 OCR/转录，需要通过附件导入通道接入。

删除一条来源时，会清理含有这条内容的已保存归档原件，其他消息的独立记录继续保留；原件入口标明缺失。重导入新归档时仍检查被删来源的阻断标记，含有被阻断内容的归档原件也会被清理。重新上传同一归档不会撤销用户纠正。

## 摘要与曝光

`POST /v1/memory/summary` 接受 `{ "id": "UUID", "version": 1, "tokens": 2000 }`，返回带版本引用的摘录摘要、依赖、覆盖缺口及缓存状态。当前来源、相关陈述及经历成员共同参与，原话与当前理解分别标注。缓存绑定 principal、根版本、预算和成员指纹；纠正、删除、授权变化或新增材料后重建。摘要不是独立事实库。

`POST /v1/memory/activity` 接受 `ref`、`half_life_days`、`reinforcement_limit`、`pinned`；对应设置页的「记忆曝光」。基础减半时间为 1 至 36,500 天，强化上限为 1 至 100。用户采用只在有效使用后有限强化，查询和展示不强化；暂缓提醒、事实状态和事项状态保持独立。
