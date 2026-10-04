#!/usr/bin/env python3
"""Coordinator-only real-data steps. No automatic database discovery or execution.

All files are private and outside Git. Use export only after explicit user consent.
The evaluator refuses any private task that has not been approved individually.
"""
import argparse
import datetime as dt
import html
import json
import os
from pathlib import Path
import subprocess
import sys
import uuid

class WorkflowError(ValueError):
    pass


DEFAULT = Path('/var/tmp/pcas-v2-private')


def outside_git(path):
    resolved = Path(path).expanduser().resolve()
    for parent in [resolved, *resolved.parents]:
        if (parent / '.git').exists():
            raise WorkflowError('private files must stay outside every Git repository')
    return resolved


def write_private(path, content):
    path = outside_git(path)
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, 'w', encoding='utf-8') as stream:
        stream.write(content)
    os.chmod(path, 0o600)


def load_private(path):
    return json.loads(outside_git(path).read_text(encoding='utf-8'))


def export(args):
    if not args.consent_confirmed:
        raise WorkflowError('coordinator must obtain user consent before export')
    owner = str(uuid.UUID(args.owner))
    dsn = os.environ.get('PCAS_V2_READONLY_DSN')
    if not dsn:
        raise WorkflowError('set PCAS_V2_READONLY_DSN using a read-only login')
    # Enforce read-only again at session AND transaction levels. One MVCC
    # snapshot; no pg_dump, Docker access, schema changes, writes or model calls.
    sql = r"""
BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;
SELECT COALESCE(jsonb_agg(jsonb_build_object(
 'id', 'M' || row_number, 'expressed_at', expressed_at,
 'group', 'exported-memory', 'text', body, 'tags', jsonb_build_array('private-export')
) ORDER BY expressed_at, id), '[]'::jsonb)
FROM (
 SELECT t.id, t.body,
 to_char(COALESCE(v.expressed_at,v.recorded_at) AT TIME ZONE 'UTC',
         'YYYY-MM-DD"T"HH24:MI:SS"Z"') AS expressed_at,
 row_number() OVER (ORDER BY COALESCE(v.expressed_at,v.recorded_at),t.id) AS row_number
 FROM memory_records r
 JOIN memory_text t ON (t.owner_id,t.id,t.version)=(r.owner_id,r.id,r.version)
 JOIN record_versions v ON (v.owner_id,v.record_id,v.version)=(r.owner_id,r.id,r.version)
 WHERE r.owner_id=:'owner'::uuid AND r.kind='claim' AND r.state='active' AND v.state='active'
) active_memories;
ROLLBACK;
"""
    env = dict(os.environ)
    env['PGDATABASE'] = dsn  # Never place credentials on command line or in logs.
    env['PGOPTIONS'] = '-c default_transaction_read_only=on -c statement_timeout=60000'
    proc = subprocess.run(['psql', '-X', '-q', '-t', '-A', '-v', 'ON_ERROR_STOP=1',
                           '-v', 'owner=' + owner], input=sql, text=True,
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env)
    if proc.returncode:
        raise WorkflowError('read-only export failed; check login, schema and owner locally')
    memories = json.loads(proc.stdout)
    suite = dict(schema_version=1, synthetic=False, persona='private-export', timezone='UTC',
                 as_of=dt.datetime.now(dt.timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ'),
                 memories=memories, tasks=[])
    write_private(args.output, json.dumps(suite, ensure_ascii=False, indent=2) + '\n')
    print('exported_memory_count=' + str(len(memories)))


def validate_approved(source, decisions):
    """Accept only explicit human decisions, bind them to the candidate snapshot."""
    from hashlib import sha256
    fingerprint = sha256(json.dumps(source, ensure_ascii=False, sort_keys=True,
                                    separators=(',', ':')).encode()).hexdigest()
    if decisions.get('source_sha256') != fingerprint:
        raise WorkflowError('review decisions belong to a different candidate snapshot')
    if source.get('synthetic') or not source.get('tasks'):
        raise WorkflowError('expected private candidate suite')
    memory_ids = {m['id'] for m in source['memories']}
    candidates = {t['id']: t for t in source['tasks']}
    seen = set()
    approved = []
    categories = {'direct_recall', 'indirect_use', 'cross_group', 'updated_fact', 'outgoing', 'irrelevant'}
    for row in decisions.get('decisions', []):
        tid = row.get('id')
        if tid not in candidates or tid in seen:
            raise WorkflowError('unknown or repeated review task')
        seen.add(tid)
        state = row.get('state')
        if state not in ('right', 'wrong', 'edit', 'pending'):
            raise WorkflowError('unknown review decision')
        if state not in ('right', 'edit'):
            continue
        task = dict(candidates[tid] if state == 'right' else row.get('edited_task', {}))
        if task.get('id') != tid or task.get('category') not in categories:
            raise WorkflowError('edited task changed identity or has invalid category')
        if not task.get('request') or not task.get('reasonable_handling'):
            raise WorkflowError('edited task missing request or handling')
        if not isinstance(task.get('can_complete_without_followup'), bool):
            raise WorkflowError('edited task missing completion judgment')
        if not 3 <= len(task.get('must', [])) <= 6 or not task.get('forbidden'):
            raise WorkflowError('edited task has invalid rubric counts')
        check_ids = set()
        for group in ('must', 'bonus', 'forbidden'):
            for check in task.get(group, []):
                if not check.get('id') or check['id'] in check_ids or not check.get('text'):
                    raise WorkflowError('edited task has invalid check')
                check_ids.add(check['id'])
                refs = check.get('evidence')
                if not isinstance(refs, list) or any(ref not in memory_ids for ref in refs):
                    raise WorkflowError('edited task has invalid evidence')
                if not refs and task['category'] != 'irrelevant':
                    raise WorkflowError('edited task has no memory evidence')
        task['reviewed'] = True
        approved.append(task)
    if not approved:
        raise WorkflowError('no explicitly reviewed tasks approved')
    result = dict(source)
    result['tasks'] = approved
    result['synthetic'] = False
    return result


def review(args):
    from hashlib import sha256
    source = load_private(args.input)
    if source.get('synthetic') or not source.get('tasks'):
        raise WorkflowError('expected private candidate suite')
    digest = sha256(json.dumps(source, ensure_ascii=False, sort_keys=True,
                              separators=(',', ':')).encode()).hexdigest()
    memory = {m['id']: m['text'] for m in source['memories']}
    sections = []
    for t in source['tasks']:
        details = []
        for group, label in [('must', '必须'), ('bonus', '加分'), ('forbidden', '不许')]:
            for c in t[group]:
                evidence = '\n'.join(ref + '：' + memory[ref] for ref in c['evidence']) or '依据：本题请求'
                details.append('<li><b>' + label + ' ' + html.escape(c['id']) + '</b>：' +
                               html.escape(c['text']) + '<pre>' + html.escape(evidence) + '</pre></li>')
        tid = html.escape(t['id'], quote=True)
        options = ''.join('<label><input type="radio" name="' + tid + '" value="' + value + '"' +
                          (' checked' if value == 'pending' else '') + '>' + label + '</label>'
                          for value, label in [('pending', '未核对'), ('right', '对'), ('wrong', '不对'), ('edit', '改成…')])
        sections.append('<section data-id="' + tid + '"><h2>' + tid + ' · ' + html.escape(t['category']) +
                        '</h2><p>' + html.escape(t['request']) + '</p><ul>' + ''.join(details) +
                        '</ul><p>能否不追问完成：' + ('能' if t['can_complete_without_followup'] else '不能') +
                        '；合理做法：' + html.escape(t['reasonable_handling']) + '</p>' + options +
                        '<details><summary>修改这道题的JSON（保留编号，改请求/标准/依据）</summary><textarea>' +
                        html.escape(json.dumps(t, ensure_ascii=False, indent=2)) + '</textarea></details></section>')
    payload = json.dumps(dict(source_sha256=digest), ensure_ascii=False).replace('<', '\\u003c')
    page = '''<!doctype html><meta charset="utf-8"><title>私有评测抽查表</title>
<style>body{font:16px sans-serif;max-width:1000px;margin:30px auto}section{border-top:1px solid #bbb;padding:20px 0}pre{white-space:pre-wrap;background:#eee;padding:10px}textarea{width:100%;height:400px}label{margin-right:20px}button{padding:10px;position:sticky;top:0}</style>
<h1>私有评测抽查表</h1><p>请逐题核对请求、必须、不许和依据原文。默认未核对。只有“对”或明确“改成…”的题进入评测。此文件包含私有原文，请留在本机；页面不加载远程资源。</p>
<button id="save">下载核对决定（review-decisions.json）</button>''' + ''.join(sections) + '''
<script>
const header = ''' + payload + ''';
document.getElementById('save').onclick = () => {
 const decisions = [];
 try {
 for (const section of document.querySelectorAll('section')) {
  const state = section.querySelector('input:checked').value;
  const row = {id:section.dataset.id,state};
  if (state === 'edit') row.edited_task = JSON.parse(section.querySelector('textarea').value);
  decisions.push(row);
 }
 const blob = new Blob([JSON.stringify({...header,decisions},null,2)], {type:'application/json'});
 const a = document.createElement('a'); a.href=URL.createObjectURL(blob); a.download='review-decisions.json'; a.click();
 } catch (e) {alert('修改后的JSON格式不正确，请检查。');}
};
</script>'''
    write_private(args.output, page)
    print('review_task_count=' + str(len(source['tasks'])))


def approve(args):
    result = validate_approved(load_private(args.input), load_private(args.decisions))
    write_private(args.output, json.dumps(result, ensure_ascii=False, indent=2) + '\n')
    print('approved_task_count=' + str(len(result['tasks'])))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    export_parser = commands.add_parser('export', help='ONLY coordinator, AFTER user consent')
    export_parser.add_argument('--consent-confirmed', action='store_true')
    export_parser.add_argument('--owner', required=True)
    export_parser.add_argument('--output', default=DEFAULT / 'export.json', type=Path)
    export_parser.set_defaults(handler=export)
    for name, handler in [('review', review), ('approve', approve)]:
        sub = commands.add_parser(name)
        sub.add_argument('--input', default=DEFAULT / 'proposals.json', type=Path)
        sub.add_argument('--output', default=DEFAULT / ('review.html' if name == 'review' else 'approved.json'), type=Path)
        if name == 'approve':
            sub.add_argument('--decisions', default=DEFAULT / 'review-decisions.json', type=Path)
        sub.set_defaults(handler=handler)
    args = parser.parse_args()
    try:
        args.handler(args)
    except WorkflowError as error:
        print('private workflow failed: ' + str(error), file=sys.stderr)
        return 1
    except (ValueError, KeyError, TypeError, OSError, json.JSONDecodeError):
        # Never echo PostgreSQL errors, user input, paths, requests or memory text.
        print('private workflow failed: check consent, file location, review integrity and required fields locally', file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
