package tickerdualpool

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func TestRuntimeFingerprintChecksInstructionsAndAllowsImmutables(t *testing.T) {
	executable := make([]byte, 64)
	executable[0] = 0x60
	fingerprint := runtimeFingerprint{Size: 64, Hash: crypto.Keccak256Hash(executable), ImmutableOffsets: []int{16}}
	code := append(append([]byte(nil), executable...), 0xa0, 0x00, 0x01)
	code[20] = 123
	require.True(t, fingerprint.matches(code))
	code[0] ^= 1
	require.False(t, fingerprint.matches(code), "changed executable logic must be rejected")
	require.False(t, fingerprint.matches(nil))
	require.False(t, fingerprint.matches([]byte{0xff, 0xff}))
	fingerprint.ImmutableOffsets = []int{40}
	require.False(t, fingerprint.matches(code))
}

// Source-path changes alter Solidity's metadata, not this proxy's executable.
func TestProxyFingerprintAllowsMetadataButRejectsChangedInstructions(t *testing.T) {
	code := common.FromHex("0x60806040527f360894a13ba1a3210667c828492db98dca3e2076cc3735a920a3ca505d382bbc545f9081906001600160a01b0316368280378136915af43d5f803e156048573d5ff35b3d5ffdfea264697066735822122044c8b9786344fee6be53958ad44cfa4e69b9f47919040c0a4950bfcf47095b0f64736f6c634300081a0033")
	require.True(t, tickerProxyFingerprint.matches(code))
	code[90] ^= 1
	require.True(t, tickerProxyFingerprint.matches(code), "metadata is not executable")
	code[20] ^= 1
	require.False(t, tickerProxyFingerprint.matches(code), "implementation-slot instructions must match")
}

func TestMainnetHookFingerprintDiffIsOnlyReviewedConstants(t *testing.T) {
	data, err := os.ReadFile("testdata/mainnet-hook-runtime.hex")
	require.NoError(t, err)
	code := common.FromHex(strings.TrimSpace(string(data)))
	require.True(t, tickerMainnetHookFingerprint.matches(code))
	require.True(t, matchesTickerHook(code))
	require.False(t, tickerHookFingerprint.matches(code))

	// This exact conversion must recover the original executable fingerprint.
	currentSlot := common.FromHex("0x9d5f025c8c7bdc5ea69933eeff310f0406e1cbf4d3bbbe1112d62375f8c06642")
	oldSlot := common.FromHex("0x366070eb58711d20384d9d2f2aa4484e0a329bcdeb76471b0d13da4908f57f68")
	currentTopic := crypto.Keccak256([]byte("PoolBuybackBurned(bytes32,uint256,uint256)"))
	oldTopic := crypto.Keccak256([]byte("PoolBuybackLocked(bytes32,uint256,uint256)"))
	require.Equal(t, 3, bytes.Count(code, currentSlot))
	require.Equal(t, 1, bytes.Count(code, currentTopic))
	previous := bytes.ReplaceAll(code, currentSlot, oldSlot)
	previous = bytes.ReplaceAll(previous, currentTopic, oldTopic)
	require.True(t, tickerHookFingerprint.matches(previous))
	require.True(t, matchesTickerHook(previous), "older implementation remains supported")

	changed := append([]byte(nil), code...)
	changed[tickerMainnetHookFingerprint.ImmutableOffsets[0]] ^= 1
	require.True(t, matchesTickerHook(changed), "constructor immutables are separately checked by tracker")
	changed[0] ^= 1
	require.False(t, matchesTickerHook(changed), "unreviewed executable changes stay rejected")
}
