package zcash

import (
	"testing"
	"time"

	"github.com/gtank/coredns-zcash/zcash/network"
)

func TestOutboundPeer(t *testing.T) {
	regSeeder, err := NewSeeder(network.Regtest)
	if err != nil {
		t.Fatal(err)
	}

	_, err = regSeeder.ConnectToPeer("127.0.0.1")
	if err != nil {
		t.Error(err)
	}
	regSeeder.GracefulDisconnect()
}

func TestOutboundPeerAsync(t *testing.T) {
	regSeeder, err := NewSeeder(network.Regtest)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		_, err := regSeeder.ConnectToPeer("127.0.0.1")
		if err != nil {
			t.Fatal(err)
		}
		regSeeder.GracefulDisconnect()
		done <- struct{}{}
	}()

	select {
	case <-done:
	case <-time.After(time.Second * 1):
		t.Error("timed out")
	}
}
