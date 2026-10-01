# 当前c8 mocked原红简报（未重跑）

run36887421678/attempt1，job110454170387 completed/failure。实际checkout de9906eeece777e5016d50b0d85a1d0b72a68b40，其merge parents为6fd9d173及c8adbaeb；GitHub git commit API确认tree=e615cd44aa99df110a03592d965afafb06a21c8b，与c8adbaeb源tree一致。

原实际127条全部抵达：125PASS/2FAIL/0skip；test step退出1。phase2_manual26条24P/2F，其余101P。两个失败仅原target重建shape断言178/200：实际manualRecipient exact {provider:writer}，旧Expected多六个principal/role/channel/model/protocol/fingerprint。原流程运行时sourceRunId都已带原合成runId，见安全只读trace抽取mocked-two-actual-requests.json；响应是原mock fulfil200，仅说明前端组装，不冒称真实后端绑定。

package失败例已完成先preview、copy触发失败、清preview、点击重建、命令数1；失败后的新ID/clipboard空/clean校验未抵达。failed retry例已实际click重试、命令数1；失败后的新ID及clean校验未抵达。原全部这些断言将保留，未来通过不得追补成此原批已PASS。

完整原log留 /tmp/pcas-phase2-final-browser-ci-original/mocked-job-original.log；ZIP留mocked-artifact-original.zip，artifact11175561110 SHA256=d56db0fb0c9d9a3ea6de18608388c2b12c2f261730833d28056a67c42f6ba2c4，实算匹配GitHub digest，两原trace/error-context保留。最终三轮日志/产物归档将随后独立报告，不为本红改产品/runner或自行重跑。

根因：独立mock harness旧完整响应身份形状错误套用到新的provider-only选择请求，与80bcd46真实恢复形状错误同类。修订仍exact provider-only、原writer和sourceRunId原run，不放宽成任意partial receiver。原失败业务断言不删。原名称保留以对应历史叶。
