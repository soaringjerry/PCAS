#!/usr/bin/env python3
"""Establish explicit identity correspondence without accepting state equivalence.

This diagnostic keeps every field. It matches only declared object identities
through domain keys, then rewrites typed UUID columns and declared JSON refs.
Runtime differences and unresolved identities remain visible findings.
"""
import argparse
from collections import Counter, defaultdict
import copy
import gzip
import hashlib
import importlib.util
import json
import os
from pathlib import Path
from decimal import Decimal

spec = importlib.util.spec_from_file_location('comparison', Path(__file__).with_name('foundation_replay_compare.py'))
comparison = importlib.util.module_from_spec(spec)
spec.loader.exec_module(comparison)


def fingerprint(value):
    return hashlib.sha256(json.dumps(comparison.canonical(value), ensure_ascii=False,
                                    separators=(',', ':'), allow_nan=False).encode()).hexdigest()


def transform(table, row, mapping, schema):
    result = copy.deepcopy(row)
    for column in schema.get(table, set()):
        if isinstance(result.get(column), str):
            result[column] = mapping.get(result[column], result[column])
    if table == 'entity_versions':
        # claims.go writes this source reference; editing.go reads it as UUID.
        disambiguation = result.get('disambiguation') or {}
        if 'source_id' in disambiguation:
            disambiguation['source_id'] = mapping.get(disambiguation['source_id'], disambiguation['source_id'])
    elif table == 'model_calls':
        for ref in result.get('input_manifest', {}).get('memoryRefs', []):
            ref['id'] = mapping.get(ref['id'], ref['id'])
        for key in ('jobId', 'reservationId'):
            receipt = result.get('result_receipt') or {}
            if key in receipt:
                receipt[key] = mapping.get(receipt[key], receipt[key])
    elif table == 'model_usage':
        for ref in result.get('memory_refs') or []:
            ref['id'] = mapping.get(ref['id'], ref['id'])
    elif table == 'desk_turns':
        for ref in result.get('dependencies') or []:
            ref['id'] = mapping.get(ref['id'], ref['id'])
        turn = (result.get('response') or {}).get('turn') or {}
        if 'id' in turn:
            turn['id'] = mapping.get(turn['id'], turn['id'])
    elif table == 'capture_candidates':
        doc = result['document']
        for key in ('id', 'resolvedInto'):
            if key in doc:
                doc[key] = mapping.get(doc[key], doc[key])
        source = doc.get('source') or {}
        if 'sourceId' in source:
            source['sourceId'] = mapping.get(source['sourceId'], source['sourceId'])
    return result


def establish(sides, schema, existing_ids):
    mapping, reverse, audit = {}, {}, []
    unresolved = []

    def bind(left, right, reason):
        # Existing identities are pinned. Never rename a historical object.
        if left in existing_ids or right in existing_ids:
            if left != right:
                raise ValueError('existing identity cannot be remapped')
            return
        if (left in mapping and mapping[left] != right) or (right in reverse and reverse[right] != left):
            raise ValueError('identity correspondence is not bijective')
        if left not in mapping:
            mapping[left], reverse[right] = right, left
            audit.append({'left': left, 'right': right, 'basis': reason})

    def pair(table, identity, fields, reason, nested=None):
        grouped = []
        for side in ('record', 'replay'):
            groups = defaultdict(list)
            for row in sides[side].get(table, []):
                normalized = transform(table, row, mapping if side == 'record' else {}, schema)
                key = {name: normalized[name] for name in fields}
                if nested:
                    key['document'] = nested(normalized)
                groups[fingerprint(key)].append(row)
            grouped.append(groups)
        pairs = []
        for key in sorted(set(grouped[0]) | set(grouped[1])):
            left, right = grouped[0].get(key, []), grouped[1].get(key, [])
            if len(left) != 1 or len(right) != 1:
                unresolved.append({'table': table, 'basis_sha256': key, 'left_count': len(left), 'right_count': len(right)})
                continue
            bind(left[0][identity], right[0][identity], reason)
            pairs.append((left[0], right[0]))
        return pairs

    pair('sources', 'id', ['owner_id', 'connector', 'external_id'], 'source connector and external identity')
    pair('entity_versions', 'entity_id', ['owner_id', 'version', 'entity_type', 'name', 'disambiguation'], 'entity version and full description')
    pair('chunks', 'id', ['owner_id', 'source_id', 'source_version', 'ordinal', 'version', 'body', 'start_rune', 'end_rune'], 'source version, ordinal, bounds, and text')
    if sides['record'].get('claim_revisions') or sides['replay'].get('claim_revisions'):
        fields = sorted(set(next(iter(sides['record'].get('claim_revisions') or sides['replay']['claim_revisions']))) - {'claim_id'})
        pair('claim_revisions', 'claim_id', fields, 'complete claim revision with mapped subject')
    pair('memory_jobs', 'id', ['owner_id', 'record_id', 'record_version', 'stage'], 'job unique owner, record version, and stage')
    pair('evidence', 'id', ['owner_id', 'source_id', 'source_version', 'target_id', 'target_version', 'locator', 'stance', 'acquisition'], 'complete evidence relation')

    def candidate(row):
        doc = copy.deepcopy(row['document'])
        # Exclude these only from the matching key. Final data checks keep them.
        doc.pop('id', None)
        doc.pop('createdAt', None)
        (doc.get('source') or {}).pop('at', None)
        return doc

    pair('capture_candidates', 'id', ['owner_id', 'source_id', 'source_version', 'state'], 'candidate source, state, and full business document', candidate)
    pair('desk_turns', 'id', ['owner_id', 'request_id', 'request_hash', 'conversation_id', 'agent_id'], 'secretary request, conversation, and agent')
    pair('desk_turn_order', 'creator_id', ['owner_id', 'request_id', 'request_hash', 'conversation_id', 'admission_order', 'status'], 'request admission creator fence')
    calls = pair('model_calls', 'id', ['owner_id', 'execution_id', 'root_execution_id', 'causation_id', 'function_name', 'stage', 'attempt_number', 'prompt_name', 'instruction_hash', 'schema_name', 'schema_hash', 'provider_id', 'model'], 'call execution, attempt, instructions, schema, and provider')
    for left, right in calls:
        for key in ('reservation_id', 'usage_id'):
            if bool(left[key]) != bool(right[key]):
                unresolved.append({'table': 'model_calls', 'field': key, 'reason': 'linked identity is absent on one side'})
            elif left[key]:
                bind(left[key], right[key], 'paired model call ' + key)
    # Legacy usage rows share their reservation ID with background_usage.
    # Repeated identical purposes remain ambiguous; do not pair by order.
    pair('model_usage', 'id', ['owner_id', 'purpose', 'agent_id', 'model', 'turn_id', 'run_id', 'job_id', 'tier', 'plan', 'memory_refs'], 'usage purpose, execution references, provider, and memory context')
    differences, unmapped = [], []
    for table in sorted(set(sides['record']) | set(sides['replay'])):
        if 'id' in schema.get(table, set()):
            origins = {side: {row['id'] for row in sides[side].get(table, [])} for side in ('record', 'replay')}
            for side, translations, other in [('record', mapping, 'replay'), ('replay', reverse, 'record')]:
                for identity in sorted(origins[side] - existing_ids - set(translations) - origins[other]):
                    unmapped.append({'table': table, 'side': side, 'id': identity, 'reason': 'no declared unique correspondence'})
        left = Counter(fingerprint(transform(table, row, mapping, schema)) for row in sides['record'].get(table, []))
        right = Counter(fingerprint(row) for row in sides['replay'].get(table, []))
        for digest in sorted(set(left) | set(right)):
            if left[digest] != right[digest]:
                differences.append({'table': table, 'row_sha256': digest, 'left_count': left[digest], 'right_count': right[digest]})
    return {'version': 1, 'comparison': 'declared_identity_correspondence', 'mapping': audit,
            'unresolved': unresolved, 'unmapped_identities': unmapped, 'remaining_differences': differences, 'excluded_fields': [],
            'state_equivalence_certified': False,
            'limitation': 'Matching keys establish correspondence only. Runtime fields, all business fields, and unknown JSON references remain in the final comparison.'}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('delta-rows', 'exact-comparison', 'initial-snapshot', 'output'):
        parser.add_argument('--'+name, required=True)
    args = parser.parse_args()
    private = comparison.private
    def read(path):
        path = private.private_path(path)
        if not path.is_file() or path.stat().st_mode & 0o077:
            raise ValueError('private evidence required')
        return json.loads(path.read_text(), parse_float=Decimal)
    exact, delta = read(args.exact_comparison), read(args.delta_rows)
    if exact.get('comparison') != 'exact_owner_data' or not exact.get('same_initial_data'):
        raise ValueError('equal exact initial data required')
    sides = {side: defaultdict(list) for side in ('record', 'replay')}
    expected = {(d['table'], d['row_sha256']): d for d in exact['delta_differences']}
    for side in sides:
        observed = Counter()
        for entry in delta['sides'][side]:
            row = json.loads(entry['raw'], parse_float=Decimal)
            digest = fingerprint(row['row'])
            if entry['row_sha256'] != digest or (row['table'], digest) not in expected or entry['phase'] != 'after':
                raise ValueError('unsupported or mismatched delta evidence')
            sides[side][row['table']].append(row['row'])
            observed[row['table'], digest] += 1
        field = 'left_delta' if side == 'record' else 'right_delta'
        required = Counter({key: value[field] for key, value in expected.items() if value[field] > 0})
        if any(value[field] < 0 for value in expected.values()) or observed != required:
            raise ValueError('this correspondence mode requires complete added-row evidence')
    schema = defaultdict(set)
    for column in delta['schema']:
        if column['type'] == 'uuid':
            schema[column['table']].add(column['column'])
    initial = private.private_path(args.initial_snapshot)
    if not initial.is_file() or initial.stat().st_mode & 0o077:
        raise ValueError('private initial snapshot required')
    existing = set()
    sha = hashlib.sha256()
    opener = gzip.open if initial.suffix == '.gz' else open
    with opener(initial, 'rb') as source:
        for line in source:
            sha.update(line)
            entry = json.loads(line)
            # Include all initial UUID fields, so neither object identities nor
            # references to historical objects can enter a generated-ID map.
            for column in schema.get(entry['table'], set()):
                if isinstance(entry['row'].get(column), str):
                    existing.add(entry['row'][column])
    if sha.hexdigest() != exact['snapshots']['left_before']['uncompressed_sha256']:
        raise ValueError('initial snapshot fingerprint mismatch')
    result = establish(sides, schema, existing)
    result['same_initial_data'] = True
    output = private.private_path(args.output)
    with output.open('x') as dest:
        os.fchmod(dest.fileno(), 0o600)
        json.dump(result, dest, ensure_ascii=False)
        dest.flush()
        os.fsync(dest.fileno())
    print(json.dumps({'comparison': result['comparison'], 'mapped_identities': len(result['mapping']),
                      'unresolved': len(result['unresolved']), 'unmapped_identities': len(result['unmapped_identities']), 'remaining_differences': len(result['remaining_differences']),
                      'state_equivalence_certified': False}))
    return 1 if result['unresolved'] or result['unmapped_identities'] or result['remaining_differences'] else 0


if __name__ == '__main__':
    raise SystemExit(main())
