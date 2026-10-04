#!/usr/bin/env python3
"""Synthetic-only tests; never invokes export, psql or a model."""
import hashlib
import importlib.util
import json
from pathlib import Path
import unittest
import sys
sys.dont_write_bytecode = True

spec = importlib.util.spec_from_file_location('workflow', Path(__file__).with_name('p25-v2-private.py'))
workflow = importlib.util.module_from_spec(spec)
spec.loader.exec_module(workflow)


class ReviewGate(unittest.TestCase):
    def setUp(self):
        source = json.loads((Path(__file__).resolve().parents[1] / 'testdata/phase2_5/doing/suite.json').read_text())
        source["synthetic"] = False
        self.source = source
        self.digest = hashlib.sha256(json.dumps(source, ensure_ascii=False, sort_keys=True,
                                                separators=(',', ':')).encode()).hexdigest()

    def decisions(self, rows):
        return dict(source_sha256=self.digest, decisions=rows)

    def test_pending_and_rejected_do_not_enter_eval(self):
        tasks = self.source['tasks']
        rows = [dict(id=tasks[0]['id'], state='right'), dict(id=tasks[1]['id'], state='pending'),
                dict(id=tasks[2]['id'], state='wrong')]
        approved = workflow.validate_approved(self.source, self.decisions(rows))
        self.assertEqual(len(approved['tasks']), 1)
        self.assertTrue(approved['tasks'][0]['reviewed'])
        self.assertFalse(approved['synthetic'])

    def test_invalid_snapshot_or_reference_rejected(self):
        task = json.loads(json.dumps(self.source['tasks'][0]))
        task['must'][0]['evidence'] = ['nonexistent']
        with self.assertRaises(ValueError):
            workflow.validate_approved(self.source, self.decisions([dict(id=task['id'], state='edit', edited_task=task)]))
        with self.assertRaises(ValueError):
            workflow.validate_approved(self.source, dict(source_sha256='different', decisions=[]))
        with self.assertRaises(ValueError):
            workflow.validate_approved(self.source, self.decisions([]))

    def test_repository_and_symlink_paths_are_rejected(self):
        with self.assertRaises(ValueError):
            workflow.outside_git(Path(__file__).parent / 'private.json')
        # resolve() includes symlinked parents; no need to touch online paths.
        self.assertEqual(workflow.outside_git('/var/tmp/pcas-v2-private/proposals.json'),
                         Path('/var/tmp/pcas-v2-private/proposals.json'))


if __name__ == '__main__':
    unittest.main()
