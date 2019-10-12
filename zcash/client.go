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

	handshakeSignals map[string]chan *peer.Peer
	pendingPeers     map[string]*peer.Peer
	livePeers        map[string]*peer.Peer

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
		handshakeSignals: make(map[string]chan *peer.Peer),
		pendingPeers:     make(map[string]*peer.Peer),
		livePeers:        make(map[string]*peer.Peer),
	}

	newSeeder.config.Listeners.OnVerAck = newSeeder.onVerAck

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
		handshakeSignals: make(map[string]chan *peer.Peer),
		pendingPeers:     make(map[string]*peer.Peer),
		livePeers:        make(map[string]*peer.Peer),
	}

	newSeeder.config.Listeners.OnVerAck = newSeeder.onVerAck

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
	// lock peers for read
	s.peerState.RLock()
	_, expectingPeer := s.pendingPeers[p.Addr()]
	s.peerState.RUnlock()

	if !expectingPeer {
		s.logger.Printf("Got verack from unexpected peer %s", p.Addr())
		return
	}

	s.peerState.Lock()
	{
		// Add to set of live peers
		s.livePeers[p.Addr()] = p

		// Remove from set of pending peers
		delete(s.pendingPeers, p.Addr())
	}
	s.peerState.Unlock()

	s.peerState.RLock()
	{
		// Signal successful connection
		s.handshakeSignals[p.Addr()] <- p
	}
	s.peerState.RUnlock()

}

// ConnectToPeer attempts to connect to a peer on the default port at the
// specified address. It returns either a live peer connection or an error.
func (s *Seeder) ConnectToPeer(addr string) error {
	connectionString := net.JoinHostPort(addr, s.config.ChainParams.DefaultPort)

	p, err := peer.NewOutboundPeer(s.config, connectionString)
	if err != nil {
		return errors.Wrap(err, "constructing outbound peer")
	}

	conn, err := net.Dial("tcp", p.Addr())
	if err != nil {
		return errors.Wrap(err, "dialing new peer address")
	}

	s.peerState.Lock()
	{
		// Record that we're expecting a verack from this peer.
		s.pendingPeers[p.Addr()] = p

		// Make a channel for us to wait on.
		s.handshakeSignals[p.Addr()] = make(chan *peer.Peer, 1)
	}
	s.peerState.Unlock()

	// Begin connection negotiation.
	s.logger.Printf("Handshake initated with new peer %s", p.Addr())
	p.AssociateConnection(conn)

	for {
		// lock signals map for select
		s.peerState.RLock()
		handshakeChan := s.handshakeSignals[p.Addr()]
		s.peerState.RUnlock()

		select {
		case verackPeer := <-handshakeChan:
			s.peerState.Lock()
			{
				close(s.handshakeSignals[p.Addr()])
				delete(s.handshakeSignals, p.Addr())
			}
			s.peerState.Unlock()
			s.logger.Printf("Handshake completed with new peer %s", verackPeer.Addr())
			return nil
		case <-time.After(time.Second * 1):
			return errors.New("peer handshake timed out")
		}
	}

	panic("This should be unreachable")
}

func (s *Seeder) GetPeer(addr string) (*peer.Peer, error) {
	lookupKey := net.JoinHostPort(addr, s.config.ChainParams.DefaultPort)
	s.peerState.RLock()
	p, ok := s.livePeers[lookupKey]
	s.peerState.RUnlock()

	if !ok {
		return nil, errors.New("no such active peer")
	}

	return p, nil
}

func (s *Seeder) WaitForPeers() {
	panic("not yet implemented")
}

func (s *Seeder) DisconnectAllPeers() {
	s.peerState.Lock()
	{
		for _, v := range s.pendingPeers {
			s.logger.Printf("Disconnecting from peer %s", v.Addr())
			v.Disconnect()
			v.WaitForDisconnect()
		}
		s.pendingPeers = make(map[string]*peer.Peer)

		for _, v := range s.livePeers {
			s.logger.Printf("Disconnecting from peer %s", v.Addr())
			v.Disconnect()
			v.WaitForDisconnect()
		}
		s.livePeers = make(map[string]*peer.Peer)
	}
	s.peerState.Unlock()
}
