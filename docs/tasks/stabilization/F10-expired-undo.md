# F10：撤销有效期与资料删除后的明确拒绝

执行者：6.1 Sol / high，`f10_expired_undo`，不能是 T1 测试作者。T1 已交付 #26 / `6d0c27e` 并停止写入，F9 已释放 `actions_log.go`，可以启动。依据正式契约的 `expired` 规则及 T1-U12/U13、T3-D3 的独立证据。`newer_action` 的重叠语义仍待用户裁定，不在本次实现范围。

## 问题与要求

- 删除相关资料已清空快照并设置 `expired_at`，撤销目前仍返回 `changed_since`；应返回 HTTP 409 `expired`。
- 31 天前的动作在下次快照清理前仍可能撤销成功；有效期必须在撤销入口自行判定，不能依赖后续新动作触发清理。
- 自动清理后的旧动作同样应返回 `expired`；拒绝过程中不得恢复已删除内容或改动业务行。

对外统一沿用契约文案「超过 30 天或相关资料已删除，无法撤销」。保持未找到、已撤销、工作已开始和真正内容冲突的既有行为；已撤销与过期同时成立时，先提出明确的兼容判定顺序供协调者核对，再实现。不要扩展快照保留时长或改变指纹算法。

已核对的重叠顺序：查找并限定 owner → not_found → already_undone → expired → 既有恢复资格。依据 U11，同一原请求重放仍返回原结果，新请求对已撤销动作仍明确返回 already_undone，即使快照后来清理。当前实现先检查 expired_at，所以这项属于依既定语义修正，不能声称原实现已经如此。

## 归属与输入

在 `/root/PCAS-wt/F10` 从 F9 `26dba8672b578cf1baadd71c0ba257f7691d8109` 与 T1 `6d0c27e70ede296cb0247151ffc31f239051de3a` 无冲突集成 `stabilization/expiry-candidate`，记录并推候选 SHA，再建立 `stabilization/F10-expired-undo`。PR base 为该候选，明确依赖 #20/#24/#25/#26；main 未合并。冲突时报告，不自行改产品解决。

前端验证临时预览已登记 `127.0.0.1:18140`，由本任务独占并在结束时关闭；F8 保留 18138/18139。

预留 `internal/postgres/actions_log.go`、`internal/workspace/model.go`、`internal/httpapi/server.go` 的错误类型及 HTTP 映射、`web/src/store/api.ts` 的错误文案；`internal/telegram/poller.go` 仅 `expired` 文案分支，启动前必须与 Telegram 修复的文件归属排期。可以移除 `internal/postgres/stabilization_undo_test.go` 的 U12/U13 finding skip，保留独立断言。

旧测试只有确实把「过期」误记为内容冲突的断言可随正式规则修正，逐项列出原因；不能全局替换 `changed_since`。其他文件、迁移和共用 helpers 先报告协调者。唯一报告 `docs/evaluations/2026-10-01-expired-undo.md`。

已追加授权：`internal/postgres/undo_test.go` 的删除快照断言从 ChangedSince 改 Expired；保留期测试分别对未撤销 created 期待 Expired、已撤销 edited 期待 AlreadyUndone。`internal/telegram/poller_test.go` 仅在既有错误文案映射表增加 ErrExpired 一项。其他冲突/回调断言不变。

## 验收

1. 先复现原始 U12/U13 失败，再修复并去掉对应 skip；同时覆盖刚好未到期和已超期，不等待后台清理。
2. HTTP 和 Telegram 的错误说明准确，普通内容冲突、重复撤销、不存在动作仍保持兼容；删除后拒绝不恢复任何正文。
3. F9 的连续撤销及旧提醒抑制不回退；T1 其余明确用例通过。待裁定项继续明确标注，不强行置绿。
4. 按统一隔离规则运行 make check、数据库集成、相关前端检查，使用本地假服务，清理自有资源，交付独立 PR。不得合并或部署。
