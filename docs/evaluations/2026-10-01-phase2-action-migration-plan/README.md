# 024→025 independent migration harness

人工预期先冻结于 `c21975556353b22a8d497666a973107896c22c2a` 的 `action-migration-sequences.json`，SHA256 `34d5cf079b1741147ad56457eafe2fa65b0a97691f61ef53c3a7402b94e2f817`。产品DDL批准版本 `dd1ffc4b39a11fdf8bec219002f568b3a412ff9f`，025文件SHA256 `ce73cfd82375ee0af81a50736aa07e2f043757d8c78daeb33dfb7650e3e8a1d1`，作为固定预期，不从被测当前文件重新算预期来迁就变化。

新自有 `phase2_runtime_action_migration_test.go` 三顶层/十三叶：

1. **真实旧024升级及Undo四叶**。继续复用原022 fixture、应用原023与024真实SQL/账本，硬断言24条、没有新origin列。用真实PG文档触发器生成changes/afterHash，建立command-live、desk-undone、worker-expired、newer-command五行四状态；SQL标记历史undone/expired属于明确旧状态fixture，没有运行旧二进制API。升级后逐字段比较原action JSON（移除唯二新列）与原work_item完整JSON，含ID/source/body/afterHash/times/order不变；old context_task一律SQL NULL且context_stale=false；exact25且固定checksum。重复Migrate账本和内容不变。随后真实Store.Undo分别精确AlreadyUndone/Expired/NewerAction，live成功且实际事项删除/原receipt undone标记存在。
2. **八叶有限DDL行为**。SQL NULL、对象可存；array/string/number/boolean/JSON null必须PG SQLSTATE23514；context_stale SQL NULL必须23502；默认值NULL/false。各叶各自fresh schema、独立autocommit语句，不让预期错误污染其他SQL或Undo正控。对象被DDL接受不代表有效trusted task或runtime授权。
3. **fresh恰25/restart**。实际Migrate后exact25、025恰1、固定checksum、CheckSchema、再次Migrate完整账本字节不变，无配置model。

原022→023政策含义、023→024自动lease及旧gold断言均未更改；已有fresh至少24断言仍作原门，新用例独立精确fresh25。原K0/fresh22、023/024动态报告保留其精确产品版本，不能把后来迁移数量加一解释成它们此前失败或抹掉其证据。

编译开发基线仍 `7706baa4f7c66fb7415c294f2bd703df62503154`（不含025），随后只有C自有报告/fixture/gold变化。执行 `go test ./internal/postgres -run '^$' -count=1` 退出0、零动态。这些用例的SQL仅在root另给含冻结025的精确组合头执行，未把编译当迁移通过。

本计划只验证DDL与真实状态升级，不证明action-field读取/外发/promotion/undo全部runtime闭环；其独立矩阵由D负责，queued delegation origins补门由C另冻结D4。
