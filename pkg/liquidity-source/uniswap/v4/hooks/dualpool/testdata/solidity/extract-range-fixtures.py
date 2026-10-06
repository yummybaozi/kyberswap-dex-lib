#!/usr/bin/env python3
"""Extract the executed Solidity range cases from forge -vv output.

Usage: extract-range-fixtures.py /path/to/forge.log /path/to/range-quotes.json
Fails on failed tests, missing cases, or truncated/non-numeric fixture output.
"""
import json
from pathlib import Path
import sys

source, destination = map(Path, sys.argv[1:])
lines = source.read_text().splitlines()
if '[FAIL' in source.read_text() or '[PASS] test_exportRangeConfigurations' not in source.read_text():
    raise SystemExit('Solidity fixture did not pass')
keys = 'config side input used net fee tax price tick after0 after1'.split()
rows=[]
for i,line in enumerate(lines):
    if line.strip().startswith('RANGES config,'):
        values=[v.strip() for v in lines[i+1:i+1+len(keys)]]
        if len(values)!=len(keys): raise SystemExit('truncated fixture')
        for v in values: int(v)
        rows.append(dict(zip(keys,values)))
if len(rows)!=18: raise SystemExit(f'expected 18 cases, found {len(rows)}')
destination.write_text(json.dumps(rows,indent=2)+'\n')
print(f'Wrote {len(rows)} executed cases to {destination}')
