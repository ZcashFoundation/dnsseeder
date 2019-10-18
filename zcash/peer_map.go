package zcash

import (
	"net"
	"strconv"
	"sync"

	"github.com/btcsuite/btcd/peer"
	"github.com/btcsuite/btcd/wire"
)

// The "host:port" format used throughout our maps and lists.
type PeerKey string

func peerKeyFromPeer(p *peer.Peer) PeerKey {
	return PeerKey(p.Addr())
}

func peerKeyFromNA(na *wire.NetAddress) PeerKey {
	portString := strconv.Itoa(int(na.Port))
	return PeerKey(net.JoinHostPort(na.IP.String(), portString))
}

// PeerMap is a typed wrapper for a sync.Map. Its keys are PeerKeys (host:port
// format strings) and it stores a pointer to a btcsuite peer.Peer.
type PeerMap struct {
	m *sync.Map
}

// NewPeerMap returns a fresh, empty PeerMap.
func NewPeerMap() *PeerMap {
	return &PeerMap{
		m: new(sync.Map),
	}
}

// Load returns the value stored in the map for a key, or nil if no value is
// present. The ok result indicates whether value was found in the map.
func (pm *PeerMap) Load(key PeerKey) (*peer.Peer, bool) {
	v, mapOk := pm.m.Load(key)
	if mapOk {
		p, typeOk := v.(*peer.Peer)
		if typeOK {
			return p, true
		}
	}
	return nil, false
}

// LoadOrStore returns the existing value for the key if present. Otherwise, it
// stores and returns the given value. The loaded result is true if the value
// was loaded, false if stored.
func (pm *PeerMap) LoadOrStore(key PeerKey, value *peer.Peer) (*peer.Peer, bool) {
	v, loaded := pm.m.LoadOrStore(key, value)
	p, _ := v.(*peer.Peer)
	return p, loaded
}

// Store sets the value for a key.
func (pm *PeerMap) Store(key PeerKey, value *peer.Peer) {
	pm.m.Store(key, value)
}

// Delete deletes the value for a key.
func (pm *PeerMap) Delete(key PeerKey) {
	pm.m.Delete(key)
}

// Range calls f sequentially for each key and value present in the map. If f
// returns false, range stops the iteration.
//
// Range does not necessarily correspond to any consistent snapshot of the
// Map's contents: no key will be visited more than once, but if the value for
// any key is stored or deleted concurrently, Range may reflect any mapping for
// that key from any point during the Range call.
//
// Range may be O(N) with the number of elements in the map even if f returns
// false after a constant number of calls.
func (pm *PeerMap) Range(f func(key PeerKey, value *peer.Peer) bool) {
	pm.m.Range(f)
}
