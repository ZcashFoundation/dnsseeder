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

var logger = log.New(os.Stdout, "zcash_client: ", log.Ldate|log.Ltime|log.Lshortfile|log.LUTC)

type Seeder struct {
	peer   *peer.Peer
	config *peer.Config

	handshakeComplete     chan *peer.Peer
	handshakePendingPeers map[string]*peer.Peer
	livePeers             map[string]*peer.Peer

	// For mutating the above
	peerState sync.RWMutex
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

func (s *Seeder) OnVerAck(p *peer.Peer, msg *wire.MsgVerAck) {
	s.peerState.RLock()
	if s.handshakePendingPeers[p.Addr()] == nil {
		logger.Printf("Got verack from unexpected peer %s", p.Addr())
		s.peerState.RUnlock()
		return
	}
	s.peerState.RUnlock()

	logger.Printf("Handshake completed with new peer %s", p.Addr())

	s.peerState.Lock()
	delete(s.handshakePendingPeers, p.Addr())
	s.livePeers[p.Addr()] = p
	s.peerState.Unlock()

	s.handshakeComplete <- p
}

func NewSeeder(network network.Network) (*Seeder, error) {
	config, err := newSeederPeerConfig(network, defaultPeerConfig)
	if err != nil {
		return nil, errors.Wrap(err, "could not construct seeder")
	}

	newSeeder := Seeder{
		config:                config,
		handshakeComplete:     make(chan *peer.Peer, 1),
		handshakePendingPeers: make(map[string]*peer.Peer),
		livePeers:             make(map[string]*peer.Peer),
	}

	newSeeder.config.Listeners.OnVerAck = newSeeder.OnVerAck

	return &newSeeder, nil
}

// ConnectToPeer attempts to connect to a peer on the default port at the
// specified address. It returns either a live peer connection or an error.
func (s *Seeder) ConnectToPeer(addr string) (*peer.Peer, error) {
	connectionString := net.JoinHostPort(addr, s.config.ChainParams.DefaultPort)

	p, err := peer.NewOutboundPeer(s.config, connectionString)
	if err != nil {
		return nil, errors.Wrap(err, "constructing outbound peer")
	}

	conn, err := net.Dial("tcp", p.Addr())
	if err != nil {
		return nil, errors.Wrap(err, "dialing new peer address")
	}

	s.peerState.Lock()
	s.handshakePendingPeers[p.Addr()] = p
	s.peerState.Unlock()

	p.AssociateConnection(conn)

	for {
		select {
		case verackPeer := <-s.handshakeComplete:
			if verackPeer.Addr() == p.Addr() {
				return p, nil
			}
		case <-time.After(time.Second * 1):
			return nil, errors.New("peer handshake timed out")
		}
	}

	panic("This should be unreachable")
}

func (s *Seeder) GracefulDisconnect() {
	s.peerState.Lock()

	for _, v := range s.handshakePendingPeers {
		logger.Printf("Disconnecting from peer %s", v.Addr())
		v.Disconnect()
		v.WaitForDisconnect()
	}
	s.handshakePendingPeers = make(map[string]*peer.Peer)

	for _, v := range s.livePeers {
		logger.Printf("Disconnecting from peer %s", v.Addr())
		v.Disconnect()
		v.WaitForDisconnect()
	}
	s.livePeers = make(map[string]*peer.Peer)

	s.peerState.Unlock()
}
