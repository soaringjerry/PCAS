#!/usr/bin/env python3
"""Check the live secretary and deputy on the real default channel after a release.

Four check-only turns in one smoke group: an answer, an arrangement, a change,
and a delegation. The delegated run is awaited before the group is cleaned.
The group is cleaned by its own smoke id. Nothing is selected by time.
Replies are printed; credentials are not.
"""
import json, subprocess, sys, time, urllib.error, urllib.request, uuid

DOCKER = ['docker', '--host=unix:///var/run/docker.sock']
BASE = 'http://127.0.0.1:12352'
inspection = json.loads(subprocess.check_output(DOCKER + ['inspect', 'pcas-api-1']))[0]
env = dict(v.split('=', 1) for v in inspection['Config']['Env'] if '=' in v)
owner, token = str(uuid.UUID(env['PCAS_OWNER_ID'])), env['PCAS_API_TOKEN']
revision = inspection['Config']['Image'].split('release-')[1]
if len(sys.argv) > 1:
    assert revision == sys.argv[1], 'live revision is %s' % revision


def sql(query):
    p = subprocess.run(DOCKER + ['exec', 'pcas-db-1', 'psql', '-U', 'pcas', '-d', 'pcas', '-At', '-v', 'ON_ERROR_STOP=1', '-c', query], text=True, capture_output=True)
    if p.returncode:
        raise RuntimeError('live database check failed: ' + p.stderr[:300])
    return json.loads(p.stdout.strip())


def counts():
    return sql("SELECT json_build_object('claims',(SELECT count(*) FROM claims WHERE owner_id='%s'),'sources',(SELECT count(*) FROM sources WHERE owner_id='%s'),'workItems',(SELECT count(*) FROM work_items WHERE owner_id='%s'))" % (owner, owner, owner))


def call(method, path, body=None):
    data = None if body is None else json.dumps(body, ensure_ascii=False).encode()
    request = urllib.request.Request(BASE + path, data=data, method=method, headers={'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'})
    try:
        with urllib.request.urlopen(request, timeout=180) as response:
            return json.load(response)
    except urllib.error.HTTPError as e:
        raise RuntimeError('HTTP %s on %s: %s' % (e.code, path, e.read().decode(errors='replace')[:400])) from None


smoke = str(uuid.uuid4())
report = {'revision': revision, 'smokeId': smoke, 'before': counts(), 'turns': []}
failures = []
models = call('GET', '/v1/models')
default = next((p for p in models['providers'] if p['id'] == 'chatgpt'), None)
if not default or default['protocol'] != 'codex' or not default['available']:
    failures.append('the default Codex channel is not available')
conversation = None
try:
    for label, text in [
        ('answer', '这是一次部署检查。请只回复“运行正常”，不要修改任何记录。'),
        ('arrangement', '部署检查：帮我建一个待办，标题叫“部署检查待办甲”，截止明天下午三点。'),
        ('change', '部署检查：把“部署检查待办甲”改到后天下午三点。'),
        ('delegation', '部署检查：把“部署检查待办甲”交给副手，拆成三个步骤。'),
    ]:
        request_id = str(uuid.uuid4())
        body = {'smokeId': smoke, 'requestId': request_id, 'text': text, 'agentId': ''}
        if conversation:
            body['conversationId'] = conversation
        started = time.monotonic()
        out = call('POST', '/v1/desk/turn', body)
        conversation = out.get('conversationId', conversation)
        turn = out['turn']
        calls = sql("SELECT coalesce(json_agg(json_build_object('function',function_name,'stage',stage,'provider',provider_id,'outcome',outcome,'accounting',accounting_state,'error',error_code,'fallback',actual_mode->'unsupportedFallback','schema',schema_name) ORDER BY created_at),'[]') FROM model_calls WHERE owner_id='%s' AND (execution_id='%s' OR root_execution_id='%s' OR causation_id='%s')" % (owner, request_id, request_id, request_id))
        entry = {'label': label, 'seconds': round(time.monotonic() - started, 1), 'reply': turn.get('reply'), 'receipts': [{k: r.get(k) for k in ('op', 'status', 'text')} for r in turn.get('receipts') or []], 'notices': out.get('notices'), 'calls': calls}
        report['turns'].append(entry)
        answers = [c for c in calls if c['stage'] == 'answer']
        if len(answers) != 1 or answers[0]['provider'] != 'chatgpt' or answers[0]['outcome'] != 'returned' or answers[0]['accounting'] != 'settled' or answers[0]['fallback']:
            failures.append(label + ': the answer call is not one settled Codex call without fallback')
        if any(c['outcome'] in ('prepared', 'started') or c['accounting'] in ('pending', 'failed') for c in calls):
            failures.append(label + ': a call is unfinished')
        if any(r.get('status') == 'failed' for r in turn.get('receipts') or []):
            failures.append(label + ': a receipt failed')
        if label == 'answer' and (turn.get('reply') or '').strip().strip('。.!！') != '运行正常':
            failures.append('answer: unexpected reply')
        if label == 'arrangement' and not any(r.get('op') == 'create_task' for r in turn.get('receipts') or []):
            failures.append('arrangement: no task was created')
        if label == 'change' and not any(r.get('op') in ('update_task', 'reschedule_task', 'update') or 'task' in (r.get('op') or '') for r in turn.get('receipts') or []):
            failures.append('change: no task change receipt')
        if label == 'delegation':
            if not any(r.get('op') == 'delegate' and r.get('status') == 'done' for r in turn.get('receipts') or []):
                failures.append('delegation: no run was requested')
            else:
                # The worker runs the deputy. Wait for the run before the cleanup.
                runs_query = "SELECT coalesce(json_agg(json_build_object('id',r.id::text,'status',r.status)),'[]') FROM agent_runs r JOIN desk_smoke_changes c ON (c.owner_id,c.record_id)=(r.owner_id,r.id) WHERE c.owner_id='%s' AND c.smoke_id='%s' AND c.table_name='agent_runs'" % (owner, smoke)
                deadline = time.monotonic() + 240
                runs = sql(runs_query)
                while time.monotonic() < deadline and (not runs or any(r['status'] in ('queued', 'waiting', 'running') for r in runs)):
                    time.sleep(5)
                    runs = sql(runs_query)
                entry['runs'] = [r['status'] for r in runs]
                entry['runSeconds'] = round(time.monotonic() - started, 1)
                if len(runs) != 1 or runs[0]['status'] != 'done':
                    failures.append('delegation: the run did not finish as done')
                else:
                    deputy = sql("SELECT coalesce(json_agg(json_build_object('function',function_name,'stage',stage,'provider',provider_id,'outcome',outcome,'accounting',accounting_state,'error',error_code) ORDER BY created_at),'[]') FROM model_calls WHERE owner_id='%s' AND root_execution_id='%s'" % (owner, runs[0]['id']))
                    entry['deputyCalls'] = deputy
                    answers = [c for c in deputy if c['function'] == 'deputy' and c['stage'] == 'answer']
                    if not answers or any(c['outcome'] != 'returned' or c['accounting'] != 'settled' for c in answers):
                        failures.append('delegation: the deputy call is not returned and settled')
                    if any(c['outcome'] in ('prepared', 'started') or c['accounting'] in ('pending', 'failed') for c in deputy):
                        failures.append('delegation: a deputy call is unfinished')
finally:
    try:
        report['cleanup'] = call('DELETE', '/v1/desk/smoke/' + smoke)
    except Exception as e:
        report['cleanupError'] = str(e)
        failures.append('cleanup failed')
    report['after'] = counts()
if report['before'] != report['after']:
    failures.append('record counts differ after cleanup')
report['failures'] = failures
print(json.dumps(report, ensure_ascii=False, indent=1))
sys.exit(1 if failures else 0)
