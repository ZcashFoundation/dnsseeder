package zcash

import (
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/peer"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btclog"
	"github.com/zcashfoundation/dnsseeder/zcash/network"
)

func TestMain(m *testing.M) {
	err := startMockLoop()
	if err != nil {
		fmt.Printf("Failed to start mock loop!")
		os.Exit(1)
	}
	exitCode := m.Run()
	os.Exit(exitCode)
}

func startMockLoop() error {
	// Configure peer to act as a regtest node that offers no services.
	config, err := newSeederPeerConfig(network.Regtest, defaultPeerConfig)
	if err != nil {
		return err
	}

	config.AllowSelfConns = true

	backendLogger := btclog.NewBackend(os.Stdout)
	mockPeerLogger := backendLogger.Logger("mockPeer")
	//mockPeerLogger.SetLevel(btclog.LevelTrace)
	peer.UseLogger(mockPeerLogger)

	config.Listeners.OnGetAddr = func(p *peer.Peer, msg *wire.MsgGetAddr) {
		cache := make([]*wire.NetAddress, 0, 1)

		// This will an unusable peer (testnet port)
		addr := wire.NewNetAddressTimestamp(
			time.Now(),
			0,
			net.ParseIP("127.0.0.1"),
			uint16(18233),
		)

		// This will be an unusable peer (bs port)
		addr2 := wire.NewNetAddressTimestamp(
			time.Now(),
			0,
			net.ParseIP("127.0.0.1"),
			uint16(31337),
		)

		// This will be a usable peer, corresponding to our second listener.
		addr3 := wire.NewNetAddressTimestamp(
			time.Now(),
			0,
			net.ParseIP("127.0.0.1"),
			uint16(12345),
		)

		cache = append(cache, addr, addr2, addr3)
		_, err := p.PushAddrMsg(cache)
		if err != nil {
			mockPeerLogger.Error(err)
		}
	}

	listener1, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", config.ChainParams.DefaultPort))
	if err != nil {
		return err
	}
	listener2, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", "12345"))
	if err != nil {
		return err
	}

	go func() {
		for {
			conn, err := listener1.Accept()
			if err != nil {
				return
			}

			mockPeer := peer.NewInboundPeer(config)
			mockPeer.AssociateConnection(conn)
		}
	}()

	go func() {
		for {
			conn, err := listener2.Accept()
			if err != nil {
				return
			}
			defaultConfig, _ := newSeederPeerConfig(network.Regtest, defaultPeerConfig)
			defaultConfig.AllowSelfConns = true
			mockPeer := peer.NewInboundPeer(defaultConfig)
			mockPeer.AssociateConnection(conn)
		}
	}()

	return nil
}

func TestOutboundPeerSync(t *testing.T) {
	regSeeder, err := newTestSeeder(network.Regtest)
	if err != nil {
		t.Error(err)
		return
	}

	err = regSeeder.ConnectOnDefaultPort("127.0.0.1")
	if err != nil {
		t.Error(err)
		return
	}

	// Can we address that peer if we want to?
	p, err := regSeeder.GetPeer("127.0.0.1:18344")
	if err != nil {
		t.Error(err)
		return
	}

	if p.Connected() {
		regSeeder.DisconnectPeer("127.0.0.1:18344")
	} else {
		t.Error("Peer never connected")
	}

	// Can we STILL address a flushed peer?
	p, err = regSeeder.GetPeer("127.0.0.1:18344")
	if err != ErrNoSuchPeer {
		t.Error("Peer should have been cleared on disconnect")
	}
}

func TestOutboundPeerAsync(t *testing.T) {
	regSeeder, err := newTestSeeder(network.Regtest)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			err := regSeeder.ConnectOnDefaultPort("127.0.0.1")
			if err != nil && err != ErrRepeatConnection {
				t.Error(err)
			}
			wg.Done()
		}()
	}

	wg.Wait()

	// Can we address that peer if we want to?
	p, err := regSeeder.GetPeer("127.0.0.1:18344")
	if err != nil {
		t.Error(err)
		return
	}

	if p.Connected() {
		// Shouldn't try to connect to a live peer again
		err := regSeeder.ConnectOnDefaultPort("127.0.0.1")
		if err != ErrRepeatConnection {
			t.Error("should have caught repeat connection attempt")
		}
	} else {
		t.Error("Peer never connected")
	}

	regSeeder.DisconnectAllPeers()
}

func TestRequestAddresses(t *testing.T) {
	regSeeder, err := newTestSeeder(network.Regtest)
	if err != nil {
		t.Error(err)
		return
	}

	err = regSeeder.ConnectOnDefaultPort("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	go regSeeder.RequestAddresses()
	err = regSeeder.WaitForAddresses(1, 1*time.Second)

	if err != nil {
		t.Errorf("Error getting one mocked address: %v", err)
	}

	err = regSeeder.WaitForAddresses(500, 1*time.Second)
	if err != ErrAddressTimeout {
		t.Errorf("Should have timed out, instead got: %v", err)
	}
}

func TestBlacklist(t *testing.T) {
	regSeeder, err := newTestSeeder(network.Regtest)
	if err != nil {
		t.Error(err)
		return
	}

	err = regSeeder.ConnectOnDefaultPort("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	go regSeeder.RequestAddresses()
	err = regSeeder.WaitForAddresses(1, 1*time.Second)

	if err != nil {
		t.Errorf("Error getting one mocked address: %v", err)
	}

	regSeeder.testBlacklist(PeerKey("127.0.0.1:12345"))
	err = regSeeder.Connect("127.0.0.1", "12345")
	if err != ErrBlacklistedPeer {
		t.Errorf("Blacklist did not prevent connection")
	}
}
