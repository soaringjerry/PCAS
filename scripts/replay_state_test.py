#!/usr/bin/env python3
import gzip
import importlib.util
import json
from pathlib import Path
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
if __name__=='__main__':unittest.main()
