package ws

import "sync"

// What a socket that has not paired yet is allowed to occupy.
//
// The authentication deadline bounds how long one unpaired socket may live; it
// says nothing about how many there may be. Only paired clients count against
// max_sessions, so without a second limit anyone who can reach the port — with
// remote access on, that is the internet — can hold sockets open in bulk,
// re-opening each as its deadline expires, and spend the machine's file
// descriptors while the owner's phone gets nothing.
//
// The per-address share is the part that matters. A global cap alone would just
// move the problem: one host could fill it and lock everyone out. With both, a
// single source can never take more than its share, and the global figure is
// what is left for everyone else.
const (
	maxPendingConns       = 32
	maxPendingConnsPerAdr = 4
)

// pendingConns counts sockets that have connected but not yet paired.
//
// A slot is taken when a connection arrives and given back the moment it pairs,
// so a paired phone never occupies one — its cost is accounted for by the
// session cap instead. Slots are also given back when the socket goes away,
// whether that is the deadline, a network drop or a refused key.
type pendingConns struct {
	mu      sync.Mutex
	total   int
	byAddr  map[string]int
	limit   int
	perAddr int
}

func newPendingConns(limit, perAddr int) *pendingConns {
	return &pendingConns{
		byAddr:  make(map[string]int),
		limit:   limit,
		perAddr: perAddr,
	}
}

// acquire takes a slot for addr, reporting whether one was available.
func (p *pendingConns) acquire(addr string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.total >= p.limit || p.byAddr[addr] >= p.perAddr {
		return false
	}
	p.total++
	p.byAddr[addr]++
	return true
}

// release gives back a slot taken for addr. Exactly one release must follow
// each successful acquire; Hub.releasePending is what guarantees that, since a
// connection can reach the end of its life either by pairing or by dropping.
func (p *pendingConns) release(addr string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.total > 0 {
		p.total--
	}
	if n := p.byAddr[addr]; n > 1 {
		p.byAddr[addr] = n - 1
	} else {
		// The map is cleared rather than left holding zeroes, so a stream of
		// connections from changing addresses cannot grow it without limit.
		delete(p.byAddr, addr)
	}
}

// count returns how many unpaired sockets are currently held. For tests.
func (p *pendingConns) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.total
}
