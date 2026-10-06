#!/usr/bin/env python3
"""Run local-only Foundry fixtures against the pinned Ticker checkout.

Usage: python3 run-fixtures.py /path/to/ticker-contracts [--fork]
The fork reads Monad RPC state; this script never broadcasts transactions.
"""
import argparse
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("ticker_checkout", type=Path)
parser.add_argument("--fork", action="store_true")
parser.add_argument("--match-test", help="optional Foundry test filter")
parser.add_argument("--export-rpc", action="store_true", help="export the fresh fork for local Anvil")
parser.add_argument("--output-dir", type=Path, default=Path(tempfile.gettempdir()))
args = parser.parse_args()
if args.export_rpc and not args.fork:
    parser.error("--export-rpc requires --fork")
output_dir = args.output_dir.resolve()
if args.export_rpc:
    output_dir.mkdir(parents=True, exist_ok=True)
root = args.ticker_checkout.resolve()
expected = "a8b41e44916d3d6266f8d38f2f3cd116c2379207"
actual = subprocess.check_output(["git", "-C", str(root), "rev-parse", "HEAD"], text=True).strip()
if actual != expected:
    parser.error("Ticker checkout must be pinned to " + expected)
if subprocess.run(["git", "-C", str(root), "diff", "--quiet", "HEAD", "--", "contracts-ticker", "test"]).returncode:
    parser.error("pinned Ticker source/test files have tracked changes")
remaps = re.findall(r'"([^"\n]+/=[^"\n]+)"', (root / "foundry.toml").read_text())
remaps = [x.split("=")[0] + "=" + str(root / x.split("=", 1)[1]) + "/" for x in remaps]
remaps += ["ticker-tests/=" + str(root / "test") + "/",
           "ticker-src/=" + str(root / "contracts-ticker/src") + "/"]
with tempfile.TemporaryDirectory(prefix="ticker-kyber-fixture-") as folder:
    target = Path(folder)
    fixture = "TickerMonadFork.t.sol" if args.fork else "TickerQuoteFixture.t.sol"
    shutil.copy(Path(__file__).parent / fixture, target / fixture)
    config = {
        "src": "src", "test": ".", "out": "out", "cache_path": "cache",
        "solc_version": "0.8.26", "evm_version": "cancun", "optimizer": True,
        "optimizer_runs": 200, "via_ir": True, "code_size_limit": 131072,
        "remappings": remaps,
        "fs_permissions": [{"access":"read-write", "path":str(output_dir)}],
    }
    (target / "foundry.toml").write_text("[profile.default]\n" + "\n".join(
        key + " = " + ('[ { access = "read-write", path = ' + json.dumps(str(output_dir)) + " } ]" if key == "fs_permissions" else json.dumps(value)) for key, value in config.items()) + "\n")
    test = ("test_exportLocalRPCState" if args.export_rpc else args.match_test) or ("test_freshAavePoolTradesBothWays" if args.fork else "test_export")
    env = dict(os.environ)
    if args.export_rpc:
        env["EXPORT_KYBER_FIXTURE"] = "true"
        env["KYBER_FIXTURE_OUTPUT_DIR"] = str(output_dir)
    subprocess.run(["forge", "test", "--root", str(target), "--match-test", test, "-vv"], check=True, env=env)
    if args.export_rpc:
        print("Local fixture exported to", output_dir)
