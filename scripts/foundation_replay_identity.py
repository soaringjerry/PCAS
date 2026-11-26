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
from pathlib import Path
from decimal import Decimal

spec = importlib.util.spec_from_file_location('comparison', Path(__file__).with_name('foundation_replay_compare.py'))
comparison = importlib.util.module_from_spec(spec)
spec.loader.exec_module(comparison)


fingerprint = comparison.fingerprint


def reference_locations(table, row, schema):
    """Use the same declared references for mapping and historical pinning."""
    for column in schema.get(table, set()):
        if column in row:
            yield row, column
    containers = []
    if table == 'entity_versions':
        containers.append((row.get('disambiguation'), ('source_id',)))
    elif table == 'model_calls':
        manifest = row.get('input_manifest') or {}
        containers.extend((ref, ('id',)) for ref in manifest.get('memoryRefs') or [])
        receipt = row.get('result_receipt') or {}
        containers.append((receipt, ('jobId', 'reservationId', 'invocationId')))
        if isinstance(receipt, dict):
            billing = receipt.get('billing') or {}
            containers.append((billing, ('invocationId', 'Reservation')))
            if isinstance(billing, dict):
                containers.extend((ref, ('id',)) for ref in billing.get('Refs') or [])
        mode = row.get('actual_mode') or {}
        if isinstance(mode, dict):
            containers.append((mode.get('deputySelection'), ('main', 'output', 'review')))
            containers.append((mode.get('originalUsage'), ('ID', 'TurnID', 'RunID', 'JobID')))
    elif table == 'model_usage':
        containers.extend((ref, ('id',)) for ref in row.get('memory_refs') or [])
    elif table == 'desk_turns':
        containers.extend((ref, ('id',)) for ref in row.get('dependencies') or [])
        containers.append(((row.get('response') or {}).get('turn'), ('id',)))
    elif table == 'capture_candidates':
        doc = row.get('document') or {}
        containers.append((doc, ('id', 'resolvedInto')))
        containers.append((doc.get('source'), ('sourceId',)))
    for container, fields in containers:
        if isinstance(container, dict):
            for field in fields:
                if field in container:
                    yield container, field


def transform(table, row, mapping, schema):
    if not any(isinstance(container[field], str) and container[field] in mapping
               for container, field in reference_locations(table, row, schema)):
        return row
    result = copy.deepcopy(row)
    for container, field in reference_locations(table, result, schema):
        value = container[field]
        if isinstance(value, str):
            container[field] = mapping.get(value, value)
    return result


def row_differences(sides, schema, mapping):
    differences = []
    for table in sorted(set(sides['record']) | set(sides['replay'])):
        left = Counter(fingerprint(transform(table, row, mapping, schema)) for row in sides['record'].get(table, []))
        right = Counter(fingerprint(row) for row in sides['replay'].get(table, []))
        for digest in sorted(set(left) | set(right)):
            if left[digest] != right[digest]:
                differences.append({'table': table, 'row_sha256': digest,
                                    'left_count': left[digest], 'right_count': right[digest]})
    return differences


def delta_rows(exact, delta):
    """Check every signed occurrence before establishing any correspondence."""
    if exact.get('comparison') != 'exact_owner_data' or not exact.get('same_initial_data'):
        raise ValueError('equal exact initial data required')
    if delta.get('comparison') is not None:
        if (delta.get('comparison') != 'signed_owner_data_delta' or delta.get('version') != 1
                or delta.get('exact_comparison_sha256') != fingerprint(exact)
                or delta.get('snapshots') != exact.get('snapshots')):
            raise ValueError('delta does not bind to the exact comparison')
    if set(delta['sides']) != {'record', 'replay'}:
        raise ValueError('both delta sides are required')
    expected = {(d['table'], d['row_sha256']): d for d in exact['delta_differences']}
    if len(expected) != len(exact['delta_differences']):
        raise ValueError('duplicate expected delta occurrence')
    phases = {phase: {side: defaultdict(list) for side in ('record', 'replay')}
              for phase in ('before', 'after')}
    for side, field in (('record', 'left_delta'), ('replay', 'right_delta')):
        observed = Counter()
        for entry in delta['sides'][side]:
            row = json.loads(entry['raw'], parse_float=Decimal)
            if set(row) != {'table', 'row'} or not isinstance(row['row'], dict):
                raise ValueError('invalid raw delta row')
            digest, phase = fingerprint(row['row']), entry['phase']
            key = row['table'], digest
            if entry['row_sha256'] != digest or key not in expected or phase not in phases:
                raise ValueError('unsupported or mismatched delta evidence')
            sign = -1 if phase == 'before' else 1
            if expected[key][field] * sign <= 0:
                raise ValueError('delta phase does not match its signed change')
            phases[phase][side][row['table']].append(row['row'])
            observed[(phase,) + key] += 1
        required = Counter({('after' if value[field] > 0 else 'before', *key): abs(value[field])
                            for key, value in expected.items() if value[field]})
        if observed != required:
            raise ValueError('complete signed delta evidence required')
    return phases


def value_differences(left, right, path=()):
    """Retain typed values, field presence, and array positions in private output."""
    if isinstance(left, dict) and isinstance(right, dict):
        keys = sorted(set(left) | set(right))
    elif isinstance(left, list) and isinstance(right, list):
        keys = range(max(len(left), len(right)))
    else:
        if comparison.canonical(left) != comparison.canonical(right):
            yield {'path': list(path), 'left_present': True, 'right_present': True,
                   'left_value': comparison.canonical(left), 'right_value': comparison.canonical(right)}
        return
    for key in keys:
        lp = key in left if isinstance(left, dict) else key < len(left)
        rp = key in right if isinstance(right, dict) else key < len(right)
        if lp and rp:
            yield from value_differences(left[key], right[key], (*path, key))
        else:
            yield {'path': [*path, key], 'left_present': lp, 'right_present': rp,
                   'left_value': comparison.canonical(left[key]) if lp else None,
                   'right_value': comparison.canonical(right[key]) if rp else None}


RUNTIME_TIMESTAMP_COLUMNS = {
    'at', 'created_at', 'updated_at', 'started_at', 'finished_at',
    'occurred_at', 'lease_until', 'expires_at', 'available_at',
    'written_at', 'built_at', 'refreshed_at', 'synced_at', 'checked_at',
    'last_attempt_at', 'next_attempt_at', 'delivered_at', 'undone_at',
}
RUNTIME_SCALAR_COLUMNS = {'duration_ms'}
RUNTIME_TIMESTAMP_TYPES = {
    'timestamptz', 'timestamp with time zone', 'timestamp without time zone', 'timestamp',
}
RUNTIME_NESTED_PATHS = {
    ('actual_mode', 'leaseToken'),
    ('actual_mode', 'executionToken'),
    ('actual_mode', 'interruptedAt'),
    ('actual_mode', 'originalUsage', 'At'),
    ('result_receipt', 'billing', 'DurationMS'),
    ('document', 'createdAt'),
    ('document', 'updatedAt'),
    ('document', 'startedAt'),
    ('document', 'finishedAt'),
    ('response', 'turn', 'at'),
}


def is_runtime_metadata_path(table, path, column_types):
    if len(path) == 1:
        col = path[0]
        if col in RUNTIME_SCALAR_COLUMNS:
            return True
        if col in RUNTIME_TIMESTAMP_COLUMNS and column_types.get((table, col)) in RUNTIME_TIMESTAMP_TYPES:
            return True
        return False
    return tuple(path) in RUNTIME_NESTED_PATHS


def column_metadata(columns):
    schema, primary = defaultdict(set), defaultdict(list)
    names = defaultdict(set)
    for column in columns:
        table, name = column['table'], column['column']
        if (not isinstance(table, str) or not table or not isinstance(name, str) or not name
                or name in names[table] or not isinstance(column['type'], str) or not column['type']
                or not isinstance(column['primary_key'], bool)):
            raise ValueError('invalid or duplicate column metadata')
        names[table].add(name)
        if column['type'] == 'uuid':
            schema[table].add(name)
        if column['primary_key']:
            primary[table].append(name)
    return schema, primary, names


def field_comparison(phases, columns, mapping):
    schema, primary, names = column_metadata(columns)
    column_types = {(c['table'], c['column']): c['type'] for c in columns}
    runtime_diffs, business_diffs = [], []
    report = {'paired_rows': 0, 'identical_paired_rows': 0, 'field_differences': [],
              'unpaired_rows': [], 'excluded_fields': [], 'value_format': 'typed_canonical'}
    for phase, sides in phases.items():
        for table in sorted(set(sides['record']) | set(sides['replay'])):
            grouped = {side: defaultdict(list) for side in sides}
            if not primary[table]:
                report['unpaired_rows'].append({'phase': phase, 'table': table, 'reason': 'primary_key_not_declared',
                                               'left_count': len(sides['record'].get(table, [])),
                                               'right_count': len(sides['replay'].get(table, []))})
                continue
            for side in sides:
                for row in sides[side].get(table, []):
                    if set(row) != names[table]:
                        raise ValueError('delta columns differ from declared metadata')
                    normalized = transform(table, row, mapping if side == 'record' else {}, schema)
                    if any(name not in normalized or normalized[name] is None for name in primary[table]):
                        raise ValueError('delta row lacks its declared primary key')
                    key = fingerprint({name: normalized[name] for name in primary[table]})
                    grouped[side][key].append(normalized)
            for key in sorted(set(grouped['record']) | set(grouped['replay'])):
                left, right = grouped['record'].get(key, []), grouped['replay'].get(key, [])
                row = (left or right)[0]
                primary_value = comparison.canonical({name: row[name] for name in primary[table]})
                if len(left) != 1 or len(right) != 1:
                    report['unpaired_rows'].append({'phase': phase, 'table': table, 'primary_key_sha256': key,
                                                   'primary_key': primary_value,
                                                   'reason': 'ambiguous_or_absent_primary_key',
                                                   'left_count': len(left), 'right_count': len(right)})
                    continue
                report['paired_rows'] += 1
                differences = list(value_differences(left[0], right[0]))
                if differences:
                    entry = {'phase': phase, 'table': table,
                             'primary_key_sha256': key, 'primary_key': primary_value,
                             'differences': differences}
                    report['field_differences'].append(entry)
                    for diff in differences:
                        item = {'phase': phase, 'table': table, 'primary_key_sha256': key, 'path': diff['path']}
                        if is_runtime_metadata_path(table, diff['path'], column_types):
                            runtime_diffs.append(item)
                        else:
                            business_diffs.append(item)
                else:
                    report['identical_paired_rows'] += 1
    report['difference_classification'] = {
        'runtime_metadata_differences': runtime_diffs,
        'business_state_differences': business_diffs,
        'runtime_only': bool(report['field_differences']) and not business_diffs and not report['unpaired_rows'],
        'rule': 'Wall-clock execution timestamps, durations, and per-attempt execution tokens are classified as runtime metadata; business dates, versions, actions, costs, and outputs remain business state. Every difference remains in field_differences.',
    }
    return report


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
    pair('recorded_events', 'id', ['owner_id', 'type', 'stage', 'object_id', 'object_version', 'trigger_source', 'job_id', 'root_id', 'cause_id', 'chain_depth'], 'recorded queue event causal identity')
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
    unmapped = []
    for table in sorted(set(sides['record']) | set(sides['replay'])):
        if 'id' in schema.get(table, set()):
            origins = {side: {row['id'] for row in sides[side].get(table, [])} for side in ('record', 'replay')}
            for side, translations, other in [('record', mapping, 'replay'), ('replay', reverse, 'record')]:
                for identity in sorted(origins[side] - existing_ids - set(translations) - origins[other]):
                    unmapped.append({'table': table, 'side': side, 'id': identity, 'reason': 'no declared unique correspondence'})
    return {'version': 1, 'comparison': 'declared_identity_correspondence', 'mapping': audit,
            'unresolved': unresolved, 'unmapped_identities': unmapped, 'remaining_differences': row_differences(sides, schema, mapping), 'excluded_fields': [],
            'state_equivalence_certified': False,
            'limitation': 'Matching keys establish correspondence only. Runtime fields, all business fields, and unknown JSON references remain in the final comparison.'}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('delta-rows', 'exact-comparison', 'initial-snapshot', 'output'):
        parser.add_argument('--'+name, required=True)
    for name in ('left-after', 'right-before', 'right-after'):
        parser.add_argument('--'+name, help='Optional complete snapshot set for checking mappings outside changed rows')
    args = parser.parse_args()
    private = comparison.private
    output = private.private_path(args.output)
    if output.exists():
        raise ValueError('comparison cannot replace earlier evidence')
    full_paths = [args.left_after, args.right_before, args.right_after]
    if any(full_paths) and not all(full_paths):
        raise ValueError('all complete snapshot paths are required')
    def read(path):
        path = private.private_path(path)
        if not path.is_file() or path.stat().st_mode & 0o077:
            raise ValueError('private evidence required')
        return json.loads(path.read_text(), parse_float=Decimal)
    exact, delta = read(args.exact_comparison), read(args.delta_rows)
    phases = delta_rows(exact, delta)
    schema, _, column_names = column_metadata(delta['schema'])
    initial = private.private_path(args.initial_snapshot)
    if not initial.is_file() or initial.stat().st_mode & 0o077:
        raise ValueError('private initial snapshot required')
    existing = set()
    missing_tables = set()
    sha = hashlib.sha256()
    opener = gzip.open if initial.suffix == '.gz' else open
    with opener(initial, 'rb') as source:
        for line in source:
            sha.update(line)
            entry = json.loads(line)
            if not column_names[entry['table']]:
                missing_tables.add(entry['table'])
            elif set(entry['row']) != column_names[entry['table']]:
                raise ValueError('initial columns differ from declared metadata')
            # Include all initial UUID fields, so neither object identities nor
            # references to historical objects can enter a generated-ID map.
            for container, field in reference_locations(entry['table'], entry['row'], schema):
                if isinstance(container[field], str):
                    existing.add(container[field])
    if sha.hexdigest() != exact['snapshots']['left_before']['uncompressed_sha256']:
        raise ValueError('initial snapshot fingerprint mismatch')
    if delta.get('comparison') and missing_tables:
        raise ValueError('complete initial column metadata required for bound delta evidence')
    result = establish(phases['after'], schema, existing)
    mapping = {item['left']: item['right'] for item in result['mapping']}
    result['remaining_differences'] = [dict(difference, phase=phase) for phase, sides in phases.items()
                                       for difference in row_differences(sides, schema, mapping)]
    result['field_comparison'] = field_comparison(phases, delta['schema'], mapping)
    result['delta_binding'] = 'exact_report_and_snapshots' if delta.get('comparison') else 'legacy_occurrence_counts'
    result['historical_schema_gaps'] = sorted(missing_tables)
    result['full_owner_data_checked'] = False
    if all(full_paths):
        if missing_tables:
            raise ValueError('full owner comparison requires complete historical column metadata')
        paths = {('left', 'before'): args.initial_snapshot, ('left', 'after'): args.left_after,
                 ('right', 'before'): args.right_before, ('right', 'after'): args.right_after}
        full = comparison.compare(paths, output.parent,
            lambda side, phase, table, row: transform(table, row, mapping if side == 'left' else {}, schema))
        if full['snapshots'] != exact['snapshots']:
            raise ValueError('complete snapshots do not bind to the original exact comparison')
        result['full_owner_comparison'] = full
        result['full_owner_data_checked'] = True
    result['same_initial_data'] = True
    with comparison.new_private_file(output) as dest:
        json.dump(result, dest, ensure_ascii=False)
    classification = result['field_comparison']['difference_classification']
    print(json.dumps({'comparison': result['comparison'], 'mapped_identities': len(result['mapping']),
                      'unresolved': len(result['unresolved']), 'unmapped_identities': len(result['unmapped_identities']), 'remaining_differences': len(result['remaining_differences']),
                      'paired_rows': result['field_comparison']['paired_rows'],
                      'unpaired_groups': len(result['field_comparison']['unpaired_rows']),
                      'business_state_differences': len(classification['business_state_differences']),
                      'runtime_metadata_differences': len(classification['runtime_metadata_differences']),
                      'full_owner_data_checked': result['full_owner_data_checked'],
                      'state_equivalence_certified': False}))
    return 1 if (result['unresolved'] or result['unmapped_identities'] or result['remaining_differences']
                 or result['field_comparison']['unpaired_rows'] or result['historical_schema_gaps']
                 or result.get('full_owner_comparison', {}).get('equal_changes') is False) else 0


if __name__ == '__main__':
    raise SystemExit(main())
