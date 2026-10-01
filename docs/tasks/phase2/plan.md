# 第二阶段隔离实施计划

2026-10-01；基线 d8d6fb3；唯一开发 checkout `/tmp/pcas-phase2-a` 等 dispatch worktree。研究迁为正式方案见 [K0 契约](contracts.md)，批准以协调记录为准。

## 批次与出口

| 批次 | 交付 | 验收与未完成项 |
|---|---|---|
| K0 | 上位小修、共享可信上下文/授权/typed证据/四记录/attempt类型、兼容迁移 | root/B/C审阅；A只编译，产品/迁移测试C执行；不等于2.0完成 |
| 2.0 / K1+K2 | 自然/公开来源授权、exact source/claim/summary验证、三消费者实际原话、manual重取、在途/下一轮闭包、有界attempt | 固定gold及真实入口fake provider捕获；独立自然授权与SQL装配诊断分开；无claim、旧版本、跨范围、pending、barrier撤权/删除/纠正、回滚/崩溃、容量/到期逐题留证；失败不skip |
| 2.1 | 可靠抽取、共用planner、独立raw补漏、成都时间轴；然后ChatGPT历史迁入可用性/规模/记忆贯通 | 角色/分支/表达时间/附件缺口、幂等及更正删除重导；active旧计划在AutoAccept/WakeIdeas下不创建今日行动/提醒/唤醒；真实模型和实际黄金路径单独验收 |
| 2.2 | 连续任务、同模型同预算hybrid对照、actions后续治理和费用收敛 | 安全条件全过、成本三账加诊断存储；收益/延迟/费用先校准再预登记，不由旧7/7或mock绿色推导 |

## 所有权与依赖

A 独占本契约/计划、whitepaper/memory-architecture、memory共享类型和迁移。B 独占 source policy/来源编辑/API。C 独占独立gold/testdata、phase2测试和评测证据。K0审核冻结后，A/B消费者开工前以同一提交作为基线；任何新增共享接口先由A发布并通知C。

K1 A保留 retrieval/expand/desk_turn/desk/run_context/runs/artifacts、typed/manifest模块、ai最终捕获。协调已扩A到workspace.Command/model、commands.go、desk_actions.go/schema、actions_log.go（限policy undo）、相关prompt；A新manual handler，B只注册route。自然授权首批deterministic整句，不依赖模型op；policy undo仍为2.0必须项。不在K0先接消费者。B调用A统一typed失效hook；A调用B source policy事务helper。事务顺序与容量门槛按契约，不拆成彼此等待的两套verifier。

## 序列和证明边界

1. ingest纯文本→pending抽取→明确授权→秘书/API/manual实际输入含exact原话，不借claim完成遮掩。
2. 仅claim grant→查来源；来源授权→换role/model/provider/channel/studio：不扩权限，未知/不支持kind有缺口。
3. source v1→v2→当前问/合法历史问：当前v2，历史exact v1明示已更改。
4. prepare→dispatch reservation→fake provider barrier→更正/删除/撤权提交→返回/采纳：mutation可在barrier期间完成，失效结果不可采纳；不把reservation称外部接收。
5. 已有回答/摘要/Brief→失效→同/新会话续做；manual准备→失效→State/重取/复制/提交结果：无缓存绕过。
6. claim-only删除保持独立授权source；source删除清原件/派生/attempt/archive并阻断同身份重导，迟到worker无复活。
7. 重复/回滚/崩溃/失败→重试：attempt独立留骨架，无无界正文或隐含重收费。
8. 最终截断/容量/显式到期→查看/删除：映射与实际载荷一致，omitted与未dispatch分立，durable deps不随diagnostics过期。

C先冻结人工预期再读取产品；从真实入口捕获最终provider/适配器输入，Recall/Expand只诊断。失败记录精确SHA与日志，不重跑碰运气、不减少断言。7aae834旧M1动态、d8静态、本批动态分别记账。线上11项、真机、一天试用没有通过证据，保留原gate；隔离开发授权不等于gate通过。合并、部署等待明确后续授权。

## 工程初值与风险

7天正文/64MiB每owner/256KiB单次、30天骨架/10000条及逻辑metadata每owner64MiB/单次64KiB是本批工程选择；metadata含manifest、recipient、attempt依赖等所有可变诊断内容，PG物理开销另计。候选15/边15/一跳/4000与8000 token待测。容量失败拒绝外发；不得牺牲完整记录或权限换绿色。检索扫描、token实际值、订阅成本、WAL/备份体积、大批导入能力均未测。真实provider隐藏上下文、manual外部接收记unknown。

K0验证：A仅执行`GOCACHE=/tmp/pcas-phase2-a-gocache go build -buildvcs=false ./...`及`git diff --check`，均通过。普通build因隔离worktree的VCS stamping失败，关闭stamp后完成纯编译。A未写/运行产品或迁移测试；C独立执行，不把编译当2.0/迁移/线上验收。

最需验证的实现风险：Desk现有事务跨provider持锁、独立attempt FK死锁、source政策与实际provider路由不一致、旧summary含actions JSON、存活产物随attempt到期丢依赖、State/导出漏sanitize。K0只冻结解决边界；动态检查由C证明实现是否闭合。
