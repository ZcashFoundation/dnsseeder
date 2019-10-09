package network

import (
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/wire"
	"github.com/pkg/errors"
)

var (
	// These are not fully valid chainparams, but they'll do for a seeder.
	regtestParams = chaincfg.Params{
		Name:        "regtest",
		Net:         wire.BitcoinNet(Regtest),
		DefaultPort: "18344",
	}

	// These are not fully valid chainparams, but they'll do for a seeder.
	mainnetParams = chaincfg.Params{
		Name:        "mainnet",
		Net:         wire.BitcoinNet(Mainnet),
		DefaultPort: "8233",
	}

	// These are not fully valid chainparams, but they'll do for a seeder.
	testnetParams = chaincfg.Params{
		Name:        "testnet",
		Net:         wire.BitcoinNet(Testnet),
		DefaultPort: "18233",
	}
)

func GetNetworkParams(magic Network) (*chaincfg.Params, error) {
	var cfg chaincfg.Params

	switch magic {
	case Regtest:
		cfg = regtestParams
	case Mainnet:
		cfg = mainnetParams
	case Testnet:
		cfg = testnetParams
	default:
		return nil, errors.Wrap(ErrInvalidMagic, "no network params")
	}

	return &cfg, nil
}
