package staff

import (
	"fmt"
	"io"
	"sync"
	"time"
)

type timedMessage struct {
	p    []byte
	when time.Time
}

type latencyConn struct {
	conn    io.ReadWriteCloser
	latency time.Duration
	queue   []*timedMessage
	lastErr error
	mux     *sync.Mutex
}

// For testing use: wrapper around a tcp Conn that adds sender-side
// latency (while preserving message order).
func newLatencyConn(conn io.ReadWriteCloser, latency time.Duration) io.ReadWriteCloser {
	return &latencyConn{conn, latency, make([]*timedMessage, 0), nil, &sync.Mutex{}}
}

func (conn *latencyConn) Read(p []byte) (n int, err error) {
	return conn.conn.Read(p)
}

func (conn *latencyConn) Write(p []byte) (n int, err error) {
	conn.mux.Lock()
	defer conn.mux.Unlock()

	if conn.lastErr != nil {
		return 0, conn.lastErr
	}

	when := time.Now().Add(conn.latency)
	// Writer.Write says "Implementations must not retain p", so we copy it.
	pCopy := make([]byte, len(p))
	copy(pCopy, p)
	conn.queue = append(conn.queue, &timedMessage{pCopy, when})
	if len(conn.queue) == 1 {
		// Resart the sendNext() chain.
		go conn.sendNext(conn.latency)
	}
	return len(p), nil
}

// There should be at most one of these running at time.
func (conn *latencyConn) sendNext(delay time.Duration) {
	time.Sleep(delay)

	conn.mux.Lock()
	defer conn.mux.Unlock()

	if conn.lastErr != nil {
		return
	}
	first := conn.queue[0]
	// Note this will block inside the lock, but it's okay because it will
	// just make Write() block as well, which is what conn.conn intended.
	_, err := conn.conn.Write(first.p)
	if err != nil {
		conn.lastErr = err
		return
	}
	conn.queue[0] = nil // Allow GC
	conn.queue = conn.queue[1:]
	if len(conn.queue) != 0 {
		when := conn.queue[0].when
		go conn.sendNext(when.Sub(time.Now()))
	}
}

func (conn *latencyConn) Close() error {
	err := conn.conn.Close()
	conn.mux.Lock()
	if conn.lastErr != nil {
		conn.lastErr = fmt.Errorf("Already closed")
	}
	conn.mux.Unlock()
	return err
}
