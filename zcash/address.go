package zcash

import (
	"net"
	"strconv"
	"time"

	"github.com/btcsuite/btcd/wire"
)

type Address struct {
	netaddr   *wire.NetAddress
	valid     bool
	blacklist bool
	lastTried time.Time
}

func (a *Address) IsGood() bool {
	return a.valid == true
}

func (a *Address) IsBad() bool {
	return a.blacklist == true
}

func (a *Address) String() string {
	portString := strconv.Itoa(int(a.netaddr.Port))
	return net.JoinHostPort(a.netaddr.IP.String(), portString)
}

// Addresses should be sortable by least-recently-tried

type AddrList []*Address

func (list AddrList) Len() int           { return len(list) }
func (list AddrList) Swap(i, j int)      { list[i], list[j] = list[j], list[i] }
func (list AddrList) Less(i, j int) bool { return list[i].lastTried.Before(list[j].lastTried) }

// GetShuffledAddressList returns a slice of n valid addresses in random order.
func GetShuffledAddressList(addrList []*Address, n int) []*Address { return nil }
