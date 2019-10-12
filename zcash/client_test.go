package zcash

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/btcsuite/btcd/peer"
	"github.com/btcsuite/btclog"
	"github.com/gtank/coredns-zcash/zcash/network"
)

func mockLocalPeer(ctx context.Context) error {
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

	mockPeer := peer.NewInboundPeer(config)

	listenAddr := net.JoinHostPort("127.0.0.1", config.ChainParams.DefaultPort)
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}

		mockPeer.AssociateConnection(conn)

		select {
		case <-ctx.Done():
			mockPeer.Disconnect()
			mockPeer.WaitForDisconnect()
			return
		}
	}()

	return nil
}

func TestOutboundPeerSync(t *testing.T) {
	testContext, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mockLocalPeer(testContext); err != nil {
		t.Logf("error starting mock peer (%v).", err)
	}

	regSeeder, err := newTestSeeder(network.Regtest)
	if err != nil {
		t.Fatal(err)
	}

	err = regSeeder.ConnectToPeer("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	// Can we address that peer if we want to?
	p, err := regSeeder.GetPeer("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	if p.Connected() {
		regSeeder.DisconnectAllPeers()
	} else {
		t.Error("Peer never connected")
	}

	// Can we STILL address a flushed peer?
	p, err = regSeeder.GetPeer("127.0.0.1")
	if err == nil {
		t.Error("Peer should have been cleared on disconnect")
	}
}

func TestOutboundPeerAsync(t *testing.T) {
	testContext, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := mockLocalPeer(testContext); err != nil {
		t.Logf("error starting mock peer (%v).", err)
	}

	regSeeder, err := newTestSeeder(network.Regtest)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		err := regSeeder.ConnectToPeer("127.0.0.1")
		if err != nil {
			t.Fatal(err)
		}
		regSeeder.DisconnectAllPeers()
		done <- struct{}{}
	}()

	select {
	case <-done:
	case <-time.After(time.Second * 1):
		t.Error("timed out")
	}
}
