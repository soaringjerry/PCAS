#!/usr/bin/env python3
"""Compare all owner-data rows without hiding runtime or identity differences.

Use private raw JSONL (optionally gzip). This first comparison mode is exact:
JSON object key order is irrelevant; every field and array order is retained.
A mismatch is a finding, never permission to strip dates, IDs, or versions.
"""
import argparse
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


def compare(paths, directory):
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
                path = private.private_path(path)
                if not path.is_file() or path.stat().st_mode & 0o077:
                    raise ValueError('snapshot must be a private regular file')
                opener = gzip.open if path.suffix == '.gz' else open
                digest = hashlib.sha256()
                count = 0
                with opener(path, 'rb') as source:
                    for line in source:
                        digest.update(line)
                        entry = json.loads(line, parse_float=Decimal)
                        if set(entry) != {'table', 'row'} or not isinstance(entry['row'], dict):
                            raise ValueError('invalid owner-data snapshot row')
                        body = json.dumps(canonical(entry['row']), ensure_ascii=False, separators=(',', ':'), allow_nan=False).encode()
                        fingerprint = hashlib.sha256(body).hexdigest()
                        db.execute('INSERT INTO rows VALUES (?,?,?,?,1) ON CONFLICT(side,phase,tab,digest) DO UPDATE SET n=n+1', (side, phase, entry['table'], fingerprint))
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
            return {'version': 1, 'comparison': 'exact_owner_data', 'excluded_fields': [], 'generated_identity_mapping': False,
                    'same_initial_data': not initial_differences,
                    'equal_changes': not initial_differences and not delta_differences,
                    'snapshots': evidence, 'tables': tables,
                    'initial_differences': initial_differences, 'delta_differences': delta_differences,
                    'limitation': 'Runtime and generated-identity differences remain findings. This result alone does not certify refactor equivalence.'}
        finally:
            db.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ['left-before', 'left-after', 'right-before', 'right-after', 'output']:
        parser.add_argument('--'+name, required=True)
    args = parser.parse_args()
    output = private.private_path(args.output)
    if output.exists():
        raise ValueError('comparison cannot replace earlier evidence')
    paths = {(side, phase): getattr(args, side+'_'+phase) for side in ['left', 'right'] for phase in ['before', 'after']}
    result = compare(paths, output.parent)
    # Exclusive publication: preserve any earlier good comparison.
    with output.open('x') as dest:
        output.chmod(0o600)
        json.dump(result, dest, ensure_ascii=False)
    print(json.dumps({k: result[k] for k in ['comparison', 'same_initial_data', 'equal_changes', 'generated_identity_mapping']}))
    return 0 if result['equal_changes'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
