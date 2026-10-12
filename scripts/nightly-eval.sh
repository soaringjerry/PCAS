#!/usr/bin/env bash
# Nightly real-model evaluation on this host's Codex channel.
#
# It runs half of the synthetic doing suite each night (the other half the
# next night) with the product's current recall route, one repetition.
# The spending limit is one US dollar for one night. The plan is checked
# against the limit before a call is made, and the report is checked after.
# A night without a dedicated signed-in Codex home makes no call.
set -euo pipefail
repo_dir="$(cd -- "$(dirname -- "$0")/.." && pwd)"
home="${PCAS_EVAL_CODEX_HOME:-/root/PCAS-private/codex-eval}"
out_root="${PCAS_NIGHTLY_OUTPUT:-/var/tmp/pcas-nightly}"
limit_usd="${PCAS_NIGHTLY_LIMIT_USD:-1}"
suite="$repo_dir/testdata/phase2_5/doing/suite.json"
day="$(date -u +%Y-%m-%d)"
out="$out_root/$day"
mkdir -p "$out"
log() { printf '%s %s\n' "$(date -u +%FT%TZ)" "$*" | tee -a "$out_root/nightly.log"; }

# PCAS_NIGHTLY_FAKE=1 rehearses the whole script with the fake model. It makes
# no real call and its scores mean nothing.
channel=(-channel=codex -codex-home="$home")
if [[ "${PCAS_NIGHTLY_FAKE:-}" == 1 ]]; then
  channel=(-fake)
elif [[ ! -f "$home/auth.json" ]]; then
  log "skipped: no dedicated signed-in Codex home at $home; sign in once with CODEX_HOME=$home codex login"
  exit 2
fi

# The plan: the tasks of this night, and their cost at the list price of the
# model (1 USD for one million input tokens, 4 USD for one million output
# tokens). One character counts as one token, which is above the real count.
# One task is one answer call and two judge calls. The sizes are the largest
# values of the dry run of 2026-10-12 with the fake model, rounded up: 3,600
# input characters for an answer and 600 output characters (the answer
# limit). The size of one judgment is an estimate, not a measurement: 1,800
# input and 200 output characters.
plan="$(python3 - "$suite" "$limit_usd" <<'PY'
import json, sys, datetime
suite, limit = json.load(open(sys.argv[1])), float(sys.argv[2])
ids = [t["id"] for t in suite["tasks"]]
half = datetime.datetime.now(datetime.timezone.utc).timetuple().tm_yday % 2
tonight = [t for i, t in enumerate(ids) if i % 2 == half]
per_task = (3600 + 2 * 1800) * 1e-6 * 1 + (600 + 2 * 200) * 1e-6 * 4
while tonight and len(tonight) * per_task > limit:
    tonight.pop()
print(json.dumps({"tasks": tonight, "planned_usd": round(len(tonight) * per_task, 4), "left_out": len(ids) // 2 - len(tonight)}))
PY
)"
tasks="$(python3 -c 'import json,sys; print(",".join(json.loads(sys.argv[1])["tasks"]))' "$plan")"
log "plan: $(python3 -c 'import json,sys; p=json.loads(sys.argv[1]); print("tasks=%d planned_usd=%s left_out_for_the_limit=%d" % (len(p["tasks"]), p["planned_usd"], p["left_out"]))' "$plan")"
[[ -n "$tasks" ]] || { log "failed: the limit permits no task"; exit 3; }

cd "$repo_dir"
if ! scripts/p25-v2-run.sh "${channel[@]}" -repeats=1 -workers=2 -methods=current -tasks="$tasks" > "$out/run.log" 2>&1; then
  log "failed: the evaluation run did not finish; see $out/run.log"
  exit 1
fi
report="$(sed -n 's/^metrics-only report: \(.*\)\/report\.{json,md}$/\1/p' "$out/run.log" | tail -1)"
cp "$report/report.json" "$report/report.md" "$out/"
python3 - "$out/report.json" "$limit_usd" <<'PY' | tee -a "$out_root/nightly.log"
import json, sys
r, limit = json.load(open(sys.argv[1])), float(sys.argv[2])
rows = r["rows"]
calls = sum(x["model_calls"] for x in rows)
inputs = sum(x["answer_input_chars"] + 2 * 1800 for x in rows)
outputs = sum(x["answer_chars"] + 2 * 200 for x in rows)
usd = inputs * 1e-6 * 1 + outputs * 1e-6 * 4
must = sum(x["must_pass_both"] for x in rows) / max(1, sum(x["must_total"] for x in rows))
usable = sum(1 for x in rows if x["usable"]) / max(1, len(rows))
print("result: revision=%s tasks=%d calls=%d estimated_usd=%.4f must=%.1f%% usable=%.1f%%" % (r["revision"][:7], len(rows), calls, usd, 100 * must, 100 * usable))
if usd > limit:
    print("failed: the estimated cost is above the limit of %.2f USD" % limit)
    sys.exit(3)
PY
