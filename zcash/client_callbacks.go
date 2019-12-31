package zcash

import (
	"strconv"
	"time"

	"github.com/btcsuite/btcd/addrmgr"
	"github.com/btcsuite/btcd/peer"
	"github.com/btcsuite/btcd/wire"
)

func (s *Seeder) onVerAck(p *peer.Peer, msg *wire.MsgVerAck) {
	// Check if we're expecting to hear from this peer
	_, ok := s.pendingPeers.Load(peerKeyFromPeer(p))

	if !ok {
		s.logger.Printf("Got verack from unexpected peer %s", p.Addr())
		return
	}

	// Add to set of live peers
	s.livePeers.Store(peerKeyFromPeer(p), p)

	// Remove from set of pending peers
	s.pendingPeers.Delete(peerKeyFromPeer(p))

	// Signal successful connection
	if signal, ok := s.handshakeSignals.Load(p.Addr()); ok {
		signal.(chan struct{}) <- struct{}{}
	} else {
		s.logger.Printf("Got verack from peer without a callback channel: %s", p.Addr())
		s.DisconnectPeer(peerKeyFromPeer(p))
		return
	}

	// Add to list of known good addresses if we don't already have it.
	// Otherwise, update the last-valid time.
	newAddr := NewAddress(p.NA())

	if s.addrBook.AlreadyKnowsAddress(p.NA()) {
		s.addrBook.UpdateAddressStateFromTemplate(newAddr)
		return
	}

	s.logger.Printf("Adding %s to address list", p.Addr())

	s.addrBook.Add(newAddr)
	return
}

func (s *Seeder) onAddr(p *peer.Peer, msg *wire.MsgAddr) {
	if len(msg.AddrList) == 0 {
		s.logger.Printf("Got empty addr message from peer %s. Disconnecting.", p.Addr())
		s.DisconnectPeer(peerKeyFromPeer(p))
		return
	}

	s.logger.Printf("Got %d addrs from peer %s", len(msg.AddrList), p.Addr())

	queue := make(chan *wire.NetAddress, len(msg.AddrList))

	for _, na := range msg.AddrList {
		queue <- na
	}

	for i := 0; i < 32; i++ {
		go func() {
			var na *wire.NetAddress
			for {
				select {
				case next := <-queue:
					na = next
				case <-time.After(1 * time.Second):
					return
				}

				if !addrmgr.IsRoutable(na) && !s.config.AllowSelfConns {
					s.logger.Printf("Got bad addr %s:%d from peer %s", na.IP, na.Port, p.Addr())
					s.DisconnectPeerDishonorably(peerKeyFromPeer(p))
					continue
				}

				if s.addrBook.AlreadyKnowsAddress(na) {
					s.logger.Printf("Already knew about address %s:%d", na.IP, na.Port)
					continue
				}

				if s.addrBook.IsBlacklistedAddress(na) {
					s.logger.Printf("Address %s:%d is blacklisted", na.IP, na.Port)
					continue
				}

				portString := strconv.Itoa(int(na.Port))
				err := s.Connect(na.IP.String(), portString)

				if err != nil {
					s.logger.Printf("Got unusable peer %s:%d from peer %s. Error: %s", na.IP, na.Port, p.Addr(), err)

					// Mark previously-known peers as invalid
					newAddr := &Address{
						netaddr:   p.NA(),
						valid:     false,
						lastTried: time.Now(),
					}

					// TODO: function for marking bad addresses directly. needs better storage layer

					if s.addrBook.AlreadyKnowsAddress(p.NA()) {
						s.addrBook.UpdateAddressStateFromTemplate(newAddr)
					}
					continue
				}

				s.DisconnectPeer(peerKeyFromNA(na))
				s.addrBook.Add(NewAddress(na))
			}
		}()
	}
}
