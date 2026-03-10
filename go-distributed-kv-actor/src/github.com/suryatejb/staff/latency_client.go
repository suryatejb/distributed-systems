// Package staff contains utilities used to control network conditions
// during tests.
//
// You do not need to use this package in your P3 solution.
package staff

import (
	"io"
	"net"
	"net/rpc"
	"time"
)

// For testing use: Same as rpc.Dial, except the returned
// rpc.Client's TCP connection has an artificial
// latency equal to the most recent value set in SetArtiLatencyMs().
func DialWithLatency(address string) (*rpc.Client, error) {
	var conn io.ReadWriteCloser
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return nil, err
	}
	latencyMs := artiLatencyMs()
	if latencyMs != 0 {
		conn = newLatencyConn(conn, time.Duration(latencyMs)*time.Millisecond)
	}
	return rpc.NewClient(conn), nil
}
