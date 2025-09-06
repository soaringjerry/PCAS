---
title: "统一 LLM 协议适配：兼容 OpenAI / Claude（Anthropic）并保持内核中立"
description: "以 Canonical Schema + 协议适配层的方式，让 PCAS 同时兼容主流 LLM 提供方的数据格式与流式协议。"
tags: ["idea", "llm", "protocol", "adapter", "streaming"]
version: "draft"
---

# 统一 LLM 协议适配（Idea）

## 背景 / 问题

- D-App 生态里常见两类对接格式：
  - OpenAI Chat（messages 数组、流式增量 delta）
  - Anthropic Messages（Claude 3+，content 支持多模态与 tool_use）
- 直接把这些厂商格式“穿透到内核”，会让策略与总线强绑定供应商语义，后期迁移/扩展成本高。
- 现状 InteractStream 已通，但仅支持“聚合纯文本 + system”最小协议；dApp 若上送 JSON（openai/anthropic）或业务参数（target_lang/mode），需要服务端解析与映射。

## 目标

- 定义一套 **Canonical Schema（内核统一契约）**，统一消息/参数/错误与流式增量结构。
- 在流式入口（InteractStream）引入 **协议适配层**，兼容 openai-chat 与 anthropic-messages 等格式。
- Provider 层实现 **适配器**，把 Canonical Schema 映射到对应 SDK 并回放统一增量。

非目标（本阶段）

- 不一次性实现所有高级能力（function/tool call、观察者流、多模态内容等）；先覆盖消息/文本与基础生成流。

## 高层设计

```
Client (various protocols)
   │  StreamConfig + StreamData (text/json)
   ▼
PCAS Bus: InteractStream Handler
   ├─ Protocol Normalizer (根据 attributes.protocol / content_type 解析为 Canonical)
   │     - openai-chat JSON → Canonical
   │     - anthropic-messages JSON → Canonical（阶段2）
   │     - text/plain → Canonical（已有）
   └─ Policy → Provider 选择
         ▼
Provider Adapter（OpenAI / Claude ...）
   - Canonical → SDK 请求
   - SDK 增量 → Canonical 增量 → InteractResponse_Data
```

## Canonical Schema（草案）

- 输入（Streaming）
  - attributes（StreamConfig）：
    - protocol: `openai-chat` | `anthropic-messages` | `pcas-text`（默认）
    - content_type: `application/json` | `text/plain`
    - model / system / temperature / top_p / max_tokens（可选）
    - 任务类参数：`target_lang` / `mode`（先透传到 system 或 provider adapter）
  - payload（StreamData）：
    - text 模式：原始字节拼接为 `text`
    - openai-chat：`{"messages":[{"role":"user","content":"hi"}], ...}`
    - anthropic-messages（阶段2）：`{"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`

- 输出（Streaming 增量）
  - InteractResponse_Data：`delta` 文本块（后续扩展 role/tool_call/finish_reason）
  - InteractResponse_Error：
    - InvalidArgument（协议/载荷不合法）
    - FailedPrecondition（Provider 不支持 streaming/未就绪）
    - Internal / Unavailable（下游 SDK/网络错误）

## 协议适配（阶段 1）

- openai-chat 解析器：
  - 入口：attributes.protocol=openai-chat 且 content_type=application/json
  - 解析 StreamData JSON（允许多帧，聚合后处理）
  - Canonical 消息：`messages[{role, content}]`；system 从 attributes/system 注入；数值参数从 attributes 覆盖
  - 错误输入：返回 InteractResponse_Error(InvalidArgument)，不要“只 Ready 不 Data”

- pcas-text（默认）：
  - 维持现状：拼接文本 + attributes.system

> 交付顺序：先实现 openai-chat 解析 + OpenAI Provider 支持 messages 流式；Anthropic 放到阶段 2。

## Provider 适配

- OpenAI Adapter（已具备基础流式）
  - 新增：支持 Canonical.messages → ChatCompletion.Stream
  - 支持 attributes 覆盖：model / system / temperature / top_p / max_tokens
  - 增量把 `choices[0].delta.content` 回放为 Data 块

- Anthropic Adapter（阶段 2）
  - 支持 Messages API 流式，解析 tool_use / tool_result（后续）

## 错误语义（统一规范）

- 协议/JSON 解析失败 → InvalidArgument（message 说明缺少/类型错误）
- 选择到不支持 streaming 的 provider → FailedPrecondition
- 下游 SDK 报错/429/超时 → Internal/Unavailable（附带 SDK 错误信息）

## 检测与健康检查

- 启动日志：Initialized provider: <name>（确认 provider 就绪）
- 新增管理事件/CLI：`pcasctl providers list`（phase later）
  - 列出可用 provider、模型、是否支持 streaming

## 兼容性与迁移

- 现有 text 流程不变；要求 Client 在最后发送 ClientEnd（输入结束）
- dApp 可逐步迁移到 openai-chat JSON，仍可在 attributes 里放通用参数

## 流程样例

1）OpenAI Chat（JSON）

StreamConfig.attributes：
```json
{ "protocol": "openai-chat", "content_type": "application/json", "model": "gpt-5", "temperature": "0.7" }
```

StreamData：
```json
{ "messages": [ { "role": "user", "content": "hi there!" } ] }
```

终止：ClientEnd

2）Translate（先用 system 表达）

attributes：
```json
{ "protocol": "pcas-text", "system": "Translate all user input to English.", "model": "gpt-5-mini" }
```

data：`你好，世界`（text/plain） → ClientEnd

## 里程碑

- M1（本周）
  - InteractStream Normalizer：openai-chat 解析（JSON→Canonical）
  - OpenAI Provider：messages 流式 → 增量回放
  - 错误返回：InvalidArgument 覆盖
  - 文档：接口契约 & 样例

- M2（+1~2 周）
  - Anthropic messages 解析与流式回放（最小）
  - attributes → provider 参数映射（top_p/max_tokens 等）

- M3（+4 周）
  - 工具调用（function/tool_call）与 finish_reason 规范
  - providers list/health API & pcasctl 子命令

## 风险与缓解

- 厂商协议频繁变动 → 适配层隔离；通过单元测试样例锁定关键字段
- JSON 边界与多帧拼接 → 限制最大 payload，提供错误返回/截断策略
- 性能：Normalizer 是轻量解析，Provider 流式主导延迟；需监控回压与断流处理

## 验收

- dApp 以 openai-chat JSON/文本两种方式，均可在 InteractStream 下得到稳定增量输出
- 错误输入返回 InteractResponse_Error（不是无响应）
- 文档与示例完整，可由第三方 dApp 复现

