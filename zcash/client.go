package zcash

import (
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/btcsuite/btcd/addrmgr"
	"github.com/btcsuite/btcd/peer"
	"github.com/btcsuite/btcd/wire"

	"github.com/gtank/coredns-zcash/zcash/network"

	"github.com/pkg/errors"
)

var (
	ErrRepeatConnection = errors.New("attempted repeat connection to existing peer")
	ErrNoSuchPeer       = errors.New("no record of requested peer")
	ErrAddressTimeout   = errors.New("wait for addreses timed out")
	ErrBlacklistedPeer  = errors.New("peer is blacklisted")
)

var defaultPeerConfig = &peer.Config{
	UserAgentName:    "MagicBean",
	UserAgentVersion: "2.0.7",
	ChainParams:      nil,
	Services:         0,
	TrickleInterval:  time.Second * 10,
	ProtocolVersion:  170009, // Blossom
}

type Seeder struct {
	peer   *peer.Peer
	config *peer.Config
	logger *log.Logger

	// Peer list handling
	peerState        sync.RWMutex
	handshakeSignals *sync.Map
	pendingPeers     *sync.Map
	livePeers        *sync.Map

	// Address list handling
	addrState    sync.RWMutex
	addrRecvCond *sync.Cond
	addrList     []*Address
}

func NewSeeder(network network.Network) (*Seeder, error) {
	config, err := newSeederPeerConfig(network, defaultPeerConfig)
	if err != nil {
		return nil, errors.Wrap(err, "could not construct seeder")
	}

	logger := log.New(os.Stdout, "zcash_seeder: ", log.Ldate|log.Ltime|log.Lshortfile|log.LUTC)

	newSeeder := Seeder{
		config:           config,
		logger:           logger,
		handshakeSignals: new(sync.Map),
		pendingPeers:     new(sync.Map),
		livePeers:        new(sync.Map),
		addrList:         make([]*Address, 0, 1000),
	}

	newSeeder.addrRecvCond = sync.NewCond(&newSeeder.addrState)

	newSeeder.config.Listeners.OnVerAck = newSeeder.onVerAck
	newSeeder.config.Listeners.OnAddr = newSeeder.onAddr

	return &newSeeder, nil
}

func newTestSeeder(network network.Network) (*Seeder, error) {
	config, err := newSeederPeerConfig(network, defaultPeerConfig)
	if err != nil {
		return nil, errors.Wrap(err, "could not construct seeder")
	}

	sink, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0666)
	logger := log.New(sink, "zcash_seeder: ", log.Ldate|log.Ltime|log.Lshortfile|log.LUTC)

	// Allows connections to self for easy mocking
	config.AllowSelfConns = true

	newSeeder := Seeder{
		config:           config,
		logger:           logger,
		handshakeSignals: new(sync.Map),
		pendingPeers:     new(sync.Map),
		livePeers:        new(sync.Map),
		addrList:         make([]*Address, 0, 1000),
	}

	newSeeder.addrRecvCond = sync.NewCond(&newSeeder.addrState)

	newSeeder.config.Listeners.OnVerAck = newSeeder.onVerAck
	newSeeder.config.Listeners.OnAddr = newSeeder.onAddr

	return &newSeeder, nil
}

func newSeederPeerConfig(magic network.Network, template *peer.Config) (*peer.Config, error) {
	var newPeerConfig peer.Config

	// Load the default values
	if template != nil {
		newPeerConfig = *template
	}

	params, err := network.GetNetworkParams(magic)
	if err != nil {
		return nil, errors.Wrap(err, "couldn't construct peer config")
	}
	newPeerConfig.ChainParams = params

	return &newPeerConfig, nil
}

// GetNetworkDefaultPort returns the default port of the network this seeder is configured for.
func (s *Seeder) GetNetworkDefaultPort() string {
	return s.config.ChainParams.DefaultPort
}

func (s *Seeder) onVerAck(p *peer.Peer, msg *wire.MsgVerAck) {
	// Check if we're expecting to hear from this peer
	_, ok := s.pendingPeers.Load(p.Addr())

	if !ok {
		s.logger.Printf("Got verack from unexpected peer %s", p.Addr())
		return
	}

	// Add to set of live peers
	s.livePeers.Store(p.Addr(), p)

	// Remove from set of pending peers
	s.pendingPeers.Delete(p.Addr())

	// Signal successful connection
	if signal, ok := s.handshakeSignals.Load(p.Addr()); ok {
		signal.(chan struct{}) <- struct{}{}
	} else {
		s.logger.Printf("Got verack from peer without a callback channel: %s", p.Addr())
		s.DisconnectPeer(p.Addr())
		return
	}

	// Add to list of known good addresses if we don't already have it.
	// Otherwise, update the last-valid time.

	newAddr := &Address{
		netaddr:   p.NA(),
		valid:     true,
		blacklist: false,
		lastTried: time.Now(),
	}

	if s.alreadyKnowsAddress(p.NA()) {
		s.updateAddressState(newAddr)
		return
	}

	s.logger.Printf("Adding %s to address list", p.Addr())

	s.addrState.Lock()
	s.addrList = append(s.addrList, newAddr)
	s.addrState.Unlock()

	return
}

// ConnectToPeer attempts to connect to a peer on the default port at the
// specified address. It returns an error if it can't complete handshake with
// the peer. Otherwise it returns nil and adds the peer to the list of live
// connections and known-good addresses.
func (s *Seeder) ConnectOnDefaultPort(addr string) error {
	return s.Connect(addr, s.config.ChainParams.DefaultPort)
}

func (s *Seeder) Connect(addr, port string) error {
	connectionString := net.JoinHostPort(addr, port)
	p, err := peer.NewOutboundPeer(s.config, connectionString)
	if err != nil {
		return errors.Wrap(err, "constructing outbound peer")
	}

	if s.isBlacklistedAddress(p.NA()) {
		return ErrBlacklistedPeer
	}

	_, alreadyPending := s.pendingPeers.Load(p.Addr())
	_, alreadyHandshaking := s.handshakeSignals.Load(p.Addr())
	_, alreadyLive := s.livePeers.Load(p.Addr())

	if alreadyPending {
		s.logger.Printf("Peer is already pending: %s", p.Addr())
		return ErrRepeatConnection
	} else {
		s.pendingPeers.Store(p.Addr(), p)
	}

	if alreadyHandshaking {
		s.logger.Printf("Peer is already handshaking: %s", p.Addr())
		return ErrRepeatConnection
	} else {
		s.handshakeSignals.Store(p.Addr(), make(chan struct{}, 1))
	}

	if alreadyLive {
		s.logger.Printf("Peer is already live: %s", p.Addr())
		return ErrRepeatConnection
	}

	conn, err := net.DialTimeout("tcp", p.Addr(), 1*time.Second)
	if err != nil {
		return errors.Wrap(err, "dialing new peer address")
	}

	// Begin connection negotiation.
	s.logger.Printf("Handshake initated with new peer %s", p.Addr())
	p.AssociateConnection(conn)

	// TODO: handle disconnect during this
	handshakeChan, _ := s.handshakeSignals.Load(p.Addr())

	select {
	case <-handshakeChan.(chan struct{}):
		s.logger.Printf("Handshake completed with new peer %s", p.Addr())
		s.handshakeSignals.Delete(p.Addr())
		return nil
	case <-time.After(1 * time.Second):
		return errors.New("peer handshake started but timed out")
	}

	panic("This should be unreachable")
}

// GetPeer returns a live peer identified by "host:port" string, or an error if
// we aren't connected to that peer.
func (s *Seeder) GetPeer(addr string) (*peer.Peer, error) {
	p, ok := s.livePeers.Load(addr)

	if ok {
		return p.(*peer.Peer), nil
	}

	return nil, ErrNoSuchPeer
}

// DisconnectPeer disconnects from a live peer identified by "host:port"
// string. It returns an error if we aren't connected to that peer.
func (s *Seeder) DisconnectPeer(addr string) error {
	p, ok := s.livePeers.Load(addr)

	if !ok {
		return ErrNoSuchPeer
	}

	// TODO: type safety and error handling

	v := p.(*peer.Peer)
	s.logger.Printf("Disconnecting from peer %s", v.Addr())
	v.Disconnect()
	v.WaitForDisconnect()
	s.livePeers.Delete(addr)

	return nil
}

// DisconnectPeerDishonorably disconnects from a live peer identified by
// "host:port" string. It returns an error if we aren't connected to that peer.
// "Dishonorably" furthermore removes this peer from the list of known good
// addresses and adds them to a blacklist.
func (s *Seeder) DisconnectPeerDishonorably(addr string) error {
	p, ok := s.livePeers.Load(addr)

	if !ok {
		return ErrNoSuchPeer
	}

	// TODO: type safety and error handling

	v := p.(*peer.Peer)
	s.logger.Printf("Disconnecting from peer %s", v.Addr())
	v.Disconnect()
	v.WaitForDisconnect()
	s.livePeers.Delete(addr)

	s.addrState.Lock()
	for i := 0; i < len(s.addrList); i++ {
		address := s.addrList[i]
		if address.String() == addr {
			s.logger.Printf("Blacklisting peer %s", v.Addr())
			address.valid = false
			address.blacklist = true
		}
	}
	s.addrState.Unlock()

	return nil
}

// DisconnectAllPeers terminates the connections to all live and pending peers.
func (s *Seeder) DisconnectAllPeers() {
	s.pendingPeers.Range(func(key, value interface{}) bool {
		p, ok := value.(*peer.Peer)
		if !ok {
			s.logger.Printf("Invalid peer in pendingPeers")
			return false
		}
		p.Disconnect()
		p.WaitForDisconnect()
		s.pendingPeers.Delete(key)
		return true
	})

	s.livePeers.Range(func(key, value interface{}) bool {
		p, ok := value.(*peer.Peer)
		if !ok {
			s.logger.Printf("Invalid peer in livePeers")
			return false
		}
		s.DisconnectPeer(p.Addr())
		return true
	})
}

func (s *Seeder) RequestAddresses() {
	s.livePeers.Range(func(key, value interface{}) bool {
		p, ok := value.(*peer.Peer)
		if !ok {
			s.logger.Printf("Invalid peer in livePeers")
			return false
		}
		s.logger.Printf("Requesting addresses from peer %s", p.Addr())
		p.QueueMessage(wire.NewMsgGetAddr(), nil)
		return true
	})
}

// WaitForAddresses waits for n addresses to be received and their initial
// connection attempts to resolve. There is no escape if that does not happen -
// this is intended for test runners.
func (s *Seeder) WaitForAddresses(n int) error {
	s.addrState.Lock()
	for {
		addrCount := len(s.addrList)
		if addrCount < n {
			s.addrRecvCond.Wait()
		} else {
			break
		}
	}
	s.addrState.Unlock()
	return nil
}

func (s *Seeder) alreadyKnowsAddress(na *wire.NetAddress) bool {
	s.addrState.RLock()
	defer s.addrState.RUnlock()

	ref := &Address{
		netaddr: na,
	}

	for i := 0; i < len(s.addrList); i++ {
		if s.addrList[i].String() == ref.String() {
			return true
		}
	}

	return false
}

func (s *Seeder) isBlacklistedAddress(na *wire.NetAddress) bool {
	s.addrState.RLock()
	defer s.addrState.RUnlock()

	ref := &Address{
		netaddr: na,
	}

	for i := 0; i < len(s.addrList); i++ {
		if s.addrList[i].String() == ref.String() {
			return s.addrList[i].IsBad()
		}
	}

	return false
}

func (s *Seeder) updateAddressState(update *Address) {
	s.addrState.Lock()
	defer s.addrState.Unlock()

	for i := 0; i < len(s.addrList); i++ {
		if s.addrList[i].String() == update.String() {
			s.addrList[i].valid = update.valid
			s.addrList[i].blacklist = update.blacklist
			s.addrList[i].lastTried = update.lastTried
			return
		}
	}
}

func (s *Seeder) onAddr(p *peer.Peer, msg *wire.MsgAddr) {
	if len(msg.AddrList) == 0 {
		s.logger.Printf("Got empty addr message from peer %s. Disconnecting.", p.Addr())
		s.DisconnectPeer(p.Addr())
		return
	}

	s.logger.Printf("Got %d addrs from peer %s", len(msg.AddrList), p.Addr())

	for _, na := range msg.AddrList {
		s.logger.Printf("Trying %s:%d from peer %s", na.IP, na.Port, p.Addr())
		go func(na *wire.NetAddress) {
			if !addrmgr.IsRoutable(na) && !s.config.AllowSelfConns {
				s.logger.Printf("Got bad addr %s:%d from peer %s", na.IP, na.Port, p.Addr())
				s.DisconnectPeerDishonorably(p.Addr())
				return
			}

			if s.alreadyKnowsAddress(na) {
				s.logger.Printf("Already knew about address %s:%d", na.IP, na.Port)
				return
			}

			if s.isBlacklistedAddress(na) {
				s.logger.Printf("Address %s:%d is blacklisted", na.IP, na.Port)
				return
			}

			portString := strconv.Itoa(int(na.Port))
			err := s.Connect(na.IP.String(), portString)

			if err != nil {
				s.logger.Printf("Got unusable peer %s:%d from peer %s. Error: %s", na.IP, na.Port, p.Addr(), err)

				// Mark previously-known peers as invalid
				newAddr := &Address{
					netaddr:   p.NA(),
					valid:     false,
					lastTried: time.Now(),
				}

				if s.alreadyKnowsAddress(p.NA()) {
					s.updateAddressState(newAddr)
				}

				return
			}

			peerString := net.JoinHostPort(na.IP.String(), portString)
			s.DisconnectPeer(peerString)

			s.addrRecvCond.Broadcast()
		}(na)
	}
}
