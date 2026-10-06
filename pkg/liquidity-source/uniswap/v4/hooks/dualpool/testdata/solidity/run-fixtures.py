#!/usr/bin/env python3
"""Run local-only Foundry fixtures against the pinned Ticker checkout.

Usage: python3 run-fixtures.py /path/to/ticker-contracts [--fork]
The fork reads Monad RPC state; this script never broadcasts transactions.
"""
import argparse
import json
from pathlib import Path
import re
import shutil
import subprocess
import tempfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("ticker_checkout", type=Path)
parser.add_argument("--fork", action="store_true")
parser.add_argument("--match-test", help="optional Foundry test filter")
args = parser.parse_args()
root = args.ticker_checkout.resolve()
expected = "a8b41e44916d3d6266f8d38f2f3cd116c2379207"
actual = subprocess.check_output(["git", "-C", str(root), "rev-parse", "HEAD"], text=True).strip()
if actual != expected:
    parser.error("Ticker checkout must be pinned to " + expected)
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
    }
    (target / "foundry.toml").write_text("[profile.default]\n" + "\n".join(
        key + " = " + json.dumps(value) for key, value in config.items()) + "\n")
    test = args.match_test or ("test_freshAavePoolTradesBothWays" if args.fork else "test_export")
    subprocess.run(["forge", "test", "--root", str(target), "--match-test", test, "-vv"], check=True)
