package zcash

import (
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/btcsuite/btcd/peer"
	"github.com/btcsuite/btcd/wire"

	"github.com/gtank/coredns-zcash/zcash/network"

	"github.com/pkg/errors"
)

var (
	ErrRepeatConnection = errors.New("attempted repeat connection to existing peer")
	ErrNoSuchPeer       = errors.New("no record of requested peer")
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

	handshakeSignals *sync.Map
	pendingPeers     *sync.Map
	livePeers        *sync.Map

	addrRecvChan chan *wire.NetAddress

	// For mutating the above
	peerState sync.RWMutex
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
		addrRecvChan:     make(chan *wire.NetAddress, 100),
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
		addrRecvChan:     make(chan *wire.NetAddress, 100),
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
		return
	}

	s.logger.Printf("Got verack from peer without a callback channel: %s", p.Addr())
}

// ConnectToPeer attempts to connect to a peer on the default port at the
// specified address. It returns either a live peer connection or an error.
func (s *Seeder) ConnectToPeer(addr string) error {
	connectionString := net.JoinHostPort(addr, s.config.ChainParams.DefaultPort)

	p, err := peer.NewOutboundPeer(s.config, connectionString)
	if err != nil {
		return errors.Wrap(err, "constructing outbound peer")
	}

	_, alreadyPending := s.pendingPeers.LoadOrStore(p.Addr(), p)
	_, alreadyHandshaking := s.handshakeSignals.LoadOrStore(p.Addr(), make(chan struct{}, 1))
	_, alreadyLive := s.livePeers.Load(p.Addr())

	if alreadyPending || alreadyHandshaking || alreadyLive {
		s.logger.Printf("Attempted repeat connection to peer %s", p.Addr())
		return ErrRepeatConnection
	}

	conn, err := net.Dial("tcp", p.Addr())
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
	case <-time.After(time.Second * 1):
		return errors.New("peer handshake timed out")
	}

	panic("This should be unreachable")
}

func (s *Seeder) GetPeer(addr string) (*peer.Peer, error) {
	lookupKey := net.JoinHostPort(addr, s.config.ChainParams.DefaultPort)
	p, ok := s.livePeers.Load(lookupKey)

	if ok {
		return p.(*peer.Peer), nil
	}

	return nil, ErrNoSuchPeer
}

func (s *Seeder) DisconnectPeer(addr string) error {
	lookupKey := net.JoinHostPort(addr, s.config.ChainParams.DefaultPort)
	p, ok := s.livePeers.Load(lookupKey)

	if !ok {
		return ErrNoSuchPeer
	}

	// TODO: type safety and error handling

	v := p.(*peer.Peer)
	v.Disconnect()
	v.WaitForDisconnect()
	s.livePeers.Delete(lookupKey)

	return nil
}

func (s *Seeder) DisconnectAllPeers() {
	s.pendingPeers.Range(func(key, value interface{}) bool {
		p, ok := value.(*peer.Peer)
		if !ok {
			s.logger.Printf("Invalid peer in pendingPeers")
			return false
		}
		s.logger.Printf("Disconnecting from pending peer %s", p.Addr())
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
		s.logger.Printf("Disconnecting from live peer %s", p.Addr())
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

func (s *Seeder) WaitForMoreAddresses() {
	<-s.addrRecvChan
}

func (s *Seeder) onAddr(p *peer.Peer, msg *wire.MsgAddr) {
	if len(msg.AddrList) == 0 {
		s.logger.Printf("Got empty addr message from peer %s. Disconnecting.", p.Addr())
		s.DisconnectPeer(p.Addr())
		return
	}

	s.logger.Printf("Got %d addrs from peer %s", len(msg.AddrList), p.Addr())
	for _, addr := range msg.AddrList {
		s.addrRecvChan <- addr
	}
}
