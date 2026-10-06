#!/usr/bin/env python3
"""Estimate test-router swaps on the loaded loopback fixture; never broadcasts."""
import json
import subprocess
import urllib.request
from pathlib import Path

URL = 'http://127.0.0.1:18545'
manifest = json.loads(Path('/tmp/ticker-kyber-rpc-manifest.json').read_text())
def rpc(method, params):
    req = urllib.request.Request(URL, json.dumps(dict(jsonrpc='2.0', id=1, method=method, params=params)).encode(), {'Content-Type':'application/json'})
    with urllib.request.urlopen(req, timeout=60) as response:
        value=json.load(response)
    if 'error' in value: raise RuntimeError(value['error'])
    return value['result']
assert 'anvil' in rpc('web3_clientVersion', []).lower()
assert int(rpc('eth_chainId', []),16)==143
assert int(rpc('eth_blockNumber', []),16)==111021450
key=f"({manifest['token0']},{manifest['token1']},2000,10,{manifest['hook']})"
sig='swap((address,address,uint24,int24,address),(bool,int256,uint160),(bool,bool),bytes)'
# A local balance override funds only gas estimation; there is no transaction send.
rpc('anvil_setBalance',[manifest['payer'],hex(10**20)])
results=[]
for direction,amount in [(True,100*10**6),(False,1000*10**18),(True,1000*10**6),(False,100000*10**18)]:
    limit=4295128740 if direction else 1461446703485210103287273052203988822378723970341
    data=subprocess.check_output(['cast','calldata',sig,key,f'({str(direction).lower()},-{amount},{limit})','(false,false)','0x'],text=True).strip()
    tx=dict(to=manifest['router'],data=data,**{'from':manifest['payer']})
    result=rpc('eth_call',[tx,'latest'])
    estimate=int(rpc('eth_estimateGas',[tx]),16)
    results.append(dict(zeroForOne=direction,input=str(amount),estimatedGas=estimate,returnData=result))
print(json.dumps(dict(block=111021450,router=manifest['router'],measurements=results),indent=2))
