package network

import (
	"bytes"
	"testing"
)

func TestMainnetMagic(t *testing.T) {
	// Zcash mainnet, src/chainparams.cpp
	var pchMessageStart [4]byte
	pchMessageStart[0] = 0x24
	pchMessageStart[1] = 0xe9
	pchMessageStart[2] = 0x27
	pchMessageStart[3] = 0x64

	magicBytes := Mainnet.Marshal(nil)

	if !bytes.Equal(magicBytes, pchMessageStart[:]) {
		t.Error("encoding failed")
	}

	magic, err := Decode(pchMessageStart[:])

	if err != nil || magic != Mainnet {
		t.Error("decoding failed")
	}
}
