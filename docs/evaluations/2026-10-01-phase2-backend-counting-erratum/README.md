# 既有R3/R4叶计数勘误：原运行与失败不变

统计脚本错误来自只排除 `name.rsplit('/', 1)[0]`。Go测试可直接写 `t.Run("a/b", ...)`；JSON事件有完整child路径而未必有中间 `/a` 的独立run事件。只取一层parent会漏掉真正顶层祖先，把该父终态额外算作叶。

**正确规则**：按包处理，终态Test仅在没有任何实际run Test名以 `本名 + "/"` 开头时算叶。等价地，排除全部实际run名字的所有proper slash-prefix祖先；不只排最近一层。顶层另计、package终态不当Test。

```python
ancestors = {
    '/'.join(name.split('/')[:i])
    for name in actually_run_tests
    for i in range(1, len(name.split('/')))
}
leaves = {name: action for name, action in terminal_tests.items()
          if name not in ancestors}
```

这是原始JSONL的**重新计数**，没有新增测试、产品变化或重跑；不修改旧报告原文、raw或既有gold/assertions。旧失败集合、顶层数、退出和未抵达都不变。

| 既有运行 | 原报告叶 PASS / FAIL / SKIP | 正确叶 PASS / FAIL / SKIP | 正确叶总数 |
| --- | --- | --- | --- |
| R3 postgres | 534 / 7 / 2 | **530 / 7 / 2** | 539 |
| R4 memory | 33 / 0 / 0 | **31 / 0 / 0** | 31 |
| R4 postgres | 541 / 3 / 2 | **537 / 3 / 2** | 542 |
| R4 合计 | 574 / 3 / 2 | **568 / 3 / 2** | 573 |

R3原JSONL SHA `345bbc0abeb838f9ed406145eeb74d902d910c29776b4e8ee1de9ad3e93a2aac`；R4原SHA `18de99dd5436a43007eaa4282c88b12611a740c515ee5c2472ee82353487a2d0`。对应[原R3报告](../2026-10-01-phase2-postgres-r3/README.md)、[原R4报告](../2026-10-01-phase2-backend-r4/README.md)。[逐包原值/修正值/受影响父测试/原raw SHA](corrected-counts.json)可直接回查原日志。

R3/R4 postgres误计的四个PASS父测试：AdoptionMatchesFrontend、AutoAdoptDestinationsAndUndo、AutoAdoptFailurePreservesCompletedResult、NotifyHTTPTelegramErrors。R4 memory误计的两个PASS父测试：Phase2ContextServiceForwardsOptionalPoliciesExactly、Phase2ContextServiceOwnerBoundaryPrecedesRepository。没有新增或减少失败/skip叶。

e6fd9b1的因果定点采用正确规则：4顶层/12真叶PASS，实际叶集合与先冻人工12叶逐名相同。后续0032883完整两包也使用正确规则。更早历史统计另由D独立核对，不以本表未覆盖的旧数字当已经复核。

## R5 Service facade 专项复核

R5 正文的 **20 叶正确**：四方法成功/错误透传8叶、nonowner/invalidowner边界8叶、不支持能力4叶。两个PASS父终态未包含在20中；旧错误算法会将其另计而得到22。R5 memory正确总31叶为facade20 + TaskJSON3 + Ingest7 + chunks1。按原raw重新统计的[实际20叶逐名终态与SHA](facade-r5-recount.json)保持全部PASS，没有重跑或产品/测试变化；旧R4原文不改。
