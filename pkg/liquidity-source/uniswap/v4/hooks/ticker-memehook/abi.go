package tickermemehook

import (
	"bytes"
	_ "embed"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

//go:embed abis/TickerMemeHook.json
var hookABIJson []byte

var hookABI abi.ABI

func init() {
	var err error
	if hookABI, err = abi.JSON(bytes.NewReader(hookABIJson)); err != nil {
		panic(err)
	}
}
