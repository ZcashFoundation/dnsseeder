package zcash

import (
	"runtime"
	"strconv"
	"time"

	"github.com/btcsuite/btcd/addrmgr"
	"github.com/btcsuite/btcd/peer"
	"github.com/btcsuite/btcd/wire"
)

func (s *Seeder) onVerAck(p *peer.Peer, msg *wire.MsgVerAck) {
	pk := peerKeyFromPeer(p)

	// Check if we're expecting to hear from this peer
	_, ok := s.pendingPeers.Load(pk)

	if !ok {
		s.logger.Printf("Got verack from unexpected peer %s", p.Addr())
		// TODO: probably want to disconnect from the peer sending us out-of-order veracks
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

	queue := make(chan *wire.NetAddress, len(msg.AddrList))

	for _, na := range msg.AddrList {
		queue <- na
	}

	for i := 0; i < runtime.NumCPU()*2; i++ {
		go func() {
			var na *wire.NetAddress
			for {
				select {
				case next := <-queue:
					// Pull the next address off the queue
					na = next
				case <-time.After(1 * time.Second):
					// Or die if there wasn't one
					return
				}

				// Note that AllowSelfConns is only exposed in a fork of btcd
				// pending https://github.com/btcsuite/btcd/pull/1481, which
				// is why the module `replace`s btcd.
				if !addrmgr.IsRoutable(na) && !s.config.AllowSelfConns {
					s.logger.Printf("Got bad addr %s:%d from peer %s", na.IP, na.Port, p.Addr())
					s.DisconnectAndBlacklist(peerKeyFromPeer(p))
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
						s.logger.Printf("Got duplicate peer %s:%d from peer %s. Error: %s", na.IP, na.Port, p.Addr(), err)
						continue
					}

					// Blacklist the potential peer. We might try to connect again later,
					// since we assume IsRoutable filtered out the truly wrong ones.
					s.logger.Printf("Got unusable peer %s:%d from peer %s. Error: %s", na.IP, na.Port, p.Addr(), err)
					s.addrBook.Blacklist(potentialPeer)
					continue
				}

				s.DisconnectPeer(potentialPeer)

				s.logger.Printf("Successfully learned about %s:%d from %s.", na.IP, na.Port, p.Addr())
				s.addrBook.Add(potentialPeer)
			}
		}()
	}
}
