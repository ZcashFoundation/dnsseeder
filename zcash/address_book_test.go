package zcash

import (
	"testing"
	"time"
)

func TestShuffleAddressListMaxAge(t *testing.T) {
	book := NewAddressBook()

	// Add a fresh peer
	book.Add(PeerKey("127.0.0.1:8233"))

	// Should return the peer with no age filter (maxAge=0)
	addrs := book.shuffleAddressList(10, false, "8233", 0)
	if len(addrs) != 1 {
		t.Errorf("expected 1 address with no age filter, got %d", len(addrs))
	}

	// Should return the peer with large age filter
	addrs = book.shuffleAddressList(10, false, "8233", time.Hour)
	if len(addrs) != 1 {
		t.Errorf("expected 1 address with hour filter, got %d", len(addrs))
	}

	// Wait a bit and then test with tiny age filter
	time.Sleep(10 * time.Millisecond)

	// Should NOT return the peer with tiny age filter (1 millisecond)
	addrs = book.shuffleAddressList(10, false, "8233", time.Millisecond)
	if len(addrs) != 0 {
		t.Errorf("expected 0 addresses with millisecond filter, got %d", len(addrs))
	}

	// Touch the address to refresh its timestamp
	book.Touch(PeerKey("127.0.0.1:8233"))

	// Should return the peer again after touching
	addrs = book.shuffleAddressList(10, false, "8233", time.Hour)
	if len(addrs) != 1 {
		t.Errorf("expected 1 address after touch, got %d", len(addrs))
	}
}

func TestShuffleAddressListFilters(t *testing.T) {
	book := NewAddressBook()

	// Add peers on different ports
	book.Add(PeerKey("127.0.0.1:8233"))  // default mainnet port
	book.Add(PeerKey("127.0.0.2:18233")) // testnet port
	book.Add(PeerKey("127.0.0.3:9999"))  // random port

	// Should only return the address on default port 8233
	addrs := book.shuffleAddressList(10, false, "8233", 0)
	if len(addrs) != 1 {
		t.Errorf("expected 1 address on port 8233, got %d", len(addrs))
	}

	// Should only return the address on port 18233
	addrs = book.shuffleAddressList(10, false, "18233", 0)
	if len(addrs) != 1 {
		t.Errorf("expected 1 address on port 18233, got %d", len(addrs))
	}
}

func TestBlacklistExcludesFromShuffle(t *testing.T) {
	book := NewAddressBook()

	book.Add(PeerKey("127.0.0.1:8233"))
	book.Add(PeerKey("127.0.0.2:8233"))

	// Should return both peers
	addrs := book.shuffleAddressList(10, false, "8233", 0)
	if len(addrs) != 2 {
		t.Errorf("expected 2 addresses, got %d", len(addrs))
	}

	// Blacklist one peer
	book.Blacklist(PeerKey("127.0.0.1:8233"))

	// Should only return the non-blacklisted peer
	addrs = book.shuffleAddressList(10, false, "8233", 0)
	if len(addrs) != 1 {
		t.Errorf("expected 1 address after blacklist, got %d", len(addrs))
	}

	// Redeem the blacklisted peer
	book.Redeem(PeerKey("127.0.0.1:8233"))

	// Should return both peers again
	addrs = book.shuffleAddressList(10, false, "8233", 0)
	if len(addrs) != 2 {
		t.Errorf("expected 2 addresses after redeem, got %d", len(addrs))
	}
}
