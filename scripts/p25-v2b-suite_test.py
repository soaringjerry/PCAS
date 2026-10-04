#!/usr/bin/env python3
import copy
import importlib.util
import json
import unittest
from pathlib import Path

spec = importlib.util.spec_from_file_location("v2b_suite", Path(__file__).with_name("p25-v2b-suite.py"))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class AssemblyTest(unittest.TestCase):
    def setUp(self):
        self.base = m.BASE.read_bytes()
        self.additions = json.loads(m.ADDITIONS.read_bytes())

    def test_literal_assembly_and_unique_requests(self):
        s = m.assemble(self.base, self.additions)
        self.assertEqual(m.encoded(s), m.SUITE.read_bytes())
        old = json.loads(self.base)
        self.assertEqual(old["tasks"], s["tasks"][:120])
        self.assertEqual(old["memories"], s["memories"][:671])
        self.assertEqual(48, len({t["request"] for t in self.additions["tasks"]}))

    def test_old_mutation_rejected(self):
        with self.assertRaisesRegex(ValueError, "old suite changed"):
            m.assemble(self.base + b" ", self.additions)

    def test_broken_chains_and_coverage_rejected(self):
        variants = []
        a = copy.deepcopy(self.additions)
        a["chains"][0]["changes"] = 3
        variants.append(a)
        a = copy.deepcopy(self.additions)
        a["chains"][0]["task_ids"] = a["chains"][1]["task_ids"]
        variants.append(a)
        a = copy.deepcopy(self.additions)
        a["tasks"][0]["category"] = "cross_group"
        variants.append(a)
        a = copy.deepcopy(self.additions)
        a["memories"][1]["supersedes"] = []
        variants.append(a)
        a = copy.deepcopy(self.additions)
        for mem in a["memories"]:
            if mem["id"] in ("I-C07a", "I-C07b"):
                mem["group"] = "single"
        variants.append(a)
        for a in variants:
            with self.assertRaises(ValueError):
                m.assemble(self.base, a)


if __name__ == "__main__":
    unittest.main()
