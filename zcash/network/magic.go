package network

import (
	"encoding/binary"
	"errors"
)

/// Network represents the byte sequences used to identify different Zcash networks.
type Network uint32

const (
	// Mainnet identifies the Zcash mainnet
	Mainnet Network = 0x6427e924
	// Testnet identifies ECC's public testnet
	Testnet Network = 0xbff91afa
	// Regtest identifies the regression test network
	Regtest Network = 0x5f3fe8aa
)

var (
	ErrInvalidMagic = errors.New("invalid network magic")
)

// Marshal appends the 4-byte, little endian encoding of a network identifier
// to the dst slice and returns the resulting slice. If there is sufficient room
// in the dst slice, Marshal does not allocate.
func (m Network) Marshal(dst []byte) (out []byte) {
	if n := len(dst) + 4; cap(dst) >= n {
		out = dst[:n]
	} else {
		out = make([]byte, n)
		copy(out, dst)
	}

	binary.LittleEndian.PutUint32(out[len(dst):], uint32(m))
	return
}

// Decode parses a valid network identifier from a byte slice. It
// returns the identifier on success, zero and an error on failure.
func Decode(data []byte) (Network, error) {
	if len(data) != 4 {
		return 0, ErrInvalidMagic
	}

	number := Network(binary.LittleEndian.Uint32(data))

	switch number {
	case Mainnet:
		return Mainnet, nil
	case Testnet:
		return Testnet, nil
	case Regtest:
		return Regtest, nil
	default:
		return 0, ErrInvalidMagic
	}
}
