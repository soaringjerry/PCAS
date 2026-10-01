# 新手动交接：真实 PCAS 浏览器最小闭环预期（冻结）

只读盘点确认现有 26 phase2_manual 例截获全部 /v1 API 与 clipboard.writeText；现有 real-backend.sh 默认 timezone、golden 三轮和四旧 backend spec 没有 manual requestRun/package/paste 操作。本表补两个真实闭环，不能把旧轮次当成本功能证明，不重跑 26 mocked。

独占新文件 `web/tests/phase2_manual_backend.spec.ts` 与本前缀评测文档；复用既有 support/real.ts、real-backend.sh 的 custom-command 入口和 golden 合成服务，不改产品/runner/旧 specs/CI 既有三轮门。所有 PCAS API 都真正到 serve+worker+己 Docker PostgreSQL，浏览器不 route/fulfill/stub；只被动记录相关请求和响应。golden 是外部服务替身，剪贴板用真实 Chromium permission/write/read API，不伪造复制成功。

正式执行必须使用 root 指定精确组合产品 SHA 并独立构建匹配 dist，先记录 harness SHA；环境由真实 runner 创建己 tmpfs container、临时配置、owner/token与loopback端口，不读 /root/PCAS、部署或个人账号配置。浏览器 spec 必须要求显式 loopback PCAS_TEST_BASE_URL/PCAS_TEST_FIXTURE_URL 与 runner 合成 token。source、owner、回答、prompt 全部合成。

| 两叶固定预期 | 操作与证据 | 不可放宽的断言 |
|---|---|---|
| real_target_package_clipboard_submit | 公开 POST source 与正式 manual authorization；owner task 保独立说明；UI 点手动转交、从真实 /v1/models 选友好名“验收假模型” golden，输入自然请求，实际 requestRun；preview、copy 各一个真实 GET；Chromium readText；UI 贴回合成回答 | 请求 manualRecipient 只有 provider，服务端绑定有效完整 manual route；原 prompt/owner 段保留；包含 exact source marker 与 manifest Input source，Run.Brief 不含 marker；两 GET attempt UUID 不同且 delivered_at 有、external_receipt=unknown；preview/真实 clipboard 分别等于对应真实包；paste 真成功，持久 done/output 与实际自动 doc 采纳/正文正确；manual 不触发 golden assistant 模型请求 |
| real_revoke_clear_refuse_regenerate_submit | 另建有 source 许可 waiting run，UI preview；正式 DELETE 原 authorization tuple；真实 3s poll 更新 stale；真实旧包 GET 与旧 paste 命令；UI 向原接收者重新生成新 run，preview/copy 无源许可包，再贴回并完成 | stale 前包正控含源；stale 后 preview 清空、copy/submit disabled、真实 clipboard 无新泄漏；旧 package 与最新 workspace revision 的旧 paste **409 / {error:version_conflict}**（按已存在 fail 映射，不能接受任意非2xx）；旧 output/采纳/文档无保存；新 run ID、原完整 recipient/prompt/old run 保留；新包含 owner、不含 revoked source marker，Input 无该 source；真实 clipboard 等于新安全包；新提交实际 done/自动 doc 采纳，owner 文字始终保留 |

精确 API 错误来自当前 server.go memory.ErrConflict→409/version_conflict 与 manual/package/paste 的 stale precondition，不是 mock 中使用的 context_changed。若指定产品头改变已审定协议，先报告差异，不接受宽泛任意失败。

source 入库可能触发 golden 合成 extraction（items=[]）；等待其正常完成避免后台 revision 写与 preview 相撞，允许此明确 fixture 调用。manual 流程必须无 assistant 模型外发，不把 extraction 事件混成 manual 调用；不接真实模型。模拟贴回内容仅证明 PCAS 真实提交/采纳，不证明外部模型接收或生成。

真实网络晚响应竞争、全目标过滤排列和390px几何不在本两叶覆盖内，保原 mocked/浏览器证据层边界。无真实手机或外部接收承诺。真实 clipboard 是此 Chromium 会话读回，不能冒称任意 OS/第三方应用粘贴。

先净提交冻结，再 harness+静态 lint/targeted TS compile/list，零动态。A组合到达优先执行已冻十二叶动作谱系矩阵，之后才经root指定同产品的这两叶一次；保存退出、计数、原日志、真实HTTP与桌面截图。失败不得skip/force/偶然重跑。
