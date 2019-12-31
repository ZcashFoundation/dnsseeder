package zcash

import (
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/peer"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btclog"
	"github.com/gtank/coredns-zcash/zcash/network"
)

func TestMain(m *testing.M) {
	startMockLoop()
	exitCode := m.Run()
	os.Exit(exitCode)
}

func startMockLoop() {
	// Configure peer to act as a regtest node that offers no services.
	config, err := newSeederPeerConfig(network.Regtest, defaultPeerConfig)
	if err != nil {
		return
	}

	config.AllowSelfConns = true

	backendLogger := btclog.NewBackend(os.Stdout)
	mockPeerLogger := backendLogger.Logger("mockPeer")
	//mockPeerLogger.SetLevel(btclog.LevelTrace)
	peer.UseLogger(mockPeerLogger)

	config.Listeners.OnGetAddr = func(p *peer.Peer, msg *wire.MsgGetAddr) {
		cache := make([]*wire.NetAddress, 0, 1)
		addr := wire.NewNetAddressTimestamp(
			time.Now(),
			0,
			net.ParseIP("127.0.0.1"),
			uint16(18233),
		)
		addr2 := wire.NewNetAddressTimestamp(
			time.Now(),
			0,
			net.ParseIP("127.0.0.1"),
			uint16(18344),
		)
		cache = append(cache, addr, addr2)
		_, err := p.PushAddrMsg(cache)
		if err != nil {
			mockPeerLogger.Error(err)
		}
	}

	listenAddr := net.JoinHostPort("127.0.0.1", config.ChainParams.DefaultPort)
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			mockPeer := peer.NewInboundPeer(config)
			mockPeer.AssociateConnection(conn)
		}
	}()
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

	regSeeder.RequestAddresses()
	err = regSeeder.WaitForAddresses(1, 1*time.Second)

	if err != nil {
		t.Errorf("Error getting one mocked address: %v", err)
	}

	err = regSeeder.WaitForAddresses(500, 1*time.Second)
	if err != ErrAddressTimeout {
		t.Errorf("Should have timed out, instead got: %v", err)
	}
}
