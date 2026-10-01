# CI 跨包 vector fixture 初始化：一次 fresh DB 验证

产品基线 `0032883950d7dc72b1bdb160c1a98e3da17eded9`，实际固定 fixture 测试头 `1b539590cf611e5197cb9980d7882cc9dfd8e2df`。只改三个授权测试 helper：SIWC Store、Postgres testStore、legacy022 setup 在短事务取得与 Migrate 相同 advisory 键 `734826190`，初始化 `public.vector`，提交后再进入独立 schema/Migrate。产品、workflow、gold、业务断言不变，无新增 skip。

原 CI `make check` 跨包 race 并行时 SIWC 先到，三个叶全部在第139行因 vector 不存在失败，未到模型/DeskTurn 正反断言；同次 PostgreSQL race 包 **PASS 388.474s**。完整原日志保存在 [ci-original.log.gz](ci-original.log.gz)，SHA `3df2473072632bc23c8df709bed0e416af0dd14c142e1438ecbce9e5cfebef39`；[原终态/原因](ci-summary.json)记录1失败顶层/3失败叶。此记录来自 root 提供的实际 CI，无重跑或抹除。

获准在专属容器新建 `phase2_c_fixture_20261001`，owner phase2_c，没有预建 vector。[运行前](database-before.json)证实该 DB/UTF8/vector=false；[结束](database-after.json)证实同 DB 的 vector 在 public。各测试继续独立 schema，不触碰其它 DB。

实际 list 的三个顶层见 [list.txt](list.txt)，一次运行：

```sh
go test -race ./internal/ai/siwc ./internal/postgres -count=1 -json -run '^(TestPhase2SIWCUnknownModelRealStore|TestPostgresMigrations|TestPhase2Runtime023PreservesOldPolicyMeaningAndVersions)$'
```

**3 顶层/5 真叶全 PASS，FAIL/SKIP/未抵达均0，退出0，墙钟8.824s**。SIWC2.583s，Postgres1.430s。跨包按 Go 原有并发调度，没有预热 extension、串行包、重试或完整PG重跑。全部终态见 [summary.json](summary.json)，[执行参数](execution.json)。[原JSONL](acceptance.jsonl.gz)解压 SHA `9f323b1f20befdb2768c84602f684d599d33fff3e5e6651dfaf1899ebe8eb8d0`；[真实合成输入/manifest](inputs.tar.gz)保留 unknown-model普通问题、独立claim与raw拒绝三叶原断言证据。[fixture/gold SHA](fixture-and-gold-sha256.json)固定本次内容。

这验证 fresh DB 的测试基础设施与原三叶行为，未代表完整 make check 已重新通过。G5/F7 浏览器红属于独立产品路径，不由该 fixture 修复解释；本次不使用真实网络账户/模型，线上11/真机/一天试用仍未完成。
