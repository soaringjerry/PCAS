# 任务 U4：导入 ChatGPT 历史的界面（前端）

执行者 Opus 5.5。分支 `phase2/b4-U4-frontend`，从 `origin/phase2/batch4` 建；工作区 `/root/PCAS-wt/b4-U4`；Draft PR 的 base 是 `phase2/batch4`。

先读 [并行方案与数据约定](../parallel.md) 第 5 节、[第 4 批契约](README.md) 的 R1、R3–R5、R10、R20 和序列 W1–W4，以及 [界面与交互原则](../../../design/principles.md)。

## 现在的界面

`ImportSheet.tsx`：选一个文件或粘贴文字，点导入，面板关掉。文本上限 1 MB、附件 20 MB。导入之后没有任何进度。连接相关的设置在 `ConnectorSettings.tsx`。

## 要交付什么

把「搬 ChatGPT 的聊天记录」做成一条看得见的流程：

1. **选文件。** 用户从 ChatGPT 导出的是一个 zip。认出是聊天记录导出时走这条流程，别的文件照旧。
2. **预览。** 调 `POST /v1/connectors/archive/preview`，告诉用户：多少段对话、多少条消息、从哪天到哪天、有多少已经导过、有多少因为太多放不下。这一步什么都没存，用户可以取消。大文件上传要有进度。
3. **确认导入。** 调现有的导入接口，拿到批次。
4. **进度。** 从 `GET /v1/connectors/imports` 读：已经存好多少条（存好就能查到）、已经整理成记忆多少条。两个数字是两件事，要让人看懂「现在已经能问到里面的话了，整理还在慢慢做」。可以暂停和继续。面板关掉再打开，进度还在。
5. **出错。** 接口返回 `{ "error", "message" }`，`message` 就是给用户看的那句话，直接显示；失败的批次可以继续。
6. **删除一次导入。** 用现有的 `POST /v1/memory/delete`，目标是批次里的 `archiveId`、`archiveVersion`（契约第 6 节）。删之前说清楚会删掉什么：这次导入的全部消息，以及从它们整理出的记忆。

进度放在哪（导入面板里、资料库里、还是设置里）由你定，要求是用户导入之后回来能找到它。能用图形表达的不用长文字。

后端接口没合进来之前，用模拟数据自己看效果；字段名按契约，不要自己起名。

## 你独占的文件

`web/src/components/ImportSheet.tsx`、`web/src/components/ConnectorSettings.tsx`、你新增的导入组件；`web/src/styles/app.css` 只在文件末尾追加自己的一段，用注释标出起止。需要在数据层加导入相关的调用时，加在你自己新建的文件里，不要改 `web/src/store/` 里已有的文件（那是第 2 批 U2 的）。

不要动：后端代码、`web/tests/`、资料库页面和秘书相关的组件。

## 约束

界面上不出现「批次」「归档」「抽取」「来源」这类内部说法。手机宽度（390px）下不溢出。已有的浏览器测试照常通过；预期必须变的先列给协调者。

## 交付

Draft PR；`npm run lint && npm run type-check && npm run build` 通过；桌面 1440 和手机 390 各三张截图（模拟数据）：预览、进行中的进度、一种出错。说明里写清进度放在哪、为什么。

## 跟进任务 U5：导入时选择「先存着」（2026-10-03）

规则在契约第 11 节（R21，序列 W5）。另开分支 `phase2/b4-U5-organize-later`，从 `origin/phase2/batch4` 建，Draft PR 的 base 是 `phase2/batch4`。

- 确认导入那一步加一个选择：「先存着，以后再整理（现在不调用 AI）」和「现在就整理」。默认是先存着。用一句话说清区别：存好就能问到里面的话；整理成带人、地点、时间的记忆可以以后再做。
- 上传时把选择作为表单字段 `organize`（`later` 或 `now`）带上。
- 进度那里：`organizeLater` 为真并且已经存完时，显示「已存好，还没开始整理」和一个「开始整理」按钮（调 `POST /v1/connectors/imports/{id}/organize`）。
- 界面上不出现「抽取」「批次」这类内部说法。
