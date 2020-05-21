package zcash

import (
	"log"
	"net"
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/btcsuite/btcd/addrmgr"
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

const (
	// The minimum number of addresses we need to know about to begin serving introductions
	minimumReadyAddresses = 10

	// The maximum amount of time we will wait for a peer to complete the initial handshake
	maximumHandshakeWait = 1 * time.Second

	// The timeout for the underlying dial to a peer
	connectionDialTimeout = 1 * time.Second

	// The amount of time crawler goroutines will wait for incoming addresses after a RequestAddresses()
	crawlerThreadTimeout = 30 * time.Second
)

// Seeder contains all of the state and configuration needed to request addresses from Zcash peers and present them to a DNS provider.
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

	// The queue of incoming potential addresses
	addrQueue chan *wire.NetAddress
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
		addrBook:         NewAddressBook(),
		addrQueue:        make(chan *wire.NetAddress, 100),
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
		addrBook:         NewAddressBook(),
		addrQueue:        make(chan *wire.NetAddress, 100),
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

	// PeerKeys are used in our internal maps to keep signals and responses from specific peers straight.
	pk := peerKeyFromPeer(p)

	if s.addrBook.IsBlacklisted(pk) {
		return ErrBlacklistedPeer
	}

	_, alreadyPending := s.pendingPeers.Load(pk)
	_, alreadyHandshaking := s.handshakeSignals.Load(pk)
	_, alreadyLive := s.livePeers.Load(pk)

	if alreadyPending {
		s.logger.Printf("Peer is already pending: %s", p.Addr())
		return ErrRepeatConnection
	}
	s.pendingPeers.Store(pk, p)

	if alreadyHandshaking {
		s.logger.Printf("Peer is already handshaking: %s", p.Addr())
		return ErrRepeatConnection
	}
	s.handshakeSignals.Store(pk, make(chan struct{}, 1))

	if alreadyLive {
		s.logger.Printf("Peer is already live: %s", p.Addr())
		return ErrRepeatConnection
	}

	conn, err := net.DialTimeout("tcp", p.Addr(), connectionDialTimeout)
	if err != nil {
		return errors.Wrap(err, "dialing new peer address")
	}

	// Begin connection negotiation.
	s.logger.Printf("Handshake initated with new peer %s", p.Addr())
	p.AssociateConnection(conn)

	// Wait for
	handshakeChan, _ := s.handshakeSignals.Load(pk)

	select {
	case <-handshakeChan.(chan struct{}):
		s.logger.Printf("Handshake completed with new peer %s", p.Addr())
		s.handshakeSignals.Delete(pk)
		return nil
	case <-time.After(maximumHandshakeWait):
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

// DisconnectAndBlacklist disconnects from a live peer identified by
// "host:port" string. It returns an error if we aren't connected to that peer.
// It furthermore removes this peer from the list of known good
// addresses and adds them to a blacklist. to prevent future connections.
func (s *Seeder) DisconnectAndBlacklist(addr PeerKey) error {
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

// RequestAddresses sends a request for more addresses to every peer we're connected to,
// then checks to make sure the addresses that come back are usable before adding them to
// the address book.
func (s *Seeder) RequestAddresses() {
	s.livePeers.Range(func(key PeerKey, p *peer.Peer) bool {
		s.logger.Printf("Requesting addresses from peer %s", p.Addr())
		p.QueueMessage(wire.NewMsgGetAddr(), nil)
		return true
	})

	// There's a sync concern: if this is called repeatedly you could end up broadcasting
	// GetAddr messages to briefly live trial connections without meaning to. It's
	// meant to be run on a timer that takes longer to fire than it takes to check addresses.

	for i := 0; i < runtime.NumCPU()*4; i++ {
		go func() {
			var na *wire.NetAddress
			for {
				select {
				case next := <-s.addrQueue:
					// Pull the next address off the queue
					na = next
				case <-time.After(crawlerThreadTimeout):
					// Or die if there wasn't one
					s.logger.Printf("Crawler thread timeout")
					return
				}

				// Note that AllowSelfConns is only exposed in a fork of btcd
				// pending https://github.com/btcsuite/btcd/pull/1481, which
				// is why the module `replace`s btcd.
				if !addrmgr.IsRoutable(na) && !s.config.AllowSelfConns {
					s.logger.Printf("Got bad addr %s:%d from peer %s", na.IP, na.Port, "<placeholder>")
					// TODO blacklist peers who give us crap addresses
					//s.DisconnectAndBlacklist(peerKeyFromPeer(p))
					continue
				}

				potentialPeer := peerKeyFromNA(na)

				if s.addrBook.IsKnown(potentialPeer) {
					s.logger.Printf("Already knew about %s:%d", na.IP, na.Port)
					continue
				}

				if s.addrBook.IsBlacklisted(potentialPeer) {
					s.logger.Printf("Previously blacklisted %s:%d", na.IP, na.Port)
					continue
				}

				portString := strconv.Itoa(int(na.Port))
				err := s.Connect(na.IP.String(), portString)

				if err != nil {
					if err == ErrRepeatConnection {
						//s.logger.Printf("Got duplicate peer %s:%d.", na.IP, na.Port)
						continue
					}

					// Blacklist the potential peer. We might try to connect again later,
					// since we assume IsRoutable filtered out the truly wrong ones.
					s.logger.Printf("Got unusable peer %s:%d. Error: %s", na.IP, na.Port, err)
					s.addrBook.Blacklist(potentialPeer)
					continue
				}

				s.DisconnectPeer(potentialPeer)

				s.logger.Printf("Successfully learned about %s:%d.", na.IP, na.Port)
				s.addrBook.Add(potentialPeer)
			}
		}()
	}
}

// WaitForAddresses waits for n addresses to be confirmed and available in the address book.
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

// Ready reports if the seeder is ready to provide addresses.
func (s *Seeder) Ready() bool {
	return s.WaitForAddresses(minimumReadyAddresses, 1*time.Millisecond) == nil
}

// Addresses returns a slice of n IPv4 addresses or as many as we have if it's less than that.
func (s *Seeder) Addresses(n int) []net.IP {
	return s.addrBook.shuffleAddressList(n, false)
}

// AddressesV6 returns a slice of n IPv6 addresses or as many as we have if it's less than that.
func (s *Seeder) AddressesV6(n int) []net.IP {
	return s.addrBook.shuffleAddressList(n, true)
}

// testBlacklist adds a peer to the blacklist directly, for testing.
func (s *Seeder) testBlacklist(pk PeerKey) {
	s.addrBook.Blacklist(pk)
}
