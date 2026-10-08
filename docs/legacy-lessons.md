# 旧版经验记录

> **Historical implementation lessons.** This text records an earlier phase or investigation.
> Use the [current documentation](README.md) and [project status](status.md) for new work.
> Its old assignments, limits, and precedence statements do not govern current development.

旧版 PCAS（事件总线 + 策略路由 + SQLite/HNSW 记忆）已整体归档在 `legacy` 分支，
v0.0.1 到 v0.1.2 的版本标签保持不变。新版不复用旧代码，只保留下面这些经验。

## 模型接入

- 新一代 OpenAI 模型会拒绝 `max_tokens` 和非默认的 `temperature`，请求返回 400。
  按模型能力决定发送哪些参数，不要写死默认值。
- OpenRouter 通过 `OPENAI_BASE_URL` 接入，另需 `HTTP-Referer` 和 `X-Title` 请求头；
  `sk-or-` 开头的 key 可以用来自动识别。
- Provider 不能按名字硬编码判断。旧版的 RAG 只对名为 `openai-gpt4` 的 provider 生效，
  默认配置改用 gpt-5 后 RAG 就静默失效了。

## 存储与检索

- 先全局取 `topK*10` 个近邻、再按用户过滤的做法，在数据量上来后召回率会塌。
  过滤条件要在检索阶段生效，而不是事后筛选。
- 能进入查询条件的字段必须完整持久化。旧版从没存过 `attributes`，按属性过滤的搜索
  因此永远返回空。
- 不要把调用方可控的字符串拼进 SQL（旧版把属性 key 直接拼进了 JSON 路径）。
- 进程退出时，要先等后台写入任务结束，再关闭存储、保存索引。

## 工程

- 从第一天起就在 CI 里强制 gofmt 和 lint。旧版几乎所有文件都没格式化，lint 也被关掉了。
- 不要用 `+build ignore` 屏蔽跟不上重构的测试，它会让覆盖率看起来比实际高。
- 自动发布要和安装脚本的更新渠道一起设计。旧版每次推送到 main 都会发布 edge 镜像，
  而安装脚本默认拉的就是 edge。
- 编译产物和索引文件不要提交进仓库。
