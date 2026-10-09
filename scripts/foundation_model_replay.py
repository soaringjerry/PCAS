#!/usr/bin/env python3
"""Private Codex recording/replay transport for the foundation diagnostic runner.

Configuration lives in CODEX_HOME/.pcas-replay.json, outside Git. Replay never
starts a provider. Request content and output-schema order must match exactly.
The only transport exclusions are the disabled-tools scratch cwd and thread ID.
"""
import fcntl
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import threading

# Match the existing Codex adapter's maximum RPC frame size. Overflow fails the
# capture/replay explicitly, rather than truncating a model request or response.
MAX_FRAME_BYTES = 8 << 20


def digest(value, ordered=False):
    data = json.dumps(value, ensure_ascii=False, separators=(',', ':'), sort_keys=not ordered).encode()
    return hashlib.sha256(data).hexdigest()


def private_path(path):
    path = Path(path).resolve()
    if any((p / '.git').exists() for p in [path, *path.parents]):
        raise ValueError('private replay artifacts cannot be inside a repository')
    return path


def write_private(path, value):
    path = private_path(path)
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    tmp = path.with_name(path.name + '.tmp-' + str(os.getpid()) + '-' + str(threading.get_ident()))
    fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    try:
        with os.fdopen(fd, 'w') as stream:
            json.dump(value, stream, ensure_ascii=False)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(tmp, path)
    finally:
        if tmp.exists():
            tmp.unlink()


def binding(thread, turn):
    if thread.get('sandbox') != 'read-only' or thread.get('approvalPolicy') != 'never' or not thread.get('ephemeral'):
        raise ValueError('replay requires the existing ephemeral read-only text mode')
    thread = {k: v for k, v in thread.items() if k != 'cwd'}
    turn = {k: v for k, v in turn.items() if k != 'threadId'}
    # JSON object order in an output schema remains an explicit binding, even
    # though the ordinary RPC object keys are canonicalized for transport.
    schema = turn.get('outputSchema')
    return {'thread': thread, 'turn': turn, 'schema_order_sha256': digest(schema, ordered=True)}


class State:
    def __init__(self, config):
        self.path = private_path(config['state_file'])
        self.lock = self.path.with_name(self.path.name + '.lock')
        self.case_id = config['case_id']

    def change(self, action):
        self.path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        fd = os.open(self.lock, os.O_RDWR | os.O_CREAT, 0o600)
        with os.fdopen(fd, 'r+') as lock:
            fcntl.flock(lock, fcntl.LOCK_EX)
            state = json.loads(self.path.read_text()) if self.path.exists() else {
                'case_id': self.case_id, 'requests': 0, 'cursor': 0, 'completed': 0,
                'live_provider_starts': 0, 'suppressed': 0, 'errors': []}
            if state['case_id'] != self.case_id:
                raise ValueError('replay state belongs to another case')
            result = action(state)
            write_private(self.path, state)
            return result

    def error(self, code, actual=None):
        return self.change(lambda state: state['errors'].append({'code': code, 'actual_sha256': actual}))


def rpc_error(request, message):
    return {'id': request['id'], 'error': {'code': -32090, 'message': message}}


class Replay:
    def __init__(self, config, emit):
        self.config, self.emit = config, emit
        self.state = State(config)
        self.records = [json.loads(line) for line in private_path(config['record_file']).read_text().splitlines() if line.strip()]
        self.records.sort(key=lambda record: record.get('ordinal', 0))
        self.threads = {}
        self.sequence = 0
        for ordinal, record in enumerate(self.records, 1):
            if record.get('version') != 1 or record.get('ordinal') != ordinal:
                raise ValueError('recording has an unsupported format or duplicate invocation')
            if record.get('output_sha256') != digest({'result': record.get('turn_result'), 'error': record.get('turn_error'), 'events': record.get('events')}):
                raise ValueError('recorded reply fingerprint does not match its content')
            if record.get('case_id') != config['case_id'] or not record.get('complete'):
                raise ValueError('recording is incomplete or belongs to another case')
            if record.get('input_sha256') != digest(binding(record['thread'], record['turn'])):
                raise ValueError('recorded request fingerprint does not match its content')

    def handle(self, request):
        method = request.get('method')
        if 'id' not in request:
            if method != 'initialized':
                self.state.error('unexpected_notification')
            return
        params = request.get('params', {})
        if method == 'initialize':
            self.emit({'id': request['id'], 'result': {}})
        elif method == 'account/read':
            # This is an offline transport identity, not evidence of live login.
            self.emit({'id': request['id'], 'result': {'account': {'type': 'chatgpt'}}})
        elif method == 'thread/start':
            try:
                binding(params, {})
            except ValueError:
                self.state.error('unsupported_thread_mode')
                self.emit(rpc_error(request, 'Recorded replay rejected thread mode'))
                return
            self.sequence += 1
            thread_id = 'replay-thread-' + str(self.sequence)
            self.threads[thread_id] = params
            self.emit({'id': request['id'], 'result': {'thread': {'id': thread_id}}})
        elif method == 'turn/start':
            thread_id = params.get('threadId')
            if thread_id not in self.threads:
                self.state.error('unknown_thread')
                self.emit(rpc_error(request, 'Recorded replay has no matching thread'))
                return
            fingerprint = digest(binding(self.threads[thread_id], params))

            def consume(state):
                state['requests'] += 1
                index = state['cursor']
                if index >= len(self.records):
                    state['errors'].append({'code': 'missing_reply', 'actual_sha256': fingerprint})
                    write_private(self.state.path.parent / ('codex-unmatched-' + str(state['requests']) + '.json'), {'actual': binding(self.threads[thread_id], params), 'expected_input_sha256': None})
                    return None
                record = self.records[index]
                if record['input_sha256'] != fingerprint:
                    state['errors'].append({'code': 'input_mismatch', 'actual_sha256': fingerprint})
                    write_private(self.state.path.parent / ('codex-unmatched-' + str(state['requests']) + '.json'), {'actual': binding(self.threads[thread_id], params), 'expected_input_sha256': record['input_sha256'], 'recorded_ordinal': record['ordinal']})
                    return None
                state['cursor'] += 1
                state['completed'] += 1
                return record

            record = self.state.change(consume)
            if record is None:
                self.emit(rpc_error(request, 'Recorded replay has no matching reply'))
                return
            turn_id = 'replay-turn-' + str(self.sequence)
            result = dict(record.get('turn_result') or {'turn': {}})
            if record.get('turn_error'):
                self.emit({'id': request['id'], 'error': record['turn_error']})
                return
            result['turn'] = dict(result.get('turn') or {})
            result['turn']['id'] = turn_id
            self.emit({'id': request['id'], 'result': result})
            for event in record['events']:
                event = json.loads(json.dumps(event))
                event_params = event['params']
                event_params['threadId'] = thread_id
                if 'turnId' in event_params:
                    event_params['turnId'] = turn_id
                if isinstance(event_params.get('turn'), dict) and 'id' in event_params['turn']:
                    event_params['turn']['id'] = turn_id
                self.emit(event)
        elif method == 'thread/archive':
            self.threads.pop(params.get('threadId'), None)
            self.emit({'id': request['id'], 'result': {}})
        else:
            self.state.error('unexpected_rpc_method')
            self.emit(rpc_error(request, 'Recorded replay rejected RPC method'))


class Recorder:
    def __init__(self, config):
        self.config = config
        self.state = State(config)
        self.pending, self.threads = {}, {}
        self.lock = threading.Lock()
        self.record_file = private_path(config['record_file'])

    def request(self, request):
        method, params = request.get('method'), request.get('params', {})
        with self.lock:
            if method == 'thread/start':
                binding(params, {})
            if method == 'turn/start':
                thread_id = params.get('threadId')
                if thread_id not in self.threads:
                    raise ValueError('recording received an unknown thread')

                def start(state):
                    state['requests'] += 1
                    if state['errors']:
                        state['suppressed'] += 1
                        state['errors'].append({'code': 'capture_previously_failed'})
                        return None
                    if state['requests'] > self.config['call_limit']:
                        state['suppressed'] += 1
                        state['errors'].append({'code': 'capture_call_limit'})
                        return None
                    return state['requests']

                ordinal = self.state.change(start)
                if ordinal is None:
                    return False
                self.threads[thread_id]['turn'] = params
                self.threads[thread_id]['ordinal'] = ordinal
            if 'id' in request and method in ('thread/start', 'turn/start'):
                self.pending[json.dumps(request['id'])] = request
            return True

    def response(self, response):
        with self.lock:
            pending = self.pending.pop(json.dumps(response.get('id')), None) if 'id' in response else None
            if pending and pending['method'] == 'thread/start':
                if response.get('error'):
                    self.state.error('thread_start_failed')
                    return
                thread_id = response.get('result', {}).get('thread', {}).get('id')
                if not thread_id:
                    self.state.error('thread_identity_missing')
                    return
                self.threads[thread_id] = {'version': 1, 'case_id': self.config['case_id'], 'thread': pending['params'], 'events': [], 'complete': False}
            elif pending and pending['method'] == 'turn/start':
                record = self.threads[pending['params']['threadId']]
                record['turn_result'] = response.get('result')
                record['turn_error'] = response.get('error')
                if record['turn_error']:
                    self.finish(record, True)
            if response.get('method') and isinstance(response.get('params'), dict):
                record = self.threads.get(response['params'].get('threadId'))
                if record and 'turn' in record and not record['complete']:
                    record['events'].append(response)
                    if response['method'] == 'turn/completed':
                        self.finish(record, True)

    def finish(self, record, complete):
        if record['complete']:
            return
        record['complete'] = complete
        record['input_sha256'] = digest(binding(record['thread'], record['turn']))
        record['output_sha256'] = digest({'result': record.get('turn_result'), 'error': record.get('turn_error'), 'events': record.get('events')})
        self.record_file.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
        fd = os.open(self.record_file, os.O_WRONLY | os.O_APPEND | os.O_CREAT, 0o600)
        with os.fdopen(fd, 'a') as stream:
            fcntl.flock(stream, fcntl.LOCK_EX)
            stream.write(json.dumps(record, ensure_ascii=False) + '\n')
            stream.flush()
            os.fsync(stream.fileno())
        if complete:
            self.state.change(lambda state: state.update(completed=state['completed'] + 1))
        else:
            self.state.error('capture_incomplete')

    def close(self):
        with self.lock:
            for record in self.threads.values():
                if 'turn' in record and not record['complete']:
                    self.finish(record, False)


def frames(stream):
    while True:
        line = stream.readline(MAX_FRAME_BYTES + 1)
        if not line:
            return
        if len(line) > MAX_FRAME_BYTES:
            raise ValueError('RPC frame exceeds the existing adapter limit')
        yield line


def run(config, input_stream, output_stream, args):
    if config.get('mode') not in ('record', 'replay') or not config.get('case_id'):
        raise ValueError('invalid replay configuration')
    private_path(config['record_file'])
    private_path(config['state_file'])
    output_lock = threading.Lock()

    def emit(message):
        line = json.dumps(message, ensure_ascii=False).encode() + b'\n'
        with output_lock:
            output_stream.write(line)
            output_stream.flush()

    if config['mode'] == 'replay':
        if config.get('real_binary'):
            raise ValueError('offline replay cannot configure a live binary')
        replay = Replay(config, emit)
        for line in frames(input_stream):
            replay.handle(json.loads(line))
        return 0
    if not config.get('real_binary') or not isinstance(config.get('call_limit'), int) or config['call_limit'] < 1:
        raise ValueError('capture requires an explicit binary and positive call allowance')
    recorder = Recorder(config)
    child = subprocess.Popen([config['real_binary'], *args], stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    recorder.state.change(lambda state: state.update(live_provider_starts=state['live_provider_starts'] + 1))
    thread_errors = []

    def requests():
        try:
            for line in frames(input_stream):
                request = json.loads(line)
                if not recorder.request(request):
                    emit(rpc_error(request, 'Recording call allowance exhausted'))
                    continue
                child.stdin.write(line)
                child.stdin.flush()
        except Exception as error:
            thread_errors.append(error)
            recorder.state.error('capture_request_transport_failed')
            child.terminate()
        finally:
            child.stdin.close()

    reader = threading.Thread(target=requests, daemon=True)
    reader.start()
    try:
        for line in frames(child.stdout):
            response = json.loads(line)
            recorder.response(response)
            with output_lock:
                output_stream.write(line)
                output_stream.flush()
    finally:
        if child.poll() is None:
            child.terminate()
        status = child.wait()
        recorder.close()
    if thread_errors:
        raise ValueError('capture request transport failed')
    return status


def main():
    config = None
    try:
        home = private_path(os.environ['CODEX_HOME'])
        config = json.loads((home / '.pcas-replay.json').read_text())
        return run(config, sys.stdin.buffer, sys.stdout.buffer, sys.argv[1:])
    except Exception:
        # Raw input and credentials never appear on stderr or in public logs.
        if config:
            try:
                State(config).error('shim_failed')
            except Exception:
                print('foundation_model_state_write_failed', file=sys.stderr)
        print('foundation_model_transport_failed; inspect private state', file=sys.stderr)
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
