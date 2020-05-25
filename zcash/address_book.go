package zcash

import (
	mrand "math/rand"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/btcsuite/btcd/wire"
)

type Address struct {
	netaddr    *wire.NetAddress
	lastUpdate time.Time
}

func (a *Address) String() string {
	portString := strconv.Itoa(int(a.netaddr.Port))
	return net.JoinHostPort(a.netaddr.IP.String(), portString)
}

func (a *Address) asPeerKey() PeerKey {
	return PeerKey(a.String())
}

func (a *Address) fromPeerKey(s PeerKey) (*Address, error) {
	host, portString, err := net.SplitHostPort(s.String())
	if err != nil {
		return nil, err
	}

	portInt, err := strconv.ParseUint(portString, 10, 16)
	if err != nil {
		return nil, err
	}

	na := wire.NewNetAddressTimestamp(
		time.Now(),
		0,
		net.ParseIP(host),
		uint16(portInt),
	)

	a.netaddr = na
	a.lastUpdate = na.Timestamp
	return a, nil
}

func (a *Address) asNetAddress() *wire.NetAddress {
	newNA := *a.netaddr
	newNA.Timestamp = a.lastUpdate
	return &newNA
}

func (a *Address) fromNetAddress(na *wire.NetAddress) (*Address, error) {
	a.netaddr = na
	a.lastUpdate = na.Timestamp
	return a, nil
}

func (a *Address) MarshalText() (text []byte, err error) {
	return []byte(a.String()), nil
}

type AddressBook struct {
	peers     map[PeerKey]*Address
	blacklist map[PeerKey]*Address

	addrState    sync.RWMutex
	addrRecvCond *sync.Cond
}

func NewAddressBook() *AddressBook {
	addrBook := &AddressBook{
		peers:     make(map[PeerKey]*Address),
		blacklist: make(map[PeerKey]*Address),
	}
	addrBook.addrRecvCond = sync.NewCond(&addrBook.addrState)
	return addrBook
}

func (bk *AddressBook) Add(s PeerKey) {
	newAddr, err := (&Address{}).fromPeerKey(s)
	if err != nil {
		// XXX effectively NOP bogus peer strings
		return
	}

	bk.addrState.Lock()
	bk.peers[s] = newAddr
	bk.addrState.Unlock()

	// Wake anyone who was waiting on us to receive an address.
	bk.addrRecvCond.Broadcast()
}

func (bk *AddressBook) Remove(s PeerKey) {
	bk.addrState.Lock()
	defer bk.addrState.Unlock()

	if _, ok := bk.peers[s]; ok {
		delete(bk.peers, s)
	}
}

// Blacklist adds an address to the blacklist so we won't try to connect to it again.
func (bk *AddressBook) Blacklist(s PeerKey) {
	bk.addrState.Lock()
	defer bk.addrState.Unlock()

	if target, ok := bk.peers[s]; ok {
		bk.blacklist[s] = target
		delete(bk.peers, s)
	} else {
		// Create a new Address just to be blacklisted
		addr, err := (&Address{}).fromPeerKey(s)
		if err != nil {
			// XXX effectively NOP bogus peer strings
			return
		}
		bk.blacklist[s] = addr
	}
}

// Redeem removes an address from the blacklist.
func (bk *AddressBook) Redeem(s PeerKey) {
	bk.addrState.Lock()
	defer bk.addrState.Unlock()

	if _, ok := bk.blacklist[s]; ok {
		delete(bk.blacklist, s)
	}
}

// Touch updates the last-seen timestamp if the peer is in the valid address book or does nothing if not.
func (bk *AddressBook) Touch(s PeerKey) {
	bk.addrState.Lock()
	defer bk.addrState.Unlock()

	if target, ok := bk.peers[s]; ok {
		target.lastUpdate = time.Now()
	}
}

func (bk *AddressBook) Count() int {
	bk.addrState.RLock()
	defer bk.addrState.RUnlock()

	return len(bk.peers)
}

// IsKnown returns true if the peer is already in our address book, false if not.
func (bk *AddressBook) IsKnown(s PeerKey) bool {
	bk.addrState.RLock()
	defer bk.addrState.RUnlock()

	_, knownGood := bk.peers[s]
	_, knownBad := bk.blacklist[s]
	return knownGood || knownBad
}

func (bk *AddressBook) IsBlacklisted(s PeerKey) bool {
	bk.addrState.RLock()
	defer bk.addrState.RUnlock()

	_, blacklisted := bk.blacklist[s]
	return blacklisted
}

// enqueueAddrs puts all of our known valid peers to a channel for processing.
func (bk *AddressBook) enqueueAddrs(addrQueue *chan *Address) {
	bk.addrState.RLock()
	defer bk.addrState.RUnlock()

	*addrQueue = make(chan *Address, len(bk.peers))
	for _, v := range bk.peers {
		*addrQueue <- v
	}
}

// WaitForAddresses waits for n addresses to be received and their initial
// connection attempts to resolve. There is no escape if that does not happen -
// this is intended for test runners or goroutines with a timeout.
func (bk *AddressBook) waitForAddresses(n int, done chan struct{}) {
	bk.addrState.Lock()
	for {
		addrCount := len(bk.peers)
		if addrCount < n {
			bk.addrRecvCond.Wait()
		} else {
			break
		}
	}
	bk.addrState.Unlock()
	done <- struct{}{}
	return
}

// GetAddressList returns a slice of n valid addresses in random order.
// If there aren't enough known addresses, it returns as many as we have.
func (bk *AddressBook) shuffleAddressList(n int, v6 bool) []net.IP {
	bk.addrState.RLock()
	defer bk.addrState.RUnlock()

	resp := make([]net.IP, 0, len(bk.peers))

	for k, v := range bk.peers {
		if _, blacklisted := bk.blacklist[k]; blacklisted {
			// Check in case we've accidentally registered a bad peer
			continue
		}

		if v6 && v.netaddr.IP.To4() != nil {
			// skip IPv4 addresses if we're asked for v6
			continue
		}

		if !v6 && v.netaddr.IP.To4() == nil {
			// skip IPv6 addresses if we're asked for v4
			continue
		}

		resp = append(resp, v.netaddr.IP)
	}

	mrand.Seed(time.Now().UnixNano())
	mrand.Shuffle(len(resp), func(i, j int) {
		resp[i], resp[j] = resp[j], resp[i]
	})

	if len(resp) > n {
		return resp[:n]
	}

	return resp
}
