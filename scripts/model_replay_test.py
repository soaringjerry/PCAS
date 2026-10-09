#!/usr/bin/env python3
import copy
import importlib.util
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('model_replay', Path(__file__).with_name('foundation_model_replay.py'))
replay = importlib.util.module_from_spec(spec)
spec.loader.exec_module(replay)


class ModelReplayTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        root = Path(self.directory.name)
        self.config = {'mode': 'replay', 'case_id': 'fictional-case', 'record_file': str(root / 'record.jsonl'), 'state_file': str(root / 'state.json')}
        self.thread = {'model': 'fictional-model', 'cwd': '/fictional/scratch', 'sandbox': 'read-only', 'approvalPolicy': 'never', 'ephemeral': True, 'baseInstructions': 'Fictional instruction', 'developerInstructions': 'Fictional format'}
        self.turn = {'threadId': 'original-thread', 'input': [{'type': 'text', 'text': 'Fictional input'}], 'outputSchema': {'type': 'object', 'properties': {'first': {'type': 'string'}, 'second': {'type': 'string'}}, 'required': ['first', 'second']}}
        self.record = {'version': 1, 'ordinal': 1, 'case_id': self.config['case_id'], 'complete': True, 'thread': self.thread, 'turn': self.turn, 'turn_result': {'turn': {'id': 'original-turn'}}, 'turn_error': None, 'events': [
            {'method': 'thread/tokenUsage/updated', 'params': {'threadId': 'original-thread', 'tokenUsage': {'last': {'inputTokens': 123, 'outputTokens': 45}}}},
            {'method': 'item/completed', 'params': {'threadId': 'original-thread', 'item': {'type': 'agentMessage', 'text': '{"first":"a","second":"b"}'}}},
            {'method': 'turn/completed', 'params': {'threadId': 'original-thread', 'turn': {'id': 'original-turn', 'status': 'completed'}}}]}
        self.seal(self.record)
        self.save_records([self.record])

    def seal(self, record):
        record['input_sha256'] = replay.digest(replay.binding(record['thread'], record['turn']))
        record['output_sha256'] = replay.digest({'result': record['turn_result'], 'error': record['turn_error'], 'events': record['events']})

    def save_records(self, records):
        path = Path(self.config['record_file'])
        path.write_text(''.join(json.dumps(record) + '\n' for record in records))
        path.chmod(0o600)

    def start(self, thread=None):
        self.output = []
        self.machine = replay.Replay(self.config, self.output.append)
        self.machine.handle({'id': 1, 'method': 'thread/start', 'params': thread or self.thread})
        return self.output[-1]['result']['thread']['id']

    def request_turn(self, thread_id, turn=None):
        params = copy.deepcopy(turn or self.turn)
        params['threadId'] = thread_id
        self.machine.handle({'id': 2, 'method': 'turn/start', 'params': params})

    def state(self):
        return json.loads(Path(self.config['state_file']).read_text())

    def test_exact_reply_and_usage_without_starting_live_provider(self):
        requests = [{'id': 0, 'method': 'initialize'}, {'id': 1, 'method': 'thread/start', 'params': self.thread}, {'id': 2, 'method': 'turn/start', 'params': dict(self.turn, threadId='replay-thread-1')}]
        output = io.BytesIO()
        with patch.object(replay.subprocess, 'Popen', side_effect=AssertionError('live process must not start')):
            replay.run(self.config, io.BytesIO(b''.join(json.dumps(r).encode() + b'\n' for r in requests)), output, [])
        messages = [json.loads(line) for line in output.getvalue().splitlines()]
        self.assertEqual(messages[-2]['params']['item']['text'], self.record['events'][1]['params']['item']['text'])
        self.assertEqual(messages[-3]['params']['tokenUsage']['last'], {'inputTokens': 123, 'outputTokens': 45})
        self.assertEqual(self.state()['live_provider_starts'], 0)
        self.assertEqual(self.state()['completed'], 1)

    def test_changed_instructions_context_model_and_schema_order_are_rejected(self):
        for kind in ('instructions', 'context', 'model', 'schema_order'):
            with self.subTest(kind=kind):
                state = Path(self.config['state_file'])
                if state.exists():
                    state.unlink()
                thread, turn = copy.deepcopy(self.thread), copy.deepcopy(self.turn)
                if kind == 'instructions':
                    thread['baseInstructions'] += ' Changed'
                elif kind == 'context':
                    turn['input'][0]['text'] += ' Changed'
                elif kind == 'model':
                    thread['model'] = 'different-model'
                else:
                    turn['outputSchema']['properties'] = {'second': {'type': 'string'}, 'first': {'type': 'string'}}
                thread_id = self.start(thread)
                self.request_turn(thread_id, turn)
                self.assertIn('error', self.output[-1])
                self.assertEqual(self.state()['cursor'], 0)
                self.assertEqual(self.state()['errors'][0]['code'], 'input_mismatch')

    def test_scratch_path_is_transport_metadata_but_tool_mode_is_bound(self):
        thread = dict(self.thread, cwd='/another/private/scratch')
        thread_id = self.start(thread)
        self.request_turn(thread_id)
        self.assertEqual(self.state()['completed'], 1)
        thread['sandbox'] = 'workspace-write'
        self.machine.handle({'id': 3, 'method': 'thread/start', 'params': thread})
        self.assertIn('error', self.output[-1])

    def test_missing_or_duplicate_reply_is_visible_and_never_reused(self):
        thread_id = self.start()
        self.request_turn(thread_id)
        self.request_turn(thread_id)
        self.assertIn('error', self.output[-1])
        self.assertEqual(self.state()['completed'], 1)
        self.assertEqual(self.state()['errors'][0]['code'], 'missing_reply')

    def test_tampered_output_and_incomplete_recording_are_rejected(self):
        for change in ('output', 'incomplete', 'duplicate'):
            record = copy.deepcopy(self.record)
            if change == 'output':
                record['events'][1]['params']['item']['text'] = 'Changed output'
            elif change == 'incomplete':
                record['complete'] = False
            self.save_records([record, record] if change == 'duplicate' else [record])
            with self.assertRaises(ValueError):
                replay.Replay(self.config, lambda m: None)

    def test_live_binary_configuration_is_refused_in_replay(self):
        with self.assertRaises(ValueError):
            replay.run(dict(self.config, real_binary='/not/a/replay/binary'), io.BytesIO(), io.BytesIO(), [])

    def test_recorder_omits_account_credentials_and_preserves_terminal_failure(self):
        config = dict(self.config, mode='record', call_limit=1)
        Path(config['record_file']).unlink()
        recorder = replay.Recorder(config)
        recorder.request({'id': 0, 'method': 'account/read'})
        recorder.response({'id': 0, 'result': {'account': {'type': 'chatgpt', 'email': 'private@example.invalid', 'accessToken': 'secret'}}})
        recorder.request({'id': 1, 'method': 'thread/start', 'params': self.thread})
        recorder.response({'id': 1, 'result': {'thread': {'id': 'original-thread'}}})
        self.assertTrue(recorder.request({'id': 2, 'method': 'turn/start', 'params': self.turn}))
        failure = {'code': -32000, 'message': 'Fictional provider error'}
        recorder.response({'id': 2, 'error': failure})
        text = Path(config['record_file']).read_text()
        self.assertNotIn('private@example.invalid', text)
        self.assertNotIn('accessToken', text)
        self.assertEqual(json.loads(text)['turn_error'], failure)
        self.assertFalse(recorder.request({'id': 3, 'method': 'turn/start', 'params': self.turn}))
        self.assertEqual(self.state()['suppressed'], 1)
        self.assertEqual(self.state()['errors'][0]['code'], 'capture_call_limit')
        self.assertEqual(os.stat(config['record_file']).st_mode & 0o777, 0o600)

    def test_interrupted_capture_remains_incomplete(self):
        config = dict(self.config, mode='record', call_limit=1)
        Path(config['record_file']).unlink()
        recorder = replay.Recorder(config)
        recorder.request({'id': 1, 'method': 'thread/start', 'params': self.thread})
        recorder.response({'id': 1, 'result': {'thread': {'id': 'original-thread'}}})
        recorder.request({'id': 2, 'method': 'turn/start', 'params': self.turn})
        recorder.close()
        self.assertFalse(json.loads(Path(config['record_file']).read_text())['complete'])
        self.assertEqual(self.state()['errors'][0]['code'], 'capture_incomplete')


if __name__ == '__main__':
    unittest.main()
