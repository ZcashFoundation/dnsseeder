package zcash

import (
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/btcsuite/btcd/wire"
)

type Address struct {
	netaddr   *wire.NetAddress
	valid     bool
	blacklist bool
	lastTried time.Time
}

func NewAddress(na *wire.NetAddress) *Address {
	return &Address{
		netaddr:   na,
		valid:     true,
		blacklist: false,
		lastTried: time.Now(),
	}
}

func (a *Address) IsGood() bool {
	return a.valid && !a.blacklist
}

func (a *Address) IsBad() bool {
	return a.blacklist
}

func (a *Address) String() string {
	portString := strconv.Itoa(int(a.netaddr.Port))
	return net.JoinHostPort(a.netaddr.IP.String(), portString)
}

func (a *Address) asPeerKey() PeerKey {
	return PeerKey(a.String())
}

func (a *Address) MarshalText() (text []byte, err error) {
	return []byte(a.String()), nil
}

type AddressBook struct {
	addrList     []*Address
	addrState    sync.RWMutex
	addrRecvCond *sync.Cond
}

func (bk *AddressBook) Add(newAddr *Address) {
	bk.addrState.Lock()
	bk.addrList = append(bk.addrList, newAddr)
	bk.addrState.Unlock()
}

func (bk *AddressBook) Blacklist(addr PeerKey) {
	bk.addrState.Lock()
	for i := 0; i < len(bk.addrList); i++ {
		address := bk.addrList[i]
		if address.asPeerKey() == addr {
			address.valid = false
			address.blacklist = true
		}
	}
	bk.addrState.Unlock()
}

func (bk *AddressBook) AlreadyKnowsAddress(na *wire.NetAddress) bool {
	bk.addrState.RLock()
	defer bk.addrState.RUnlock()

	addr := NewAddress(na)

	for i := 0; i < len(bk.addrList); i++ {
		if bk.addrList[i].String() == addr.String() {
			return true
		}
	}
	return false
}

func (bk *AddressBook) IsBlacklistedAddress(na *wire.NetAddress) bool {
	bk.addrState.RLock()
	defer bk.addrState.RUnlock()

	ref := NewAddress(na)

	for i := 0; i < len(bk.addrList); i++ {
		if bk.addrList[i].String() == ref.String() {
			return bk.addrList[i].IsBad()
		}
	}

	return false
}

func (bk *AddressBook) UpdateAddressState(update *Address) {
	bk.addrState.Lock()
	defer bk.addrState.Unlock()

	for i := 0; i < len(bk.addrList); i++ {
		if bk.addrList[i].String() == update.String() {
			bk.addrList[i].valid = update.valid
			bk.addrList[i].blacklist = update.blacklist
			bk.addrList[i].lastTried = update.lastTried
			return
		}
	}
}

func NewAddressBook(capacity int) *AddressBook {
	addrBook := &AddressBook{
		addrList: make([]*Address, 0, capacity),
	}
	addrBook.addrRecvCond = sync.NewCond(&addrBook.addrState)
	return addrBook
}

// GetShuffledAddressList returns a slice of n valid addresses in random order.
func (ab *AddressBook) GetShuffledAddressList(n int) []*Address { return nil }
