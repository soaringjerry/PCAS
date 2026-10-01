# Phase2 历史 Go 测试叶数独立审计

## 范围与结论

仅从已有原始 JSONL 重新计数，没有运行产品、测试、数据库或浏览器。主输入是产品冻结头 `0032883950d7dc72b1bdb160c1a98e3da17eded9` 的 `docs/evaluations` 内全部 **16** 份 `.jsonl.gz`；另只读核验 C 的 `e6fd9b1902db7e3e05b882094027ff8841ecf1ab` 定点原日志（当时位于 `/tmp/pcas-phase2-c-name-r1/docs/evaluations/2026-10-01-phase2-name-causal-r1/acceptance.jsonl.gz`，未在0032883的16份清单中）。各历史运行的产品版本仍以其原报告为准，不能合并成0032883的一次验收。

独立结果确认 C 的 [R3/R4 勘误](2026-10-01-phase2-backend-counting-erratum/README.md)（提交 `f3c79b3`）；该勘误是在独立读取 raw 后交叉核对，没有用其修正表代替计算。另发现 **R2 也需同类勘误：514叶应为510叶，495 PASS应为491 PASS**。R1原报告及早期 runtime、source、AI、migration 数字无需此项修正。原报告、原日志、旧断言和结果文件均未修改。

## 计数规则与父级失败保护

以 `(Package, Test)` 为键。只纳入实际 `Action=run` 的名字；收集每个名字的全部 proper slash prefixes，而非只取最近一层 parent。已run的名字只要存在任何已run后裔，就不是叶。`t.Run("a/b", ...)` 不保证为中间 `/a` 产生run事件；缺失中间事件不能使实际顶层再次算作叶。没有实际run的中间名字不造新测试数。Go package终态（没有Test字段）不计为测试节点。

对每个已run节点独立保留 `pass/fail/skip` 终态。本批全部已run节点都有终态，未抵达不算PASS。**顶层终态、所有非叶父级失败、package终态和进程exit仍分别检查；排除parent仅定义叶，绝不抹去parent cleanup或收尾失败。**

本次17份原日志中，没有 `parent FAIL` 且其所有实际后裔都不失败的记录；也没有 package FAIL但无失败顶层的记录。这个结果来自单独检查，不由叶集合推出。所有原顶层失败集合、package终态及进程退出均保留。R1 panic批次仍是229选择、28抵达、201未抵达；后续各批亦不能据修正叶数声称某个Fatal后的业务步骤已执行。

下面是独立计算的核心（只读gzip）：

```python
import collections, gzip, json

events = [json.loads(line) for line in gzip.decompress(path.read_bytes()).splitlines()]
runs = {(e["Package"], e["Test"]) for e in events
        if e.get("Action") == "run" and e.get("Test")}
terminal = {(e["Package"], e["Test"]): e["Action"] for e in events
            if e.get("Action") in {"pass", "fail", "skip"} and e.get("Test")}
ancestors = {(pkg, "/".join(name.split("/")[:i]))
             for pkg, name in runs for i in range(1, len(name.split("/")))}
leaves = runs - ancestors
tops = {key for key in runs if "/" not in key[1]}
assert runs <= terminal.keys()
leaf_counts = collections.Counter(terminal[key] for key in leaves)
top_counts = collections.Counter(terminal[key] for key in tops)
parent_fail_without_failed_descendant = [
    key for key in runs & ancestors if terminal[key] == "fail"
    and not any(terminal[child] == "fail" for child in runs
                if child[0] == key[0] and child[1].startswith(key[1] + "/"))
]
package_terminal = [(e["Package"], e["Action"]) for e in events
                    if not e.get("Test") and e.get("Action") in {"pass", "fail", "skip"}]
```

## 受影响的已发布数字

下表只修统计口径，P/F/S分别为PASS/FAIL/SKIP。R2/R3各多算4个PASS父节点；R4多算6个PASS父节点。没有减少这些批次的失败或skip。

| 批次 | 原报告叶总数与 P/F/S | 正确叶总数与 P/F/S |
| --- | --- | --- |
| postgres R2 | 514；495 / 17 / 2 | **510；491 / 17 / 2** |
| postgres R3 | 543；534 / 7 / 2 | **539；530 / 7 / 2** |
| backend R4 memory | 33；33 / 0 / 0 | **31；31 / 0 / 0** |
| backend R4 postgres | 546；541 / 3 / 2 | **542；537 / 3 / 2** |
| backend R4 合计 | 579；574 / 3 / 2 | **573；568 / 3 / 2** |

三个批次中误计的 postgres PASS父节点相同：

- `TestAdoptionMatchesFrontend`
- `TestAutoAdoptDestinationsAndUndo`
- `TestAutoAdoptFailurePreservesCompletedResult`
- `TestNotifyHTTPTelegramErrors`

R4另误计两个 memory PASS父节点：

- `TestPhase2ContextServiceForwardsOptionalPoliciesExactly`
- `TestPhase2ContextServiceOwnerBoundaryPrecedesRepository`

R1原报告已经正确记录111叶（91P/20F），因此无需改为其他数。若对R1重新使用错误的一层parent算法，会得到113叶（92P/21F）：它会额外纳入 `TestAdoptionMatchesFrontend` PASS及 `TestAutoAdoptDestinationsAndUndo` FAIL；后者的真实失败仍在独立顶层统计中，不得靠叶计数修正隐去。AI observation原报告已经正确记录10叶；一层parent算法会误计其PASS父 `TestPhase2ContextHTTPFinalBytesAndBeforeDispatchRefusal` 得到11，原报告没有采用该错误值。e6fd定点实际 **4顶层/12叶全部PASS**，无未抵达；确认其原日志，没有新增一次运行。

## 每批独立终态核查

顶层与叶列均为P/F/S。package列直接取JSONL无Test字段的终态；exit取原执行记录/summary，不从叶数推断。各行是不同运行，不能将它们相加作为一次产品验收。

| 原日志批次 | 顶层数；P/F/S | 叶数；P/F/S | package终态 | 原process exit |
| --- | --- | --- | --- | --- |
| [action migration R1](2026-10-01-phase2-action-migration-r1/acceptance.jsonl.gz) | 6；6 / 0 / 0 | 18；18 / 0 / 0 | postgres: PASS | 0 |
| [AI observation](2026-10-01-phase2-ai-observation/acceptance.jsonl.gz) | 3；3 / 0 / 0 | 10；10 / 0 / 0 | ai: PASS；siwc: PASS | 0 |
| [backend R4](2026-10-01-phase2-backend-r4/acceptance.jsonl.gz) | 244；240 / 2 / 2 | 573；568 / 3 / 2 | memory: PASS；postgres: FAIL | 1 |
| [manual fixture](2026-10-01-phase2-manual-fixture/acceptance.jsonl.gz) | 1；1 / 0 / 0 | 1；1 / 0 / 0 | postgres: PASS | 0 |
| [postgres R1](2026-10-01-phase2-postgres-r1/acceptance.jsonl.gz) | 28；23 / 5 / 0 | 111；91 / 20 / 0 | postgres: FAIL | 1 |
| [postgres R2](2026-10-01-phase2-postgres-r2/acceptance.jsonl.gz) | 230；215 / 13 / 2 | 510；491 / 17 / 2 | postgres: FAIL | 1 |
| [postgres R3](2026-10-01-phase2-postgres-r3/acceptance.jsonl.gz) | 236；228 / 6 / 2 | 539；530 / 7 / 2 | postgres: FAIL | 1 |
| [runtime migrations](2026-10-01-phase2-runtime-migrations/acceptance.jsonl.gz) | 4；4 / 0 / 0 | 13；13 / 0 / 0 | postgres: PASS | 0 |
| [runtime R1](2026-10-01-phase2-runtime-r1/acceptance.jsonl.gz) | 31；21 / 10 / 0 | 69；41 / 28 / 0 | postgres: FAIL | 1 |
| [runtime R2](2026-10-01-phase2-runtime-r2/acceptance.jsonl.gz) | 31；30 / 1 / 0 | 69；67 / 2 / 0 | postgres: FAIL | 1 |
| [runtime read gate R3](2026-10-01-phase2-runtime-read-gate-r3/acceptance.jsonl.gz) | 1；1 / 0 / 0 | 2；2 / 0 / 0 | postgres: PASS | 0 |
| [source CI fixture R1](2026-10-01-phase2-source-ci-fixtures/r1.jsonl.gz) | 2；1 / 1 / 0 | 2；1 / 1 / 0 | postgres: FAIL | 1 |
| [source CI fixture R2](2026-10-01-phase2-source-ci-fixtures/r2.jsonl.gz) | 1；0 / 1 / 0 | 1；0 / 1 / 0 | postgres: FAIL | 1 |
| [source CI fixture R3](2026-10-01-phase2-source-ci-fixtures/r3.jsonl.gz) | 1；1 / 0 / 0 | 1；1 / 0 / 0 | postgres: PASS | 0 |
| [source R1](2026-10-01-phase2-source-r1/acceptance.jsonl.gz) | 7；3 / 4 / 0 | 11；6 / 5 / 0 | postgres: FAIL | 1 |
| [source R2](2026-10-01-phase2-source-r2/acceptance.jsonl.gz) | 7；7 / 0 / 0 | 13；13 / 0 / 0 | postgres: PASS | 0 |
| [e6fd Name/Undo 因果定点](2026-10-01-phase2-name-causal-r1/acceptance.jsonl.gz) | 4；4 / 0 / 0 | 12；12 / 0 / 0 | postgres: PASS | 0 |

## 原始输入字节核验

主输入gz读取前后均吻合下表SHA；解压SHA全部与各批原summary登记值相同。没有重新压缩或修改原raw。下列相对路径位于 `docs/evaluations/`；e6fd来源如首段所列。可用 `sha256sum <path>` 核对压缩字节，用 `gzip -dc <path> | sha256sum` 核对原JSONL。这两个命令只读资料，不执行测试。

| JSONL gzip相对路径 | 压缩字节SHA256 | 解压JSONL SHA256 |
| --- | --- | --- |
| `2026-10-01-phase2-action-migration-r1/acceptance.jsonl.gz` | `fb2eef9c17b14ebb091a2225da5a2440c5b86c0bbe279164c870d2ee358aac4b` | `3aadffe96f895f304742a8b3d4f2da8cebf2d8f934fae01e9b4b54bf158546bf` |
| `2026-10-01-phase2-ai-observation/acceptance.jsonl.gz` | `bae7bb9baab541b4c852bd2820541355e1623e3915d88c72ab015a306dce1dae` | `0cacb66de680fad1fa1842c964da4d04373009f90efbffa266bbb04b771b3877` |
| `2026-10-01-phase2-backend-r4/acceptance.jsonl.gz` | `42937ceaa9e3efa9dd1fc2aee11148410283de64eee26aa63ec0dc5971e9dc2c` | `18de99dd5436a43007eaa4282c88b12611a740c515ee5c2472ee82353487a2d0` |
| `2026-10-01-phase2-manual-fixture/acceptance.jsonl.gz` | `a1deb900904291dd95506952b76913377f149bf134b97b8280aca0c16797a472` | `fcf02faca427636feca1a38f27c5ee6ff7cbf384a24dbb2ff730f007cf7d2b0e` |
| `2026-10-01-phase2-postgres-r1/acceptance.jsonl.gz` | `afbaec29235c6bcd8f65c744e5e50068d7628ebd1c2d9d0155e2b6558a99e8cd` | `c4017fa4b4a316d5b94b601b8d01361bf79cb2e9bbef1bfe5762975a577cd06e` |
| `2026-10-01-phase2-postgres-r2/acceptance.jsonl.gz` | `3b2b5f5947d80ce097e0f2f8dc77919104358ba57e9c69e4a5fe89fd91c8e82b` | `1d033922132809f05aecb4ab9ce1ad2a51679e875711a82a61f6019f2360b6cd` |
| `2026-10-01-phase2-postgres-r3/acceptance.jsonl.gz` | `7f57186116a1eb4e5c64bd3db235734d119fdcfd20ecd0dc9b0121a88afc8b5d` | `345bbc0abeb838f9ed406145eeb74d902d910c29776b4e8ee1de9ad3e93a2aac` |
| `2026-10-01-phase2-runtime-migrations/acceptance.jsonl.gz` | `ffc4d33adf986903638ad489e69e005924e0b88c6a4f54807bff728037a5867b` | `c84e2ce0d18c4c04dff67fb508e783f673a483aea6ee623a6a89cee6e90806fc` |
| `2026-10-01-phase2-runtime-r1/acceptance.jsonl.gz` | `5e75b54aad25aead0d0a6b622b34668a1758ce5d87f39154ae922aee72d1464d` | `eb397f15c1a6ab73a1aa1fdbfae3f0bdec119a9ba50e13daefbd68a4545def95` |
| `2026-10-01-phase2-runtime-r2/acceptance.jsonl.gz` | `2e0cd360e91be700b7e9f2b301fb624dc766a56b4356805dc00cfd633f01cf1d` | `57bba8a1912dd917a59edccf63c29dd5411c7a97c3c332faa5ddc1c6328f56b3` |
| `2026-10-01-phase2-runtime-read-gate-r3/acceptance.jsonl.gz` | `f3e066e64f6bd9c49aa814ff38d45c22ec9738f5fa51d1bf38dcfab6b775242b` | `6498dea1da6d57b389eecac380d1430b87ae79457c10fefb0096394a42cfdae2` |
| `2026-10-01-phase2-source-ci-fixtures/r1.jsonl.gz` | `9ee7bded533d93446a4de15fc86fcf1664b9fea4f5dc7b80fcea2fda8660e24b` | `aa95b6ec81e1003f0daf84ac6867f21857f3c596ad0f0a0d3d2e495a7d742618` |
| `2026-10-01-phase2-source-ci-fixtures/r2.jsonl.gz` | `4410e8d2fc7eebb425165019a4675f5a1409a87e6e4202bdd6afc9d84d9147e1` | `a9301b07b36c28dbabde5e4e961a9437c89861aefe22265edf2575c93bb603a1` |
| `2026-10-01-phase2-source-ci-fixtures/r3.jsonl.gz` | `4cba744914a192b3c242ddf26a121d05fe08ff4b3cab1499a521783eaa17160f` | `b36beabf5466d2f8235b962c494755cb0f0a65028f376f9c1f726fc5e5e52a62` |
| `2026-10-01-phase2-source-r1/acceptance.jsonl.gz` | `f10161b14c5ebb6486f50c7ceb865d72f1ae6274c853a2e44e873c10cc4ef215` | `a17748f6782b1bc6ec3e0142152d23b4264f3cd427909ee5e5399fbb3ef8e692` |
| `2026-10-01-phase2-source-r2/acceptance.jsonl.gz` | `2e22eb3a0fcecec9df8fdf8e61898066bd0ed3947225da0e8e397ce51c50c264` | `c4925a2ccc8053105104f6f3c16e60ac4bc94646f93f337600708e91db140d81` |
| `2026-10-01-phase2-name-causal-r1/acceptance.jsonl.gz` | `c7b6c2480ea0a62bd7b6bf68c589e65505b89e0dd738257fc7ce1f58516f2ece` | `319208006ff3ed555329815fe98cdf715fbe3b614f9d839c5b532f574f6ec9bd` |

这是文档统计勘误的独立审计。产品责任、失败原因、skip来源、退出状态以及C正在执行的0032883完整门均没有由本次计数核查改变；该新门不在这17份历史输入中。
