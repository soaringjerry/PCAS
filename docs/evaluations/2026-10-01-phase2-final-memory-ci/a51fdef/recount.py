import re,json,hashlib,sys
from pathlib import Path
from collections import Counter
raw_path=Path(sys.argv[1]); raw=raw_path.read_text(); result=[]; run=[]; terminals=[]; notest=[]; in_test=False; cmds=[]
for line in raw.splitlines():
 if '\tRun make check\t' not in line: continue
 msg=re.sub(r'^\S+Z ','',line.split('\t',2)[2])
 if msg.startswith(('go vet ','go test ','go build ')): cmds.append(msg)
 if msg.startswith('go test '): in_test=True;continue
 if msg.startswith('go build '): in_test=False
 if not in_test: continue
 m=re.match(r'=== RUN\s+(\S+)',msg)
 if m:run.append(m[1])
 m=re.match(r'\s*--- (PASS|FAIL|SKIP): (\S+) \(([^)]+)\)',msg)
 if m:terminals.append({'test':m[2],'status':m[1].lower(),'duration':m[3]})
 m=re.match(r'\?\s+(github\.com/soaringjerry/PCAS/\S+)\s+\[no test files\]',msg)
 if m:notest.append(m[1])
 m=re.match(r'(ok\s+|FAIL\s+)(github\.com/soaringjerry/PCAS/\S+)\s+(.+)',msg)
 if not m:continue
 names=set(run);tops=[x for x in terminals if '/' not in x['test']];leaves=[x for x in terminals if not any(y.startswith(x['test']+'/') for y in names)]
 result.append({'package':m[2],'package_status':m[1].strip(),'elapsed_or_cached':m[3],'cached':'cached' in m[3],'actual_run_tests':run,'actual_terminals':terminals,'top_counts':dict(Counter(x['status'] for x in tops)),'true_leaf_counts':dict(Counter(x['status'] for x in leaves)),'true_leaf_terminals':leaves,'skips':[x['test'] for x in leaves if x['status']=='skip'],'started_without_terminal':sorted(names-set(x['test'] for x in terminals))})
 run=[];terminals=[]
summary={'raw_sha256':hashlib.sha256(raw_path.read_bytes()).hexdigest(),'raw_lines':len(raw.splitlines()),'commands':cmds,'test_packages':result,'no_test_file_packages':notest,'overall_top_counts':{k:sum(b['top_counts'].get(k,0) for b in result) for k in ['pass','fail','skip']},'overall_true_leaf_counts':{k:sum(b['true_leaf_counts'].get(k,0) for b in result) for k in ['pass','fail','skip']},'cached_packages':[b['package'] for b in result if b['cached']],'race_warning':'WARNING: DATA RACE' in raw,'panic_lines':[line for line in raw.splitlines() if re.search(r'Z panic:',line)],'exit_error_lines':[line for line in raw.splitlines() if 'Process completed with exit code' in line],'unassigned_test_events':{'run':run,'terminals':terminals},'count_rule':'A terminal Test is a true leaf iff no actually started Test in the same package has its name as a proper slash-prefix. Package terminals and all ancestors are excluded.'}
print(json.dumps(summary,ensure_ascii=False,indent=2))
