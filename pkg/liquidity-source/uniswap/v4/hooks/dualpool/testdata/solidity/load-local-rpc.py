#!/usr/bin/env python3
"""Load the exported Foundry fixture into an existing LOOPBACK Anvil Monad fork.

Never use this against a remote node. This script refuses non-loopback URLs,
non-Anvil clients, and any chain/block other than the pinned fixture.
"""
import argparse
import ipaddress
import json
from pathlib import Path
import urllib.parse
import urllib.request

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--rpc", default="http://127.0.0.1:18545")
parser.add_argument("--allocs", type=Path, default=Path("/tmp/ticker-kyber-rpc-allocs.json"))
args = parser.parse_args()
url = urllib.parse.urlparse(args.rpc)
try:
    local = ipaddress.ip_address(url.hostname).is_loopback
except ValueError:
    local = False
if url.scheme != "http" or not local or url.username or url.password:
    parser.error("only a literal loopback HTTP address is allowed")

def rpc(method, params):
    data = json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode()
    req = urllib.request.Request(args.rpc, data, {"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=30) as response:
        result = json.load(response)
    if "error" in result:
        raise RuntimeError(result["error"])
    return result["result"]

if "anvil" not in rpc("web3_clientVersion", []).lower():
    parser.error("the target is not Anvil")
if int(rpc("eth_chainId", []), 16) != 143 or int(rpc("eth_blockNumber", []), 16) != 111021450:
    parser.error("start an Anvil Monad fork pinned to block 111021450")
allocs = json.loads(args.allocs.read_text())
if len(allocs) < 10 or sum(len(v["storage"]) for v in allocs.values()) < 20:
    parser.error("incomplete export: missing deployed accounts or setup storage")
for address, account in allocs.items():
    rpc("anvil_setCode", [address, account["code"]])
    rpc("anvil_setBalance", [address, hex(account["balance"]) if isinstance(account["balance"], int) else account["balance"]])
    rpc("anvil_setNonce", [address, hex(account["nonce"]) if isinstance(account["nonce"], int) else account["nonce"]])
    for slot, value in account["storage"].items():
        rpc("anvil_setStorageAt", [address, slot, value])
print("Loaded", len(allocs), "accounts into local Anvil; no transactions broadcast.")
