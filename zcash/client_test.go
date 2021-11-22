package zcash

import (
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/btcsuite/btcd/peer"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btclog"
	"github.com/zcashfoundation/dnsseeder/zcash/network"
)

// useAddrV2 specifies if startMockLoop() should send addrv2 messages or regular addr.
// It's an uint32 since it's used with the atomic package.
var useAddrV2 uint32

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

		if atomic.LoadUint32(&useAddrV2) != 0 {
			cache := make([]*wire.NetAddressV2, 0, 1)
			addrv2_1 := wire.NewNetAddressV2NetAddress(addr)
			addrv2_2 := wire.NewNetAddressV2NetAddress(addr2)
			addrv2_3 := wire.NewNetAddressV2NetAddress(addr3)
			cache = append(cache, addrv2_1, addrv2_2, addrv2_3)
			_, err := p.PushAddrV2Msg(cache)
			if err != nil {
				mockPeerLogger.Error(err)
			}
		} else {
			cache := make([]*wire.NetAddress, 0, 1)
			cache = append(cache, addr, addr2, addr3)
			_, err := p.PushAddrMsg(cache)
			if err != nil {
				mockPeerLogger.Error(err)
			}
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

func TestRequestAddressesV2(t *testing.T) {
	atomic.StoreUint32(&useAddrV2, 1)
	defer atomic.StoreUint32(&useAddrV2, 0)

	regSeeder, err := newTestSeeder(network.Regtest)
	if err != nil {
		t.Error(err)
		return
	}

	// Wrap the listener to allow checking it was called
	originalFn := regSeeder.config.Listeners.OnAddrV2
	var receivedAddrV2 uint32
	regSeeder.config.Listeners.OnAddrV2 = func(p *peer.Peer, msg *wire.MsgAddrV2) {
		atomic.StoreUint32(&receivedAddrV2, 1)
		originalFn(p, msg)
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

	if atomic.LoadUint32(&receivedAddrV2) != 1 {
		t.Error("OnAddrV2 was not called")
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

	workingTestPeer := PeerKey("127.0.0.1:12345")

	regSeeder.testBlacklist(workingTestPeer)
	_, err = regSeeder.Connect("127.0.0.1", "12345")
	if err != ErrBlacklistedPeer {
		t.Errorf("Blacklist did not prevent connection")
	}
	regSeeder.DisconnectPeer(workingTestPeer)

	regSeeder.testRedeem(workingTestPeer)
	_, err = regSeeder.Connect("127.0.0.1", "12345")
	if err != nil {
		t.Errorf("Redeem didn't allow reconnecting")
	}
}
