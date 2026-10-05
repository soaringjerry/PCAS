# V2c 三档数字产物

仅含旧 671 条虚构记忆 / 120 题的数字、题号和双判结果。题目、标准、既有产物不变。对应评测记录第 8 节；真实题未运行。

- `result.json.gz`：完整 840 行；light/medium 各 360 行，heavy 跨分组/外发共 120 行。`resume_sources` 保留来源及失败尝试。
- `initial.partial.json.gz`：首次运行正常中断后的原始 checkpoint；已完成行保留，没有按成绩重跑。
- `http.partial.json.gz`：HTTP 续跑结束的 839 个有效结果和唯一缺项；最终仅补第 3 遍 light / PLAN-13，839 行继续原样保留。
- `comparison.json`：原 671 条基线的 current/ideal 与新三档，在 old120 及共同的 cross/outgoing40 上分别复算。共同子集与全体重叠，不能相加；heavy 没有全 120 题均值。
- `preparation.json`：整理、比较、建卡和交接说明的独立耗时、调用数及状态计数。
- `prepared-membership.json.gz` / `membership-diagnostic.json`：仅原虚构记忆编号的准备状态及标准引用原文的存续核查。没有数据库原编号、原话、回复、凭据或 dump。
- `transport-change.json`：WebSocket 握手 403 的诊断、中断和独立 HTTP 预检；新连接方式沿用同一模型/high/fast/后端登录。
- `planning-date-diagnostic.json`：第一遍中档间接规划的 10 个不可用结果，有限人工查阅本次虚构调用日志得到的事实标记；8 个存在冻结日期与原生自查日期冲突，2 个仅超过字数。没有重新打分或提交回复正文。
- `ledger.json`：文件指纹、实际尝试账及日志事件计数。提供方内部连接重试次数与 Generate 调用数分列，不当作精确货币花费。

`ledger.conservative_usability` 另做保守核查：有过实际失败尝试的单元视为不可用，包含人工中断取消的调用；未发起的 `run_aborted` 不罚分。它不改完整报告的逐行双判结果、不构成新的独立一遍，显示缺行补齐可能带来的完成条件偏差。

压缩文件的 mtime 固定为 0，解压后是原始 JSON 字节。`resume_sources.sha256` 对应来源的**解压字节**；`comparison.sources.*.file_sha256` 对应压缩文件本身，`decoded_sha256` 对应解压字节。

在仓库根目录，无数据库、无模型地验证完整矩阵、双判量尺、调用记账并重新汇总：

```sh
go run ./cmd/pcas-eval/doing-tiers-report \
  -tiers docs/evaluations/2026-10-05-phase2_5-v2c-artifacts/result.json.gz \
  -output /var/tmp/pcas-v2c-recomputed
cmp docs/evaluations/2026-10-05-phase2_5-v2c-artifacts/comparison.json \
  /var/tmp/pcas-v2c-recomputed.json
```

核对两个来源的解压指纹、470/839 行逐项保留，并复算保守可用数和尝试总账：

```sh
python3 - <<'PY'
import gzip, hashlib, json
from pathlib import Path

root = Path('docs/evaluations/2026-10-05-phase2_5-v2c-artifacts')
load = lambda name: json.loads(gzip.decompress((root / name).read_bytes()))
r = load('result.json.gz')
ledger = json.load(open(root / 'ledger.json'))
key = lambda x: (x['run'], x['method'], x['task'])
completed = {key(x): x for x in r['rows']}
assert len(completed) == len(r['rows']) == 840
for index, name in enumerate(('initial.partial.json.gz', 'http.partial.json.gz')):
    raw = gzip.decompress((root / name).read_bytes())
    prior = json.loads(raw)
    assert hashlib.sha256(raw).hexdigest() == r['resume_sources'][index]['sha256']
    assert len(prior['rows']) == (470, 839)[index]
    assert all(completed[key(x)] == x for x in prior['rows'])
failures = [f for source in r['resume_sources'] for f in source['failures']]
bad = {key(f) for f in failures if f['error'] != 'run_aborted'}
categories = {'old120': None, 'cross_outgoing40': ('cross_group', 'outgoing'),
              'direct_recall20': ('direct_recall',), 'outgoing20': ('outgoing',)}
for item in ledger['conservative_usability']:
    cats = categories[item['scope']]
    rows = [x for x in r['rows'] if x['run'] == item['run']
            and x['method'] == item['method'] and (cats is None or x['category'] in cats)]
    assert len(rows) == item['tasks']
    assert sum(x['usable'] for x in rows) == item['usable_completed']
    assert sum(x['usable'] and key(x) not in bad for x in rows) == item['usable_with_any_attempt_failure_zeroed']
total = (sum(x['model_calls'] for x in r['rows'])
         + sum(x['model_calls_attempted'] for x in failures)
         + sum(x['model_calls'] for x in r['preparation']['stages'])
         + len(r['resume_sources']) + 1 + 1)  # 正式预检及独立 HTTP 预检
assert total == ledger['total_new_generate_calls'] == 3699
for name, expected in ledger['files'].items():
    data = (root / name).read_bytes()
    assert hashlib.sha256(data).hexdigest() == expected['sha256']
print('840 complete; 470/839 retained; ledger=3699')
PY
```

独立复算准备状态的存续核查，只读虚构题集和编号状态，不访问数据库：

```sh
python3 - <<'PY'
import gzip, json
from pathlib import Path

root = Path('docs/evaluations/2026-10-05-phase2_5-v2c-artifacts')
suite = json.load(open('testdata/phase2_5/doing/suite.json'))
memories = {m['id']: m for m in suite['memories']}
state = {m['memory']: m for m in json.loads(gzip.decompress(
    (root / 'prepared-membership.json.gz').read_bytes()))}
assert len(state) == len(memories) == 671
refs = sorted({ref for t in suite['tasks'] for c in t['must']
               for ref in c['evidence']})
changed, unresolved, survives = [], [], 0
for original in refs:
    current, seen = original, set()
    while current in state and state[current]['retired']:
        if current in seen:
            current = ''
            break
        seen.add(current)
        current = state[current]['replaced_by']
    if current not in state:
        unresolved.append(original)
    elif memories[current]['text'] != memories[original]['text']:
        changed.append(original)
    else:
        survives += 1
result = {'referenced': len(refs), 'same_text_survives': survives,
          'changed_terminal': changed, 'unresolved': unresolved}
assert result == json.load(open(root / 'membership-diagnostic.json'))
print(result)
PY
```

此核查证明原文仍有有效替代，不证明每题召回或使用了这些条件。主答复与 gold 的冻结日期、产品的主机日期、历史基线的不同并发、固定一次后台准备、同源双判及连接方式变化，均是比较限制。只做了上述 10 例的有限人工诊断，没有独立全量人工再判，不提交回复正文；不能由这些数字推断实际外发成功或端到端秘书性能。
