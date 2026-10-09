#!/usr/bin/env python3
import gzip
import importlib.util
import json
from decimal import Decimal
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

spec=importlib.util.spec_from_file_location('state_compare',Path(__file__).with_name('foundation_replay_compare.py'))
comparison=importlib.util.module_from_spec(spec);spec.loader.exec_module(comparison)
spec=importlib.util.spec_from_file_location('identity_compare',Path(__file__).with_name('foundation_replay_identity.py'))
identity=importlib.util.module_from_spec(spec);spec.loader.exec_module(identity)

class IdentityCorrespondenceTests(unittest.TestCase):
 def sides(self):
  return {side:{'sources':[{'id':key,'owner_id':'owner','connector':'fictional','external_id':'same-input','version':1,'deadline':'2030-01-01','created_at':'actual-time','body':key}]}
          for side,key in [('record','generated-left'),('replay','generated-right')]}
 def test_only_declared_references_change_and_business_fields_remain(self):
  sides=self.sides()
  result=identity.establish(sides,{'sources':{'id','owner_id'}},{'owner'})
  self.assertEqual(len(result['mapping']),1)
  self.assertTrue(result['remaining_differences'])
  normalized=identity.transform('sources',sides['record']['sources'][0],{'generated-left':'generated-right'},{'sources':{'id'}})
  self.assertEqual(normalized['id'],'generated-right')
  self.assertEqual(normalized['body'],'generated-left')
  for field,value in [('version',2),('deadline','2030-01-02'),('created_at','other-actual-time')]:
   with self.subTest(field=field):
    sides=self.sides()
    sides['record']['sources'][0]['body']=sides['replay']['sources'][0]['body']='same-text'
    sides['replay']['sources'][0][field]=value
    self.assertTrue(identity.establish(sides,{'sources':{'id','owner_id'}},{'owner'})['remaining_differences'])
 def test_historical_identities_cannot_be_remapped(self):
  with self.assertRaisesRegex(ValueError,'existing identity'):
   identity.establish(self.sides(),{'sources':{'id'}},{'generated-left'})
 def test_historical_references_without_a_surviving_object_are_also_pinned(self):
  row={'memory_refs':[{'id':'generated-left','version':1}],'note':'other-user-text'}
  existing={container[field] for container,field in identity.reference_locations('model_usage',row,{})}
  self.assertEqual(existing,{'generated-left'})
  with self.assertRaisesRegex(ValueError,'existing identity'):
   identity.establish(self.sides(),{'sources':{'id'}},existing)
 def test_declared_entity_source_reference_preserves_other_disambiguation(self):
  row={'disambiguation':{'source_id':'left','note':'left'}}
  normalized=identity.transform('entity_versions',row,{'left':'right'}, {})
  self.assertEqual(normalized['disambiguation'],{'source_id':'right','note':'left'})
 def test_secretary_references_preserve_user_text_and_dependency_versions(self):
  row={'response':{'turn':{'id':'left','text':'left'}},'dependencies':[{'id':'left','version':3,'kind':'claim'}]}
  normalized=identity.transform('desk_turns',row,{'left':'right'}, {})
  self.assertEqual(normalized['response']['turn'],{'id':'right','text':'left'})
  self.assertEqual(normalized['dependencies'],[{'id':'right','version':3,'kind':'claim'}])
 def test_ambiguous_domain_keys_do_not_match_by_row_order(self):
  sides=self.sides()
  sides['record']['sources'].append(dict(sides['record']['sources'][0],id='another-left'))
  result=identity.establish(sides,{'sources':{'id'}},set())
  self.assertTrue(result['unresolved'])
  self.assertEqual(result['mapping'],[])
 def test_equal_cost_cannot_supply_a_missing_reservation_link(self):
  sides={side:{'background_usage':[{'id':key,'owner_id':'owner','reserved_cost':1}]}
         for side,key in [('record','unlinked-left'),('replay','unlinked-right')]}
  result=identity.establish(sides,{'background_usage':{'id','owner_id'}},{'owner'})
  self.assertEqual(len(result['unmapped_identities']),2)
  self.assertTrue(result['remaining_differences'])
  self.assertFalse(result['state_equivalence_certified'])

class StateComparisonTests(unittest.TestCase):
 def setUp(self):
  self.directory=tempfile.TemporaryDirectory();self.addCleanup(self.directory.cleanup);self.root=Path(self.directory.name)
 def run_case(self,left,right,other_before=None):
  before=[{'table':'items','row':{'id':'fixed','version':1,'title':'Fictional','deadline':'2030-01-01','project_id':'project-a'}}]
  paths={}
  for side,phase,rows in [('left','before',before),('left','after',left),('right','before',other_before or before),('right','after',right)]:
   path=self.root/(side+'-'+phase+'.jsonl.gz')
   with gzip.open(path,'wt') as file:
    for row in rows:file.write(json.dumps(row)+'\n')
   path.chmod(0o600);paths[side,phase]=path
  return comparison.compare(paths,self.root)
 def test_row_order_and_object_key_order_do_not_change_business_data(self):
  a={'table':'items','row':{'id':'fixed','version':2,'title':'Fictional'}};b={'table':'items','row':{'id':'new','version':1,'title':'Other'}}
  result=self.run_case([a,b],[b,{'table':'items','row':dict(reversed(list(a['row'].items())))}]);self.assertTrue(result['equal_changes']);self.assertEqual(result['excluded_fields'],[])
 def test_versions_deadlines_relationships_and_runtime_dates_are_retained(self):
  for field,value in [('version',3),('deadline','2030-01-02'),('project_id','project-b'),('updated_at','2030-01-01T10:01:00Z'),('id','different-generated-id')]:
   with self.subTest(field=field):
    a={'table':'items','row':{'id':'fixed','version':2,'deadline':'2030-01-01','project_id':'project-a','updated_at':'2030-01-01T10:00:00Z'}}
    b={'table':'items','row':dict(a['row'],**{field:value})};result=self.run_case([a],[b]);self.assertFalse(result['equal_changes']);self.assertTrue(result['delta_differences'])
 def test_different_initial_data_cannot_pass_even_when_deltas_match(self):
  other=[{'table':'items','row':{'id':'fixed','version':4}}]
  original=[{'table':'items','row':{'id':'fixed','version':1,'title':'Fictional','deadline':'2030-01-01','project_id':'project-a'}}]
  result=self.run_case(original,other,other);self.assertFalse(result['same_initial_data']);self.assertFalse(result['equal_changes'])
 def test_duplicate_rows_are_counted_and_not_collapsed(self):
  a={'table':'items','row':{'id':'fixed','version':2}};result=self.run_case([a,a],[a]);self.assertFalse(result['equal_changes']);self.assertEqual(result['tables']['items']['left_after'],2)
 def test_sql_decimal_precision_cannot_be_hidden_by_float_rounding(self):
  paths={}
  for side in ['left','right']:
   for phase in ['before','after']:
    number='0' if phase=='before' else ('0.123456789123456789' if side=='left' else '0.123456789123456790')
    path=self.root/(side+'-'+phase+'.jsonl.gz')
    with gzip.open(path,'wt') as file:file.write('{"table":"usage","row":{"id":"fixed","cost":'+number+'}}\n')
    path.chmod(0o600);paths[side,phase]=path
  result=comparison.compare(paths,self.root);self.assertTrue(result['same_initial_data']);self.assertFalse(result['equal_changes'])

class SignedDeltaTests(unittest.TestCase):
 def setUp(self):
  self.directory=tempfile.TemporaryDirectory();self.addCleanup(self.directory.cleanup);self.root=Path(self.directory.name)
  self.owner={'table':'owners','row':{'id':'owner'}}
  self.item={'table':'items','row':{'id':'fixed','version':1,'deadline':'2030-01-01','dependencies':[{'id':'claim','version':1}]}}
  self.columns=[{'table':'items','column':name,'type':'text','primary_key':name=='id'} for name in self.item['row']]
 def snapshots(self,left,right):
  paths={}
  for side,phase,rows in [('left','before',[self.item]),('right','before',[self.item]),('left','after',left),('right','after',right)]:
   path=self.root/(side+'-'+phase+'.jsonl.gz')
   with gzip.open(path,'wt') as file:
    for row in [self.owner,*rows]:file.write(json.dumps(row)+'\n')
   path.chmod(0o600);paths[side,phase]=path
  return paths
 def evidence(self,left,right):
  paths=self.snapshots(left,right);exact=comparison.compare(paths,self.root);output=self.root/'delta.json'
  comparison.export_delta(paths,exact,output,self.columns)
  delta=json.loads(output.read_text(),parse_float=Decimal)
  return paths,exact,delta,identity.delta_rows(exact,delta)
 def test_one_sided_deletion_retains_the_removed_row_and_signed_phase(self):
  _,exact,delta,phases=self.evidence([], [self.item])
  self.assertFalse(exact['equal_changes'])
  self.assertEqual([entry['phase'] for entry in delta['sides']['record']],['before'])
  self.assertEqual(phases['before']['record']['items'],[self.item['row']])
  self.assertEqual(phases['after']['record'],{})
  fields=identity.field_comparison(phases,self.columns,{})
  self.assertEqual(fields['unpaired_rows'][0]['left_count'],1)
  self.assertEqual(fields['unpaired_rows'][0]['right_count'],0)
 def test_changed_versions_dates_and_dependencies_have_exact_field_paths(self):
  left={'table':'items','row':dict(self.item['row'],version=2)}
  right={'table':'items','row':dict(self.item['row'],version=3,deadline='2030-01-02',dependencies=[{'id':'claim','version':2}])}
  _,_,_,phases=self.evidence([left],[right])
  report=identity.field_comparison(phases,self.columns,{})
  self.assertEqual(report['paired_rows'],1)
  self.assertEqual([d['path'] for d in report['field_differences'][0]['differences']],
                   [['deadline'],['dependencies',0,'version'],['version']])
  self.assertEqual(report['excluded_fields'],[])
 def test_duplicate_occurrences_are_retained_and_never_paired_by_order(self):
  left={'table':'items','row':dict(self.item['row'],version=2)}
  _,_,delta,phases=self.evidence([left,left],[left])
  self.assertEqual(len(delta['sides']['record']),2)
  self.assertEqual(len(delta['sides']['replay']),1)
  report=identity.field_comparison(phases,self.columns,{})
  self.assertEqual(report['paired_rows'],0)
  self.assertEqual(report['unpaired_rows'][0]['left_count'],2)
  self.assertEqual(report['unpaired_rows'][0]['right_count'],1)
 def test_wrong_sign_missing_rows_and_changed_hashes_are_rejected(self):
  _,exact,delta,_=self.evidence([], [self.item])
  for change in ['phase','missing','digest','binding']:
   damaged=json.loads(json.dumps(delta))
   if change=='phase':damaged['sides']['record'][0]['phase']='after'
   elif change=='missing':damaged['sides']['record']=[]
   elif change=='digest':damaged['sides']['record'][0]['row_sha256']='wrong'
   else:damaged['exact_comparison_sha256']='wrong'
   with self.subTest(change=change),self.assertRaises(ValueError):identity.delta_rows(exact,damaged)
 def test_an_unchanged_phase_mutated_after_comparison_cannot_publish_delta(self):
  paths=self.snapshots([self.item],[self.item]);exact=comparison.compare(paths,self.root)
  with gzip.open(paths['left','before'],'wt') as file:file.write(json.dumps(self.owner)+'\n')
  output=self.root/'changed-delta.json'
  with self.assertRaisesRegex(ValueError,'snapshot changed'):comparison.export_delta(paths,exact,output,self.columns)
  self.assertFalse(output.exists())
 def test_empty_primary_key_metadata_is_visible_and_missing_columns_fail(self):
  row={'table':'items','row':dict(self.item['row'],version=2)}
  _,_,_,phases=self.evidence([row],[self.item])
  report=identity.field_comparison(phases,[],{})
  self.assertTrue(report['unpaired_rows'])
  with self.assertRaisesRegex(ValueError,'columns differ'):identity.field_comparison(phases,self.columns[:-1],{})
 def test_private_typed_values_keep_decimal_precision_array_order_and_field_presence(self):
  differences=list(identity.value_differences(
   {'cost':Decimal('0.123456789123456789'),'refs':['a','b'],'status':False},
   {'cost':Decimal('0.123456789123456790'),'refs':['b','a'],'status':0,'missing':None}))
  self.assertEqual([d['path'] for d in differences],[['cost'],['missing'],['refs',0],['refs',1],['status']])
  self.assertEqual(differences[0]['left_value'],['decimal','0.123456789123456789'])
  self.assertFalse(differences[1]['left_present']);self.assertEqual(differences[1]['right_value'],['null'])
  self.assertEqual(differences[-1]['left_value'],['boolean',False]);self.assertEqual(differences[-1]['right_value'],['integer','0'])
 def test_failed_publication_preserves_good_evidence_and_leaves_no_partial_result(self):
  good=self.root/'good.json';good.write_text('good');good.chmod(0o600)
  with self.assertRaises(ValueError):
   with comparison.new_private_file(good) as stream:stream.write('bad')
  self.assertEqual(good.read_text(),'good')
  failed=self.root/'failed.json'
  with self.assertRaises(RuntimeError):
   with comparison.new_private_file(failed) as stream:stream.write('partial');raise RuntimeError('fictional failure')
  self.assertFalse(failed.exists())
 def test_missing_snapshot_cannot_be_accepted_as_zero_changes(self):
  paths=self.snapshots([self.item],[self.item]);paths.pop(('right','after'))
  with self.assertRaisesRegex(ValueError,'four'):comparison.compare(paths,self.root)
 def test_command_reports_signed_deletion_and_rejects_incomplete_historical_metadata(self):
  paths,exact,delta,_=self.evidence([], [self.item])
  full_columns=[*self.columns,{'table':'owners','column':'id','type':'uuid','primary_key':True}]
  delta['schema']=full_columns
  exact_path=self.root/'exact.json';exact_path.write_text(json.dumps(exact));exact_path.chmod(0o600)
  delta_path=self.root/'delta.json';delta_path.write_text(json.dumps(delta));delta_path.chmod(0o600)
  output=self.root/'fields.json'
  command=[sys.executable,str(Path(__file__).with_name('foundation_replay_identity.py')),
           '--delta-rows',str(delta_path),'--exact-comparison',str(exact_path),
           '--initial-snapshot',str(paths['left','before']),
           '--left-after',str(paths['left','after']),'--right-before',str(paths['right','before']),
           '--right-after',str(paths['right','after']),'--output',str(output)]
  result=subprocess.run(command,text=True,capture_output=True)
  self.assertEqual(result.returncode,1,result.stderr)
  report=json.loads(output.read_text());self.assertEqual(report['historical_schema_gaps'],[])
  self.assertTrue(report['full_owner_data_checked'])
  self.assertFalse(report['full_owner_comparison']['equal_changes'])
  self.assertEqual(report['remaining_differences'][0]['phase'],'before')
  self.assertEqual(report['field_comparison']['unpaired_rows'][0]['left_count'],1)
  self.assertFalse(report['state_equivalence_certified']);self.assertEqual(output.stat().st_mode&0o077,0)
  delta['schema']=self.columns;delta_path.write_text(json.dumps(delta));bad_output=self.root/'incomplete.json'
  command[-1]=str(bad_output);result=subprocess.run(command,text=True,capture_output=True)
  self.assertNotEqual(result.returncode,0);self.assertIn('complete initial column metadata',result.stderr)
  self.assertFalse(bad_output.exists())
 def test_runtime_values_and_duplicate_schema_declarations_remain_visible(self):
  left={'table':'items','row':dict(self.item['row'],version=2,updated_at='actual-left')}
  right={'table':'items','row':dict(self.item['row'],version=2,updated_at='actual-right')}
  _,_,_,phases=self.evidence([left],[right])
  columns=[*self.columns,{'table':'items','column':'updated_at','type':'timestamptz','primary_key':False}]
  report=identity.field_comparison(phases,columns,{})
  self.assertEqual(report['field_differences'][0]['differences'][0]['path'],['updated_at'])
  with self.assertRaisesRegex(ValueError,'duplicate column'):identity.field_comparison(phases,[*columns,columns[0]],{})
 def test_comparison_command_publishes_bound_private_delta_and_preserves_existing_results(self):
  changed={'table':'items','row':dict(self.item['row'],version=2)}
  paths=self.snapshots([changed],[self.item]);schema_path=self.root/'catalog.json'
  schema_path.write_text(json.dumps({'schema':self.columns}));schema_path.chmod(0o600)
  output=self.root/'exact.json';delta_path=self.root/'delta.json'
  command=[sys.executable,str(Path(__file__).with_name('foundation_replay_compare.py'))]
  for side in ['left','right']:
   for phase in ['before','after']:command.extend(['--'+side+'-'+phase,str(paths[side,phase])])
  command.extend(['--output',str(output),'--delta-output',str(delta_path),'--schema-evidence',str(schema_path)])
  result=subprocess.run(command,text=True,capture_output=True);self.assertEqual(result.returncode,1,result.stderr)
  exact=json.loads(output.read_text());delta=json.loads(delta_path.read_text())
  self.assertEqual(delta['exact_comparison_sha256'],comparison.fingerprint(exact))
  identity.delta_rows(exact,delta)
  self.assertEqual(output.stat().st_mode&0o077,0);self.assertEqual(delta_path.stat().st_mode&0o077,0)
  previous=output.read_bytes(),delta_path.read_bytes()
  result=subprocess.run(command,text=True,capture_output=True);self.assertNotEqual(result.returncode,0)
  self.assertEqual((output.read_bytes(),delta_path.read_bytes()),previous)
 def test_mapping_must_check_references_in_otherwise_identical_rows(self):
  self.owner['row']['related_id']='generated-left'
  left={'table':'items','row':dict(self.item['row'],id='generated-left')}
  right={'table':'items','row':dict(self.item['row'],id='generated-right')}
  paths=self.snapshots([left],[right])
  # Make the new reference appear only after the baseline. Its equal raw value
  # is absent from mismatched delta rows, but mapping it changes one side.
  for side in ['left','right']:
   with gzip.open(paths[side,'before'],'wt') as file:
    for row in [{'table':'owners','row':{'id':'owner'}},self.item]:file.write(json.dumps(row)+'\n')
  exact=comparison.compare(paths,self.root)
  self.assertNotIn('owners',{row['table'] for row in exact['delta_differences']})
  schema={'items':{'id'},'owners':{'related_id'}};mapping={'generated-left':'generated-right'}
  full=comparison.compare(paths,self.root,
   lambda side,phase,table,row:identity.transform(table,row,mapping if side=='left' else {},schema))
  self.assertFalse(full['equal_changes'])
  self.assertIn('owners',{row['table'] for row in full['delta_differences']})
  self.assertEqual(full['comparison'],'identity_mapped_owner_data')
  self.assertEqual(full['snapshots'],exact['snapshots'])
if __name__=='__main__':unittest.main()
