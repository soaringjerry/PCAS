# 首轮浏览器CI失败只读归因与假服务协议迁移

协调者提供：PR44首轮head83facc4，run36869401690；browser-mocked PASS，三轮real-backend均失败G4、G5、F7采纳链（golden.spec.ts 82/97/222），既有其余断言不改。完整原CI日志保留于/tmp/pcas-phase2-consumers-browser-ci-r1.log；本任务下载round1 artifact至/tmp/pcas-phase2-ci-r1-artifact，未本机重跑三轮。

直接读取三份失败trace里的实际/v1/desk/turn response：

| 失败 | 实际receipt | state.runs | 主失败层 |
| --- | --- | ---: | --- |
| G4三步采纳 | op=delegate,status=skipped,reason=这件事暂时办不了 | 0 | delegate准备，未建立run |
| G5首错后重试 | 同上 | 0 | delegate准备，未到首次assistant503 |
| F7采纳链 | 改名update成功；delegate同上skipped | 0 | delegate准备，未到deputy采纳 |

因此当前首次CI失败不能归因成fake正文解析失败。A正修产品delegate准备；该结果尚待组合头CI复验。

另有直接源码确认的后续fixture协议阻塞：internal/postgres/runs.go的实际Run请求要求output/used JSON，并strictJSON解析；internal/testsupport/golden/main.go normal assistant仍返回原plain checklist。按协调者授权，本次只在assistant且系统指令明确“output为完整建议或草稿”时，把原selected.Content包装成`{"output":原文本,"used":[]}`。secretary/extraction/普通其他模型格式不改；同一处原Delay/Status错误分支与Event记录/Once规则消耗次序不改，所以既有error-first和callcount期望不降低。golden.spec、workflow三轮和gate没有修改。

此迁移是独立的旧fake协议适配，不是已证明的首轮主因修复。只build假服务；新组合头整体CI由协调者执行。

验证：`go build -o /tmp/pcas-phase2-golden-fixture-protocol ./internal/testsupport/golden`退出0；源码基线5cbcd01。没有执行golden/browser三轮，没有修改失败日志或宣称新CI已经通过。
