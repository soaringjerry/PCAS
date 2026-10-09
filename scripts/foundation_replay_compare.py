#!/usr/bin/env python3
"""Compare all owner-data rows without hiding runtime or identity differences.

Use private raw JSONL (optionally gzip). This first comparison mode is exact:
JSON object key order is irrelevant; every field and array order is retained.
A mismatch is a finding, never permission to strip dates, IDs, or versions.
"""
import argparse
from collections import Counter
from contextlib import contextmanager
import gzip
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sqlite3
import tempfile
from decimal import Decimal

spec = importlib.util.spec_from_file_location('model_replay', Path(__file__).with_name('foundation_model_replay.py'))
private = importlib.util.module_from_spec(spec)
spec.loader.exec_module(private)


def canonical(value):
    # Preserve decimal precision and scalar types. Do not route SQL numeric
    # values through binary floats or let a user string mimic a numeric token.
    if isinstance(value, dict):
        return ['object', [[key, canonical(item)] for key, item in sorted(value.items())]]
    if isinstance(value, list):
        return ['array', [canonical(item) for item in value]]
    if value is None:
        return ['null']
    if isinstance(value, bool):
        return ['boolean', value]
    if isinstance(value, Decimal):
        return ['decimal', str(value)]
    if isinstance(value, int):
        return ['integer', str(value)]
    if isinstance(value, str):
        return ['string', value]
    raise ValueError('unsupported snapshot scalar')


def fingerprint(value):
    return hashlib.sha256(json.dumps(canonical(value), ensure_ascii=False,
                                    separators=(',', ':'), allow_nan=False).encode()).hexdigest()


@contextmanager
def new_private_file(path):
    """Publish complete evidence exclusively; never replace an earlier result."""
    path = private.private_path(path)
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    if path.exists():
        raise ValueError('comparison cannot replace earlier evidence')
    fd, name = tempfile.mkstemp(prefix='.comparison-', dir=path.parent)
    try:
        with os.fdopen(fd, 'w', encoding='utf-8') as stream:
            yield stream
            stream.flush()
            os.fsync(stream.fileno())
        os.link(name, path)
        directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        os.unlink(name)


def snapshot_rows(path, with_fingerprints=True):
    path = private.private_path(path)
    if not path.is_file() or path.stat().st_mode & 0o077:
        raise ValueError('snapshot must be a private regular file')
    opener = gzip.open if path.suffix == '.gz' else open
    with opener(path, 'rb') as source:
        for line in source:
            if not with_fingerprints:
                # The exact report already parsed these bytes. Export still
                # rechecks every byte and occurrence, even without differences.
                yield line, None, None
                continue
            entry = json.loads(line, parse_float=Decimal)
            if (set(entry) != {'table', 'row'} or not isinstance(entry['row'], dict)
                    or not isinstance(entry['table'], str) or not entry['table']):
                raise ValueError('invalid owner-data snapshot row')
            yield line, entry, fingerprint(entry['row'])


def compare(paths, directory, normalizer=None):
    if set(paths) != {(side, phase) for side in ('left', 'right') for phase in ('before', 'after')}:
        raise ValueError('all four owner-data snapshots are required')
    directory = private.private_path(directory)
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.row-comparison-', dir=directory) as work:
        path = Path(work) / 'rows.sqlite'
        os.close(os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600))
        db = sqlite3.connect(str(path))
        try:
            db.execute('CREATE TABLE rows (side TEXT, phase TEXT, tab TEXT, digest TEXT, n INTEGER, PRIMARY KEY(side,phase,tab,digest))')
            evidence = {}
            for (side, phase), path in paths.items():
                digest = hashlib.sha256()
                count = 0
                for line, entry, row_hash in snapshot_rows(path):
                    digest.update(line)
                    if normalizer is not None:
                        normalized = normalizer(side, phase, entry['table'], entry['row'])
                        if normalized is not entry['row']:
                            row_hash = fingerprint(normalized)
                    db.execute('INSERT INTO rows VALUES (?,?,?,?,1) ON CONFLICT(side,phase,tab,digest) DO UPDATE SET n=n+1', (side, phase, entry['table'], row_hash))
                    count += 1
                if not count:
                    raise ValueError('empty owner-data snapshot')
                evidence[side + '_' + phase] = {'rows': count, 'uncompressed_sha256': digest.hexdigest()}
            db.commit()
            sql = """SELECT tab,digest,
              sum(CASE WHEN side='left' AND phase='before' THEN n ELSE 0 END) lb,
              sum(CASE WHEN side='right' AND phase='before' THEN n ELSE 0 END) rb,
              sum(CASE WHEN side='left' AND phase='after' THEN n ELSE 0 END) la,
              sum(CASE WHEN side='right' AND phase='after' THEN n ELSE 0 END) ra
              FROM rows GROUP BY tab,digest"""
            initial_differences, delta_differences = [], []
            tables = {}
            for tab, digest, lb, rb, la, ra in db.execute(sql):
                counts = tables.setdefault(tab, {'left_before': 0, 'right_before': 0, 'left_after': 0, 'right_after': 0})
                for key, n in zip(counts, [lb, rb, la, ra]):
                    counts[key] += n
                if lb != rb:
                    initial_differences.append({'table': tab, 'row_sha256': digest, 'left_count': lb, 'right_count': rb})
                if la-lb != ra-rb:
                    delta_differences.append({'table': tab, 'row_sha256': digest, 'left_delta': la-lb, 'right_delta': ra-rb})
            return {'version': 1, 'comparison': 'exact_owner_data' if normalizer is None else 'identity_mapped_owner_data',
                    'excluded_fields': [], 'generated_identity_mapping': normalizer is not None,
                    'same_initial_data': not initial_differences,
                    'equal_changes': not initial_differences and not delta_differences,
                    'snapshots': evidence, 'tables': tables,
                    'initial_differences': initial_differences, 'delta_differences': delta_differences,
                    'limitation': 'Runtime and generated-identity differences remain findings. This result alone does not certify refactor equivalence.'}
        finally:
            db.close()


def export_delta(paths, result, output, schema=None):
    """Stream complete signed differences with their original snapshot binding."""
    if result.get('comparison') != 'exact_owner_data':
        raise ValueError('exact comparison required for delta export')
    with new_private_file(output) as dest:
        header = {'version': 1, 'comparison': 'signed_owner_data_delta',
                  'exact_comparison_sha256': fingerprint(result),
                  'snapshots': result['snapshots'], 'schema': schema or []}
        encoded = json.dumps(header, ensure_ascii=False, separators=(',', ':'))
        dest.write(encoded[:-1] + ',"sides":{')
        for index, (side, label, field) in enumerate((('left', 'record', 'left_delta'), ('right', 'replay', 'right_delta'))):
            if index:
                dest.write(',')
            dest.write(json.dumps(label) + ':[')
            first = True
            for phase, sign in (('before', -1), ('after', 1)):
                required = Counter({(d['table'], d['row_sha256']): abs(d[field])
                                    for d in result['delta_differences'] if d[field] * sign > 0})
                needs_rows = bool(required)
                digest, count = hashlib.sha256(), 0
                for line, row, row_hash in snapshot_rows(paths[side, phase], needs_rows):
                    digest.update(line)
                    count += 1
                    if not needs_rows:
                        continue
                    key = row['table'], row_hash
                    if required[key] <= 0:
                        continue
                    if not first:
                        dest.write(',')
                    first = False
                    json.dump({'phase': phase, 'row_sha256': row_hash,
                               'raw': line.decode('utf-8').rstrip('\n')}, dest, ensure_ascii=False)
                    required[key] -= 1
                observed = {'rows': count, 'uncompressed_sha256': digest.hexdigest()}
                if observed != result['snapshots'][side + '_' + phase]:
                    raise ValueError('snapshot changed after exact comparison')
                if any(required.values()):
                    raise ValueError('delta export is missing snapshot occurrences')
            dest.write(']')
        dest.write('}}')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ['left-before', 'left-after', 'right-before', 'right-after', 'output']:
        parser.add_argument('--'+name, required=True)
    parser.add_argument('--delta-output', help='New private file for signed raw row differences')
    parser.add_argument('--schema-evidence', help='Private evidence containing database column and primary-key metadata')
    args = parser.parse_args()
    output = private.private_path(args.output)
    if output.exists():
        raise ValueError('comparison cannot replace earlier evidence')
    if args.schema_evidence and not args.delta_output:
        raise ValueError('schema evidence requires delta output')
    schema = None
    if args.schema_evidence:
        schema_path = private.private_path(args.schema_evidence)
        if not schema_path.is_file() or schema_path.stat().st_mode & 0o077:
            raise ValueError('private schema evidence required')
        schema = json.loads(schema_path.read_text())['schema']
        if not isinstance(schema, list):
            raise ValueError('invalid schema evidence')
    if args.delta_output and private.private_path(args.delta_output) == output:
        raise ValueError('comparison and delta outputs must be different')
    if args.delta_output and private.private_path(args.delta_output).exists():
        raise ValueError('delta export cannot replace earlier evidence')
    paths = {(side, phase): getattr(args, side+'_'+phase) for side in ['left', 'right'] for phase in ['before', 'after']}
    result = compare(paths, output.parent)
    if args.delta_output:
        export_delta(paths, result, args.delta_output, schema)
    # Exclusive publication: preserve any earlier good comparison.
    with new_private_file(output) as dest:
        json.dump(result, dest, ensure_ascii=False)
    print(json.dumps({k: result[k] for k in ['comparison', 'same_initial_data', 'equal_changes', 'generated_identity_mapping']}))
    return 0 if result['equal_changes'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
