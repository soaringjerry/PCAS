# V2 虚构实验数字产物

全部来自 671 条虚构记忆和 120 个虚构任务；没有记忆正文、请求或回答正文。JSON 用 gzip 无时间戳封装，减少仓库重复体积；SHA256 均针对解压后的原始 JSON 字节。

- `original.partial.json.gz`：原跑次的 779 个完成行；不是完整成绩。
- `original.interruption.json`：从仅含题号/方法的开始日志得到的中断账；原驱动没有保存八个其他在途流程的调用数，故仅给界限。
- `completion.json.gz`：未改标准、缺第三轮任一方法的 85 题，三方法共 255 行。
- `replacement.json.gz`：补齐别名依据的 20 题，三方法、三次共 180 行。
- `result.json.gz`、[汇总表](result.md)：最终 1,080 行及全部逐次指标、波动、争议编号。
- `result.provenance.json`：源 SHA、保留行数和由原缺失状态/标准变更自动得到的题号集合。

重算（仓库根目录，不读数据库、不调用模型）：

```sh
git show a666fe0:testdata/phase2_5/doing/suite.json > /var/tmp/v2-original-suite.json
# 目录由用户为自己的本机文件创建，不删除共享目录
mkdir -p /var/tmp/v2-recompute
go run ./cmd/pcas-eval/doing-merge \
  -original-suite /var/tmp/v2-original-suite.json \
  -original docs/evaluations/2026-10-04-phase2_5-v2-artifacts/original.partial.json.gz \
  -completion docs/evaluations/2026-10-04-phase2_5-v2-artifacts/completion.json.gz \
  -replacement docs/evaluations/2026-10-04-phase2_5-v2-artifacts/replacement.json.gz \
  -output /var/tmp/v2-recompute/result
```

合并不根据分数选择行。原报告保留 645 行，弃 132 个别名受影响行及 2 个未齐任务的已有第三轮行；加上 255 补跑和 180 修订重跑，恰为 1,080 行。输入条件不符、重复、漏方法、类别/评分不符均拒绝。三源完整行共 1,214 行；丢弃行的调用/耗时不混入最终成绩。

`revision` 是执行时传入的标识：原报告为产品基准 `0d49147`，补跑记录创建时的评测分支 HEAD。补跑使用了随后提交为 `312f191` 的故障记录改动；它不改变固定回答/评分指令或成功行打分规则。产品实现一直是该基准，所有 source 的指令 SHA、模型、host date、as_of、600 字符上限、并发数由合并器核对。

中断、标准修订、额外调用及测量局限见[实验记录](../2026-10-04-phase2_5-v2-doing.md)。这是一次显式重建的三次测量，不能称作一次无中断的全量运行。
