#!/usr/bin/env python3
"""Assemble hand-authored additions; never synthesize tasks or gold."""
import argparse
import collections
import hashlib
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
BASE = ROOT / "testdata/phase2_5/doing/suite.json"
ADDITIONS = ROOT / "testdata/phase2_5/doing-independent/additions.json"
SUITE = ROOT / "testdata/phase2_5/doing-independent/suite.json"
BASE_SHA = "c8d38bad30381a308722ca25c07bdf3d84b537a3464dde62323e5130ab77396b"


def assemble(base_bytes, additions):
    if hashlib.sha256(base_bytes).hexdigest() != BASE_SHA or additions["base_suite_sha256"] != BASE_SHA:
        raise ValueError("old suite changed")
    base = json.loads(base_bytes)
    if additions["synthetic"] is not True or additions["schema_version"] != 1:
        raise ValueError("synthetic additions required")
    tasks = additions["tasks"]
    counts = collections.Counter(t["category"] for t in tasks)
    expected = dict(direct_recall=4, indirect_use=4, cross_group=12, updated_fact=6, outgoing=12, irrelevant=10)
    if len(tasks) != 48 or counts != expected or len(additions["memories"]) != 130:
        raise ValueError("independent cohort sizes changed")
    memories = {m["id"]: m for m in additions["memories"]}
    task_ids = {t["id"] for t in tasks}
    if len(memories) != 130 or len(task_ids) != 48:
        raise ValueError("duplicate additions")
    for field in ("tasks", "memories"):
        if {v["id"] for v in base[field]} & {v["id"] for v in additions[field]}:
            raise ValueError("old ID collision")
    chains = additions["chains"]
    if len(chains) != 10 or len({c["id"] for c in chains}) != 10:
        raise ValueError("ten chains required")
    targets = []
    for chain in chains:
        ids = chain["memory_ids"]
        if chain["changes"] != len(ids) - 1 or chain["changes"] < 4 or len(set(ids)) != len(ids):
            raise ValueError("four modifications required")
        if not chain["task_ids"] or not set(chain["task_ids"]) <= task_ids:
            raise ValueError("chain target missing")
        targets.extend(chain["task_ids"])
        for i, mid in enumerate(ids):
            m = memories[mid]
            if i and (m.get("supersedes") != [ids[i-1]] or m["expressed_at"] <= memories[ids[i-1]]["expressed_at"]):
                raise ValueError("chain link/order invalid")
            if i < len(ids)-1 and m.get("superseded_by") != ids[i+1]:
                raise ValueError("chain successor invalid")
        for tid in chain["task_ids"]:
            task = next(t for t in tasks if t["id"] == tid)
            if ids[-1] not in {r for c in task["must"] for r in c["evidence"]}:
                raise ValueError("chain latest version unused")
    if len(set(targets)) < 10:
        raise ValueError("ten distinct chain tasks required")
    all_memories = {m["id"]: m for m in base["memories"] + additions["memories"]}
    for task in tasks:
        if task["category"] == "cross_group":
            groups = {all_memories[r]["group"] for c in task["must"] for r in c["evidence"]}
            if len(groups) < 2:
                raise ValueError("cross-group task depends on only one source group")
    return {**base, "memories": base["memories"] + additions["memories"], "tasks": base["tasks"] + tasks}


def encoded(suite):
    return (json.dumps(suite, ensure_ascii=False, indent=2) + "\n").encode()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--write", action="store_true", help="assemble literal JSON only")
    args = parser.parse_args()
    suite = assemble(BASE.read_bytes(), json.loads(ADDITIONS.read_bytes()))
    output = encoded(suite)
    if args.write:
        SUITE.write_bytes(output)
    elif SUITE.read_bytes() != output:
        raise ValueError("extended suite differs from unchanged old prefix plus additions")
    print("verified unchanged 120-task prefix; 48 independent tasks; 10 four-change chains; sha256=" + hashlib.sha256(output).hexdigest())


if __name__ == "__main__":
    main()
