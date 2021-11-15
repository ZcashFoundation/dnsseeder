package zcash

import (
	"github.com/btcsuite/btcd/peer"
	"github.com/btcsuite/btcd/wire"
)

func (s *Seeder) onVerAck(p *peer.Peer, msg *wire.MsgVerAck) {
	pk := peerKeyFromPeer(p)

	// Check if we're expecting to hear from this peer
	_, ok := s.pendingPeers.Load(pk)

	if !ok {
		s.logger.Printf("Got verack from unexpected peer %s", p.Addr())
		// Disconnect from the peer sending us out-of-order veracks
		s.DisconnectPeer(pk)
		return
	}

	// Add to set of live peers
	s.livePeers.Store(pk, p)

	// Remove from set of pending peers
	s.pendingPeers.Delete(pk)

	// Signal successful connection
	if signal, ok := s.handshakeSignals.Load(pk); ok {
		signal.(chan struct{}) <- struct{}{}
	} else {
		s.logger.Printf("Got verack from peer without a callback channel: %s", p.Addr())
		s.DisconnectPeer(pk)
		return
	}

	// If we've already connected to this peer, update the last-valid time.
	if s.addrBook.IsKnown(pk) {
		s.addrBook.Touch(pk)
	}

	return
}

func (s *Seeder) onAddr(p *peer.Peer, msg *wire.MsgAddr) {
	if len(msg.AddrList) == 0 {
		s.logger.Printf("Got empty addr message from peer %s. Disconnecting.", p.Addr())
		s.DisconnectPeer(peerKeyFromPeer(p))
		return
	}

	s.logger.Printf("Got %d addrs from peer %s", len(msg.AddrList), p.Addr())

	for _, na := range msg.AddrList {
		// By checking if we know them before adding to the queue, we create
		// the end condition for the crawler thread: it will time out after
		// not processing any new addresses.
		if s.addrBook.IsKnown(peerKeyFromNA(na)) {
			s.logger.Printf("Already knew about %s:%d", na.IP, na.Port)
			continue
		}
		s.addrQueue <- na
	}
}

func (s *Seeder) onAddrV2(p *peer.Peer, msg *wire.MsgAddrV2) {
	if len(msg.AddrList) == 0 {
		s.logger.Printf("Got empty addrv2 message from peer %s. Disconnecting.", p.Addr())
		s.DisconnectPeer(peerKeyFromPeer(p))
		return
	}

	s.logger.Printf("Got %d addrv2s from peer %s", len(msg.AddrList), p.Addr())

	for _, na := range msg.AddrList {
		if na.NetworkID == wire.NIIPV4 || na.NetworkID == wire.NIIPV6 {
			// By checking if we know them before adding to the queue, we create
			// the end condition for the crawler thread: it will time out after
			// not processing any new addresses.
			if s.addrBook.IsKnown(peerKeyFromNAV2(na)) {
				s.logger.Printf("Already knew about %s:%d", na.IP, na.Port)
				continue
			}
			s.addrQueue <- &na.NetAddress
		}
	}
}
