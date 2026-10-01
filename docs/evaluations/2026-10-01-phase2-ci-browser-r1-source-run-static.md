# SourceRun UI harness与静态检查

冻结预期提交 `6a3ba0c`，契约 `2fd719c784dd41d2f520ef1f001b2a1742126af4`；产品基线仍为0032883，仅增加授权的两个spec断言。未启动浏览器、真实API、模型或数据库，未动态运行四例。

- golden只有G5新增真实snapshot原failed Run定位、retry请求sourceRunId精确等于该ID、manualRecipient字段缺席，并保存被动请求证据。原失败提示、retry实际click、完成三步骤等待及实际副手model事件数2断言保留。
- manual_backend仅增加普通全新请求sourceRunId字段缺席、原目标恢复sourceRunId严格等于原Run ID。原目标、manualRecipient-only-provider、新Run ID、prompt、五次package、实际clipboard、精确409、撤权atom拒供及自动doc采纳约束全部保留。
- F7所有原代码与断言保持字节一致；没有新retries、skip、force click或fixture改动。

| 新harness文件 | SHA256 |
| --- | --- |
| `web/tests/golden.spec.ts` | `b2e9e83d7d575d079f077394f10768935d1a6634cc6a10bfaa90ec38f7331ada` |
| `web/tests/phase2_manual_backend.spec.ts` | `7bfa1acab6b0b126573536b46c6ae29ae211efa93af003b180cef6d665257489` |

Node24.18.0；复用既有依赖及此前独立临时Node类型入口 `/tmp/pcas-phase2-manual-backend-tooling/node_modules/@types`，没有改package/lock/config，临时node_modules链接检查后已移除。

| 检查 | 原退出 / 结果 |
| --- | --- |
| 两spec ESLint | 0，无诊断 |
| 两spec严格TypeScript noEmit（ES2022/DOM、bundler、真实Node typeRoots） | 0，无诊断 |
| 首次Playwright --list未配置fixture URL | 1，support/real.ts显式环境门拒绝；0 tests listed，未启动测试 |
| 只为list配置两个loopback discard端口URL后同四例选择 | 0，恰4 tests / 2 files；没有监听或访问该端口、没有动态执行 |

静态list选择为 `G5 |F7 连续撤销|real_target_package_clipboard_submit|real_revoke_clear_refuse_regenerate_submit`、retries0；首次环境缺失日志保留。原日志用gzip无损保存，解压SHA如下：

| 原检查日志 | 解压SHA256 |
| --- | --- |
| [lint](2026-10-01-phase2-ci-browser-r1-source-run-lint-original.log.gz) | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| [ts](2026-10-01-phase2-ci-browser-r1-source-run-ts-original.log.gz) | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| [list](2026-10-01-phase2-ci-browser-r1-source-run-list-original.log.gz) | `f123c5f3885b994c707a49519a9a70e7796a4fe2d579a624531f1a34a0c9f92c` |
| [list-environment](2026-10-01-phase2-ci-browser-r1-source-run-list-environment-original.log.gz) | `c7eeefaa1dbf45fe20857a99bb51a973432b4bd288861a6ebf69d36311a5c9ec` |

独立只读审阅A净UI `11e20832cad26d5e4edd0fd94c236365588c01f7`：ThingPage五类已有Run发送均携带run.id或initialRun.id；预填prompt state的fill/trim、标点修改、切换接收者没有改定位；无initialRun的普通入口使用initialRun?.id。StoreProvider原样spread request，不自行替换ID。

具体静态阻塞已交root/A：`api.ts`全局JSON stringify replacer将undefined转null。11e2083虽在helper对API返回undefined，callsite仍写manualRecipient键；普通新请求同理sourceRunId键，实际会序列化null而不是字段缺席。冻结缺席断言不放宽，root已授权作者仅局部条件组装键，保全局replacer。本文只记录11e2083当时的发现，等待净修复与精确组合头后补静态审；compile/list通过不证明产品已满足该边界。

生成Prompt失效后的自动按钮禁用属于作者后续产品范围；现有owner独立非空Prompt的manual恢复不能因该门被禁用。本批没有新增动态叶来冒充这条全部分支已实测。C独立backend gold承担生成来源撤/纠/删及修改标点不洗来源；最终四例实测需root授权的新精确头。

## 35e8f03净补的独立静态复审

净产品 `35e8f038023b80a6035efe8fdf746135c7844857` 接11e2083，只变ThingPage；本次只读复审，没有checkout该产品运行测试。API helper/replacer与原StoreProvider没有泛改。运行时API recipient被helper拒返回后，`manualSelection`为无键对象，三个RunRow retry/regenerate实际spread该对象，故不再产生manualRecipient:null；manual selection有值时仅provider。普通ManualRequest只在initialRun存在时spread sourceRunId，普通新请求确无该键；預填变字/trim/换target都不改变initialRun.id。

| 净补发送入口 | 原Run定位 | manual选项 |
| --- | --- | --- |
| ManualRequest预填及其修改 | initialRun存在才加入其id | 显式选择provider |
| Handoff原目标重新生成 | run.id | 已核manual选择provider |
| RunRow原agent/换API重试 | run.id | 原manual agent才带manualSelection，换API不带 |
| stale done清空output后重新生成 | run.id | API无键；manual仅provider |
| done output普通重新生成 | run.id | API无键；manual仅provider |

失效已清空Prompt时，各自动retry/regenerate通过 `!run.prompt.trim()` 禁用，已有Run预填入口收起并提示回秘书重新描述；无来源定位开关。独立owner非空Prompt即使stale，hasPrompt仍true，Handoff原目标重建不因stale被禁用，现两manual恢复例原流程可继续。这个判断是静态控制流结论，未冒称空Prompt分支或编辑/换目标已动态测试。

因此11e2083的undefined→null具体阻塞在35e8f03局部请求组装修正中已静态闭合；最终组合的四例实际body仍按已冻结原断言验，尚未动态执行。
