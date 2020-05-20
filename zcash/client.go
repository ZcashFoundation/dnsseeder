package zcash

import (
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/btcsuite/btcd/peer"
	"github.com/btcsuite/btcd/wire"

	"github.com/zcashfoundation/dnsseeder/zcash/network"

	"github.com/pkg/errors"
)

var (
	ErrRepeatConnection = errors.New("attempted repeat connection to existing peer")
	ErrNoSuchPeer       = errors.New("no record of requested peer")
	ErrAddressTimeout   = errors.New("wait for addresses timed out")
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
	pendingPeers     *PeerMap
	livePeers        *PeerMap

	// The set of known addresses
	addrBook *AddressBook
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
		pendingPeers:     NewPeerMap(),
		livePeers:        NewPeerMap(),
		addrBook:         NewAddressBook(1000),
	}

	newSeeder.config.Listeners.OnVerAck = newSeeder.onVerAck
	newSeeder.config.Listeners.OnAddr = newSeeder.onAddr

	return &newSeeder, nil
}

func newTestSeeder(network network.Network) (*Seeder, error) {
	config, err := newSeederPeerConfig(network, defaultPeerConfig)
	if err != nil {
		return nil, errors.Wrap(err, "could not construct seeder")
	}

	// sink, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0666)
	// logger := log.New(sink, "zcash_seeder: ", log.Ldate|log.Ltime|log.Lshortfile|log.LUTC)
	logger := log.New(os.Stdout, "zcash_seeder: ", log.Ldate|log.Ltime|log.Lshortfile|log.LUTC)

	// Allows connections to self for easy mocking
	config.AllowSelfConns = true

	newSeeder := Seeder{
		config:           config,
		logger:           logger,
		handshakeSignals: new(sync.Map),
		pendingPeers:     NewPeerMap(),
		livePeers:        NewPeerMap(),
		addrBook:         NewAddressBook(1000),
	}

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

// ConnectOnDefaultPort attempts to connect to a peer on the default port at the
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

	if s.addrBook.IsBlacklistedAddress(p.NA()) {
		return ErrBlacklistedPeer
	}

	_, alreadyPending := s.pendingPeers.Load(peerKeyFromPeer(p))
	_, alreadyHandshaking := s.handshakeSignals.Load(peerKeyFromPeer(p))
	_, alreadyLive := s.livePeers.Load(peerKeyFromPeer(p))

	if alreadyPending {
		s.logger.Printf("Peer is already pending: %s", p.Addr())
		return ErrRepeatConnection
	}
	s.pendingPeers.Store(peerKeyFromPeer(p), p)

	if alreadyHandshaking {
		s.logger.Printf("Peer is already handshaking: %s", p.Addr())
		return ErrRepeatConnection
	}
	s.handshakeSignals.Store(p.Addr(), make(chan struct{}, 1))

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
func (s *Seeder) GetPeer(addr PeerKey) (*peer.Peer, error) {
	p, ok := s.livePeers.Load(addr)

	if ok {
		return p, nil
	}

	return nil, ErrNoSuchPeer
}

// DisconnectPeer disconnects from a live peer identified by "host:port"
// string. It returns an error if we aren't connected to that peer.
func (s *Seeder) DisconnectPeer(addr PeerKey) error {
	p, ok := s.livePeers.Load(addr)

	if !ok {
		return ErrNoSuchPeer
	}

	s.logger.Printf("Disconnecting from peer %s", p.Addr())
	p.Disconnect()
	p.WaitForDisconnect()
	s.livePeers.Delete(addr)
	return nil
}

// DisconnectPeerDishonorably disconnects from a live peer identified by
// "host:port" string. It returns an error if we aren't connected to that peer.
// "Dishonorably" furthermore removes this peer from the list of known good
// addresses and adds them to a blacklist.
func (s *Seeder) DisconnectPeerDishonorably(addr PeerKey) error {
	p, ok := s.livePeers.Load(addr)

	if !ok {
		return ErrNoSuchPeer
	}

	s.logger.Printf("Disconnecting from peer %s", addr)
	p.Disconnect()
	p.WaitForDisconnect()

	// Remove from live peer set
	s.livePeers.Delete(addr)

	// Never connect to them again
	s.logger.Printf("Blacklisting peer %s", addr)
	s.addrBook.Blacklist(addr)
	return nil
}

// DisconnectAllPeers terminates the connections to all live and pending peers.
func (s *Seeder) DisconnectAllPeers() {
	s.pendingPeers.Range(func(key PeerKey, p *peer.Peer) bool {
		p.Disconnect()
		p.WaitForDisconnect()
		s.pendingPeers.Delete(key)
		return true
	})

	s.livePeers.Range(func(key PeerKey, p *peer.Peer) bool {
		s.DisconnectPeer(key)
		return true
	})
}

func (s *Seeder) RequestAddresses() {
	s.livePeers.Range(func(key PeerKey, p *peer.Peer) bool {
		s.logger.Printf("Requesting addresses from peer %s", p.Addr())
		p.QueueMessage(wire.NewMsgGetAddr(), nil)
		return true
	})
}

// WaitForAddresses waits for n addresses to be received and their initial
// connection attempts to resolve. There is no escape if that does not happen -
// this is intended for test runners.
func (s *Seeder) WaitForAddresses(n int, timeout time.Duration) error {
	done := make(chan struct{})
	go s.addrBook.waitForAddresses(n, done)
	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return ErrAddressTimeout
	}
}
