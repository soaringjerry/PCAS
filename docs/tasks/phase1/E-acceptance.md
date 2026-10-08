# 任务 E：黄金路径验收与收尾

> **Historical task record.** This text records an earlier phase or investigation.
> Use the [current documentation](../../README.md) and [project status](../../status.md) for new work.
> Its old assignments, limits, and precedence statements do not govern current development.

执行者：**Astra** · 分支：`phase1/E-acceptance` · 依赖：A、B1、B2、C1、D1、D2 全部合并
开工前读：[README.md](README.md)、[contracts.md](contracts.md)、[白皮书](../../whitepaper.md) 第 17 章。

Astra 额度紧，**不要从头审查或重写**。只做三件事：把第 1 阶段的黄金路径在真实浏览器里端到端跑通；修掉跑不通的地方；写一份验收报告。

## 1 搭环境

- 按 README 的「环境」一节，用 `main` 最新代码起一套临时实例：独立的测试库、`pcas serve`、`pcas worker`、前端 build。
- **模型用 httptest 假模型**，按场景返回固定的 JSON，确保用例结果稳定。假模型写成一个小的 Go 程序或测试辅助，放在 `web/tests/support/` 或 `internal/testsupport/`，和用例一起提交。
- 真实模型只用来做一次手动走查（第 3 节），**做之前先问用户**：这会用到用户的模型额度和账号。

## 2 自动化用例（`web/tests/golden.spec.ts`，接真实后端，不 mock API）

对应白皮书第 17 章中属于第 1 阶段的几条：

- [ ] **一句话安排**：在首页说「周五下午三点和张三对方案，算在 A 项目里」。回执显示时间、项目和提醒；「今天」或「这几天」里出现这件事；刷新后还在；点撤销后消失。
- [ ] **边问边记**：先问一个问题，在回复还没回来时接着说「顺便记下明天交电费」。两轮都得到处理，输入框始终可用，电费这件事出现在「这几天」里。
- [ ] **改安排**：接着说「改到下周一上午十点」。改的是同一件事，提醒时间跟着变。
- [ ] **事项页秘书**：在一个任务页说「拆成三步」。副手完成后，结果被自动加成 3 个子任务；撤销后子任务消失；点「放回去」后子任务又出现。
- [ ] **失败恢复**：让假模型对第一次副手请求返回错误。卡片显示人话原因和【重试】，点重试后成功。
- [ ] **提醒送达**：构造一件 1 分钟后到点的事。首页「到点了」出现这件事；Web Push 用 Chromium 的通知权限加订阅，断言 Service Worker 收到了 push（做不到就改为断言后端已向推送端点发出请求，并在报告里说明原因）；Telegram 用 httptest 模拟 Bot API，断言收到了 `sendMessage`。
- [ ] **Telegram 对话**：用 httptest 模拟 Bot API，投递一条文字消息「后天上午九点去银行」。首页能看到这件事；Bot API 收到回执和撤销按钮；投递撤销回调后，这件事消失。
- [ ] **完成即撤销**：首页打勾完成 → toast →【撤销】→ 事项回到原位。
- [ ] **不丢话**：让假模型超时。回执为「已记下原话…」，资料库里能找到这句原话。

### 旧的真实后端用例

`web/tests/backend.spec.ts` 从 B2 合并后就跑不通了：它引用的「导办台」「交给谁」「逐条确认」都已经删除。`continuity.spec.ts`、`chatgpt-direct.spec.ts`、`model-api.spec.ts` 也要逐个检查。
- 仍然有意义的场景（登录、记忆确认与纠正、来源查看、导出、模型设置等）改写后并入 `golden.spec.ts` 或原文件；
- 针对已删除界面的用例直接删掉；
- 最后让所有依赖真实后端的用例都能在临时实例上跑通，并在报告里列出每个文件的处理方式。

## 3 手动走查（需要用户同意后才做）

用真实模型把第 2 节的前三条各走一遍，截图附进报告。重点观察：时间换算对不对、项目匹配对不对、回复有没有变成长文、回执读起来是否自然。

## 4 修复范围

- 修改范围**只限于让上面的用例通过**所需的最小改动。每个修复单独提交，提交信息里写明是哪个用例暴露的问题。
- 超出这个范围的问题（设计缺陷、需要改契约的、需要大改的）不要自己修，写进报告的「遗留问题」，注明归属哪个任务、建议怎么处理。

## 5 报告 `docs/evaluations/<日期>-phase1-acceptance.md`

报告按以下格式写：

- 环境：commit、数据库版本、浏览器版本。
- 每条用例：通过或不通过、耗时、截图路径。
- 修复了哪些问题，每条附 commit。
- 遗留问题清单，按严重程度排序。
- 真实模型走查的观察（如果做了）。

## 验收

- [ ] `golden.spec.ts` 全部通过，并且连续跑 3 次都稳定通过。
- [ ] 报告已提交。
- [ ] `make check`、`make test-integration`、前端检查全部通过。
- [ ] 测试容器和临时进程已清理。
