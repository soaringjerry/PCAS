# F10：撤销有效期与资料删除后的明确拒绝

执行者：6.1 Sol / high，不能是 T1 测试作者。尚未启动；等待 T1 最终测试交接和 F9 释放 `actions_log.go`。依据正式契约的 `expired` 规则及 T1-U12/U13、T3-D3 的独立证据。`newer_action` 的重叠语义仍待用户裁定，不在本次实现范围。

## 问题与要求

- 删除相关资料已清空快照并设置 `expired_at`，撤销目前仍返回 `changed_since`；应返回 HTTP 409 `expired`。
- 31 天前的动作在下次快照清理前仍可能撤销成功；有效期必须在撤销入口自行判定，不能依赖后续新动作触发清理。
- 自动清理后的旧动作同样应返回 `expired`；拒绝过程中不得恢复已删除内容或改动业务行。

对外统一沿用契约文案「超过 30 天或相关资料已删除，无法撤销」。保持未找到、已撤销、工作已开始和真正内容冲突的既有行为；已撤销与过期同时成立时，先提出明确的兼容判定顺序供协调者核对，再实现。不要扩展快照保留时长或改变指纹算法。

## 归属与输入

启动时指定含 F7、F9 与 T1 测试的精确候选 SHA、隔离 worktree 和 PR base；原 T1 执行者停止修改测试后再交接。

预留 `internal/postgres/actions_log.go`、`internal/workspace/model.go`、`internal/httpapi/server.go` 的错误类型及 HTTP 映射、`web/src/store/api.ts` 的错误文案；`internal/telegram/poller.go` 仅 `expired` 文案分支，启动前必须与 Telegram 修复的文件归属排期。可以移除 `internal/postgres/stabilization_undo_test.go` 的 U12/U13 finding skip，保留独立断言。

旧测试只有确实把「过期」误记为内容冲突的断言可随正式规则修正，逐项列出原因；不能全局替换 `changed_since`。其他文件、迁移和共用 helpers 先报告协调者。唯一报告 `docs/evaluations/2026-10-01-expired-undo.md`。

## 验收

1. 先复现原始 U12/U13 失败，再修复并去掉对应 skip；同时覆盖刚好未到期和已超期，不等待后台清理。
2. HTTP 和 Telegram 的错误说明准确，普通内容冲突、重复撤销、不存在动作仍保持兼容；删除后拒绝不恢复任何正文。
3. F9 的连续撤销及旧提醒抑制不回退；T1 其余明确用例通过。待裁定项继续明确标注，不强行置绿。
4. 按统一隔离规则运行 make check、数据库集成、相关前端检查，使用本地假服务，清理自有资源，交付独立 PR。不得合并或部署。
