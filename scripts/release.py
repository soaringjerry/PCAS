#!/usr/bin/env python3
"""Release one main revision to the live Compose deployment.

  release.py prepare <sha>   export source, write compose files, build the image
  release.py deploy  <sha>   stop, back up, migrate, start, check readiness

Deploy rolls back to the earlier release when a step after the stop fails.
Nothing here prints credentials or database content.
"""
import datetime, hashlib, json, os, shutil, subprocess, sys, time, urllib.request, uuid
from pathlib import Path

DOCKER = ['docker', '--host=unix:///var/run/docker.sock']
RELEASES = Path('/root/PCAS-deploy/releases')
REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BASE = 'http://127.0.0.1:12352'


def out(cmd, **kw):
    return subprocess.check_output(cmd, text=True, **kw)


def run(cmd, **kw):
    subprocess.run(cmd, check=True, **kw)


def live():
    return json.loads(out(DOCKER + ['inspect', 'pcas-api-1', 'pcas-worker-1']))


def live_revision():
    images = {c['Config']['Image'] for c in live()}
    assert len(images) == 1, images
    return images.pop().split('release-')[1]


def sql(query):
    return json.loads(out(DOCKER + ['exec', 'pcas-db-1', 'psql', '-U', 'pcas', '-d', 'pcas', '-At', '-v', 'ON_ERROR_STOP=1', '-c', query]).strip())


def prepare(sha):
    assert out(['git', 'ls-remote', 'origin', 'refs/heads/main'], cwd=REPO).split()[0] == sha, 'main is not this revision'
    old = live_revision()
    release = RELEASES / sha
    source = release / 'source'
    if not source.exists():
        source.mkdir(parents=True)
        archive = subprocess.Popen(['git', 'archive', sha], cwd=REPO, stdout=subprocess.PIPE)
        run(['tar', '-x', '-C', str(source)], stdin=archive.stdout)
        assert archive.wait() == 0
    (release / 'SOURCE_COMMIT').write_text(sha + '\n')
    deploy = (RELEASES / old / 'compose.deploy.yml').read_text().replace(old, sha)
    (release / 'compose.deploy.yml').write_text(deploy)
    script = (RELEASES / old / 'compose.sh').read_text().replace(old, sha)
    (release / 'compose.sh').write_text(script)
    (release / 'compose.sh').chmod(0o755)
    run([str(release / 'compose.sh'), 'build', 'api'])
    image = json.loads(out(DOCKER + ['image', 'inspect', 'pcas-memory:release-' + sha]))[0]
    assert image['Config']['Labels']['org.opencontainers.image.revision'] == sha
    print(json.dumps({'prepared': sha, 'live': old}))


def backup(sha, owner):
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime('%Y%m%dT%H%M%SZ')
    target = Path('/root/pcas-backups') / ('foundation-review-%s-%s-%s' % (sha[:7], stamp, uuid.uuid4().hex[:8]))
    target.mkdir(mode=0o700)
    dump = target / 'database.dump'
    with dump.open('wb') as f:
        os.chmod(dump, 0o600)
        run(DOCKER + ['exec', 'pcas-db-1', 'pg_dump', '-U', 'pcas', '-d', 'pcas', '-Fc'], stdout=f)
    assert dump.stat().st_size > 0, 'empty database backup'
    with dump.open('rb') as data, (target / 'restore-list.txt').open('wb') as listing:
        run(DOCKER + ['exec', '-i', 'pcas-db-1', 'pg_restore', '--list'], stdin=data, stdout=listing)
    inspection = live()[0]
    mount = next(m['Source'] for m in inspection['Mounts'] if m['Destination'] == '/var/lib/pcas')
    shutil.copyfile('/root/PCAS/.env', target / 'environment.env')
    for relative in ('model-api.json', 'notify.json', 'codex/auth.json'):
        source = Path(mount) / relative
        if source.exists():
            shutil.copyfile(source, target / ('private-' + relative.replace('/', '-')))
    models = next(m['Source'] for m in inspection['Mounts'] if m['Destination'] == '/etc/pcas/models.json')
    shutil.copyfile(models, target / 'models.json')
    files = target / 'file-volume.tar.gz'
    run(['tar', '-czf', str(files), '-C', mount, '.'])
    assert files.stat().st_size > 0, 'empty file backup'
    for p in target.iterdir():
        p.chmod(0o600)
    record = {'backup': str(target), 'databaseBytes': dump.stat().st_size, 'fileBytes': files.stat().st_size, 'priorImage': inspection['Config']['Image'], 'revision': sha, 'validated': True}
    (target / 'release-record.json').write_text(json.dumps(record, indent=2))
    return record


def deploy(sha):
    old = live_revision()
    assert old != sha, 'already live'
    release = RELEASES / sha
    compose, rollback = str(release / 'compose.sh'), str(RELEASES / old / 'compose.sh')
    current = live()
    env = dict(v.split('=', 1) for v in current[0]['Config']['Env'] if '=' in v)
    owner = str(uuid.UUID(env['PCAS_OWNER_ID']))
    counts = "SELECT json_build_object(" + ','.join("'%s',(SELECT count(*) FROM %s WHERE owner_id='%s')" % (t, t, owner) for t in ('memory_records', 'claims', 'sources', 'work_items', 'model_calls', 'model_usage')) + ")"
    report = {'revision': sha, 'priorRevision': old}
    run(DOCKER + ['stop', '--time', '45'] + [c['Id'] for c in current])
    try:
        report['before'] = sql(counts)
        report['backup'] = backup(sha, owner)
        run([compose, 'run', '--rm', 'migrate'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        report['after'] = sql(counts)
        assert report['before'] == report['after'], 'record counts changed during migration'
        run([compose, 'up', '-d', '--no-build', 'api', 'worker'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        for attempt in range(60):
            try:
                with urllib.request.urlopen(BASE + '/readyz', timeout=2) as response:
                    assert response.status == 200
                break
            except Exception:
                if attempt == 59:
                    raise
                time.sleep(1)
        assert all(c['Config']['Image'] == 'pcas-memory:release-' + sha and c['State']['Running'] for c in live())
        report['ready'] = True
        print(json.dumps(report, indent=1))
    except BaseException as error:
        report['error'] = str(error)
        try:
            run([rollback, 'up', '-d', '--no-build', 'api', 'worker'])
            report['rolledBack'] = True
        except BaseException as rollback_error:
            report['rollbackError'] = str(rollback_error)
        print(json.dumps(report, indent=1))
        raise


if __name__ == '__main__':
    {'prepare': prepare, 'deploy': deploy}[sys.argv[1]](sys.argv[2])
