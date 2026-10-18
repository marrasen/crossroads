package serato

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
)

// clientGUID is the identity Serato keys this client by. It stays the
// same across runs.
const clientGUID = "{b6f2a1e0-3c4d-4e8a-9f12-7a5c1d9e42b0}"

// emitEvery is how often snapshots are pushed on. The playhead streams
// at about 60 a second per deck, which is far more than anything needs
// to see.
const emitEvery = 40 * time.Millisecond

// maxPending is the most bytes buffered for one connection before the
// peer is treated as hostile. Real messages are a few hundred bytes.
const maxPending = 1 << 16

// A Service is the Serato External Control adapter.
//
// It advertises crossroads as a Serato remote, answers the handshake
// itself, subscribes to the deck and mixer feeds, and keeps a live
// Snapshot.
//
// It listens on every interface. The remote protocol is meant for a
// client on the local network — it began as an iOS app — so this is not
// loopback only, which is the same trust model SoundSwitch has. The
// peer is not authenticated to us, so the parser is hardened: buffers
// are bounded and bad lengths rejected, and a hostile packet can at
// worst inject deck state rather than crash or exhaust the process.
type Service struct {
	mu       sync.Mutex
	snapshot Snapshot
	listener net.Listener
	server   *zeroconf.Server
	conns    map[net.Conn]bool
	// paired are the connections that reached Pairing/Pair: the live
	// data feeds.
	paired map[net.Conn]bool
	dirty  bool

	instanceName string
	onSnapshot   func(Snapshot)
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

// NewService makes an adapter that pushes every change to onSnapshot.
func NewService(onSnapshot func(Snapshot)) *Service {
	host, err := os.Hostname()
	if err != nil {
		host = "crossroads"
	}
	return &Service{
		snapshot:     EmptySnapshot(false),
		conns:        map[net.Conn]bool{},
		paired:       map[net.Conn]bool{},
		instanceName: "crossroads @ " + host,
		onSnapshot:   onSnapshot,
	}
}

// Snapshot is the deck state as it stands.
func (s *Service) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot.Clone()
}

// Start begins listening and advertising. It returns once the listener
// is up; everything after that runs on goroutines of its own.
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.listener != nil {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", ":0")
	if err != nil {
		return fmt.Errorf("serato: listening: %w", err)
	}
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		_ = listener.Close()
		return errors.New("serato: the listener is not TCP")
	}
	port := addr.Port
	server, err := zeroconf.Register(s.instanceName, ServiceType, "local.", port, nil, nil)
	if err != nil {
		_ = listener.Close()
		return fmt.Errorf("serato: advertising: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.listener, s.server, s.cancel = listener, server, cancel
	s.mu.Unlock()

	log.Printf("[serato] advertising %q on port %d", s.instanceName, port)

	s.wg.Go(func() { s.accept(ctx, listener) })
	s.wg.Go(func() { s.emitLoop(ctx) })
	return nil
}

// Stop shuts the adapter down and waits for its goroutines.
func (s *Service) Stop() {
	s.mu.Lock()
	cancel, listener, server := s.cancel, s.listener, s.server
	s.cancel, s.listener, s.server = nil, nil, nil
	for conn := range s.conns {
		_ = conn.Close()
	}
	s.conns = map[net.Conn]bool{}
	s.paired = map[net.Conn]bool{}
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if server != nil {
		server.Shutdown()
	}
	if listener != nil {
		_ = listener.Close()
	}
	s.wg.Wait()
	s.setConnected(false)
}

func (s *Service) accept(ctx context.Context, listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() == nil && !errors.Is(err, net.ErrClosed) {
				log.Printf("[serato] accepting: %v", err)
			}
			return
		}
		s.mu.Lock()
		s.conns[conn] = true
		s.mu.Unlock()
		s.wg.Go(func() { s.serve(ctx, conn) })
	}
}

// emitLoop gathers the high-rate playhead stream into steady pushes.
func (s *Service) emitLoop(ctx context.Context) {
	t := time.NewTicker(emitEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.mu.Lock()
			send := s.dirty
			s.dirty = false
			snapshot := s.snapshot.Clone()
			s.mu.Unlock()
			if send && s.onSnapshot != nil {
				s.onSnapshot(snapshot)
			}
		}
	}
}

// serve reads one Serato connection.
//
// Serato opens several at once — a keepalive, a challenge, a data feed
// — and each is decoded on its own, though they share the snapshot.
// Only a connection that reaches Pairing/Pair becomes the data feed.
func (s *Service) serve(ctx context.Context, conn net.Conn) {
	defer func() {
		_ = conn.Close()
		s.mu.Lock()
		delete(s.conns, conn)
		wasPaired := s.paired[conn]
		delete(s.paired, conn)
		none := len(s.paired) == 0
		s.mu.Unlock()
		// Losing the paired feed means Serato is gone: clear the stale
		// connected state so the interface and any Serato tempo source
		// stop dead rather than chase a phantom deck.
		if wasPaired && none {
			s.setConnected(false)
		}
	}()

	var pending []byte
	var isOsc, known bool
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			if !known {
				// Port scanners and TLS probes reach a listener on the
				// network too; a real packet starts with a slash or a hash.
				isOsc, known = buf[0] == '/' || buf[0] == '#', true
				if !isOsc {
					return
				}
			}
			pending = append(pending, buf[:n]...)
			// A peer that never completes a packet must not grow memory
			// without limit.
			if len(pending) > maxPending {
				return
			}
			for {
				pkt, derr := Decode(pending)
				if derr != nil {
					if errors.Is(derr, ErrShort) {
						break
					}
					return // unreadable: drop the peer
				}
				pending = pending[pkt.End:]
				for _, m := range pkt.Messages {
					s.handle(conn, m, pkt.Trailer)
				}
			}
		}
		if err != nil {
			return
		}
		if ctx.Err() != nil {
			return
		}
	}
}

func (s *Service) handle(conn net.Conn, m Message, trailer []byte) {
	switch {
	case m.Address == "/Ping":
		return

	case m.Address == "/StreamMgmt/Authorize/Request":
		if nonce, ok := m.Blob(NonceLen); ok {
			s.answerChallenge(conn, nonce, trailer)
		}

	case m.Address == "/StreamMgmt/Pairing/Pair":
		s.send(conn, []Message{{Address: "/StreamMgmt/Pairing/StatusChanged",
			Types: "s", Args: []Arg{"Paired"}}}, trailer)
		subscriptions := make([]Message, len(Subscriptions))
		for i, address := range Subscriptions {
			subscriptions[i] = Message{Address: address}
		}
		s.send(conn, subscriptions, trailer)
		s.mu.Lock()
		s.paired[conn] = true
		s.mu.Unlock()
		s.setConnected(true)

	case len(m.Address) > 8 && m.Address[:8] == "/Status/":
		s.mu.Lock()
		if ApplyStatus(&s.snapshot, m) {
			s.dirty = true
		}
		s.mu.Unlock()
	}
}

// answerChallenge replies with the blob the nonce asks for.
//
// Serato allows only about 40 ms per Authorize connection. Working the
// blob out is a hash, so the reply goes back on this goroutine, well
// inside the window.
func (s *Service) answerChallenge(conn net.Conn, nonce, trailer []byte) {
	s.replyBlob(conn, AuthBlob(nonce), trailer)
}

func (s *Service) replyBlob(conn net.Conn, blob, trailer []byte) {
	s.send(conn, []Message{{Address: "/StreamMgmt/Authorize/Response", Types: "ssb",
		Args: []Arg{clientGUID, s.instanceName, blob}}}, trailer)
}

func (s *Service) send(conn net.Conn, messages []Message, trailer []byte) {
	if _, err := conn.Write(EncodeBundle(messages, trailer)); err != nil {
		_ = conn.Close()
	}
}

func (s *Service) setConnected(connected bool) {
	s.mu.Lock()
	if s.snapshot.Connected == connected {
		s.mu.Unlock()
		return
	}
	if connected {
		s.snapshot.Connected = true
	} else {
		// Rebuild, so a disconnect clears stale deck data.
		s.snapshot = EmptySnapshot(false)
	}
	snapshot := s.snapshot.Clone()
	s.mu.Unlock()
	if s.onSnapshot != nil {
		s.onSnapshot(snapshot)
	}
}
