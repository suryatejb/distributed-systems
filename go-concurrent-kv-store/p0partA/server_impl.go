// KeyValueServer implementation using Go concurrency primitives.

package p0partA

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/suryatejb/distributed-systems/p0partA/kvstore"
)

const (
	maxQueueSize = 500 // Maximum buffered messages per client
)

type keyValueServer struct {
	store        kvstore.KVStore      // Backend key-value store
	listener     net.Listener         // TCP listener for incoming connections
	clients      map[net.Conn]*client // Map of active clients
	addClient    chan net.Conn        // Channel to add new clients
	removeClient chan net.Conn        // Channel to remove disconnected clients
	dbRequest    chan *dbOperation    // Channel for database operations
	countRequest chan *countQuery     // Channel for count queries
	shutdown     chan struct{}        // Channel for server shutdown
	countActive  int                  // Number of active clients
	countDropped int                  // Number of dropped clients
}

type client struct {
	conn     net.Conn      // TCP connection to client
	outQueue chan string   // Buffered channel for outgoing messages
	done     chan struct{} // Signal when client should terminate
}

type dbOperation struct {
	opType   string        // Operation type: Put, Get, Delete, Update
	key      string        // Key for the operation
	value    []byte        // Value to set (for Put and Update)
	oldValue []byte        // Old value (for Update)
	newValue []byte        // New value (for Update)
	response chan [][]byte // Channel to return Get results
}

// countQuery handles requests for active/dropped client counts
type countQuery struct {
	isActive bool     // true for active count, false for dropped count
	response chan int // Channel to return the count
}

// New creates and returns (but does not start) a new KeyValueServer.
func New(store kvstore.KVStore) KeyValueServer {
	return &keyValueServer{
		store:        store,
		clients:      make(map[net.Conn]*client),
		addClient:    make(chan net.Conn),
		removeClient: make(chan net.Conn),
		dbRequest:    make(chan *dbOperation),
		countRequest: make(chan *countQuery),
		shutdown:     make(chan struct{}),
	}
}

func (kvs *keyValueServer) Start(port int) error {
	listener, err := net.Listen("tcp", ":"+strconv.Itoa(port)) // Create TCP listener on specified port
	if err != nil {
		return fmt.Errorf("failed to start server on port %d: %w", port, err)
	}
	kvs.listener = listener
	// Start background routines for handling clients and operations
	go kvs.mainRoutine()
	go kvs.acceptRoutine()
	return nil
}

func (kvs *keyValueServer) Close() {
	if kvs.listener != nil {
		kvs.listener.Close()
	}
	close(kvs.shutdown) // Signal all goroutines to terminate
}

func (kvs *keyValueServer) CountActive() int {
	resp := make(chan int)
	kvs.countRequest <- &countQuery{true, resp}
	return <-resp
}

func (kvs *keyValueServer) CountDropped() int {
	resp := make(chan int)
	kvs.countRequest <- &countQuery{false, resp}
	return <-resp
}

// acceptRoutine continuously accepts new client connections
func (kvs *keyValueServer) acceptRoutine() {
	for {
		conn, err := kvs.listener.Accept()
		if err != nil {
			return // Listener closed, exit routine
		}
		select {
		case kvs.addClient <- conn: // Send new connection to main routine
		case <-kvs.shutdown:
			conn.Close()
			return
		}
	}
}

// mainRoutine is the central coordinator handling all server operations
func (kvs *keyValueServer) mainRoutine() {
	for {
		select {
		case conn := <-kvs.addClient:
			// Create new client with buffered output queue
			cli := &client{conn, make(chan string, maxQueueSize), make(chan struct{})}
			kvs.clients[conn] = cli
			kvs.countActive++
			// Start read/write routines for this client
			go kvs.readRoutine(cli)
			go kvs.writeRoutine(cli)

		case conn := <-kvs.removeClient:
			// Clean up disconnected client
			if cli, exists := kvs.clients[conn]; exists {
				delete(kvs.clients, conn)
				kvs.countActive--
				kvs.countDropped++
				conn.Close()
				close(cli.done) // Signal client routines to terminate
			}

		case op := <-kvs.dbRequest:
			// Execute database operation
			switch op.opType {
			case "Put":
				kvs.store.Put(op.key, op.value)
			case "Get":
				if op.response != nil {
					op.response <- kvs.store.Get(op.key)
				}
			case "Delete":
				kvs.store.Delete(op.key)
			case "Update":
				kvs.store.Update(op.key, op.oldValue, op.newValue)
			}

		case query := <-kvs.countRequest:
			// Return requested count
			if query.isActive {
				query.response <- kvs.countActive
			} else {
				query.response <- kvs.countDropped
			}

		case <-kvs.shutdown:
			// Clean shutdown: close all client connections
			for conn, cli := range kvs.clients {
				conn.Close()
				close(cli.done)
			}
			return
		}
	}
}

// readRoutine handles incoming messages from a client
func (kvs *keyValueServer) readRoutine(cli *client) {
	defer func() {
		select {
		case kvs.removeClient <- cli.conn:
		case <-kvs.shutdown:
		case <-cli.done:
		}
	}()
	scanner := bufio.NewScanner(cli.conn)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		select {
		case <-cli.done:
			return
		case <-kvs.shutdown:
			return
		default:
		}
		// Parse command: format is "operation:key:value"
		parts := strings.Split(line, ":")
		if len(parts) < 2 {
			continue
		}
		op := &dbOperation{opType: parts[0], key: parts[1]}
		switch parts[0] {
		case "Put":
			if len(parts) < 3 {
				continue
			}
			op.value = []byte(strings.Join(parts[2:], ":")) // Handle values with colons
		case "Get":
			op.response = make(chan [][]byte, 1) // Buffered to avoid blocking
		case "Delete":
		case "Update":
			if len(parts) < 4 {
				continue
			}
			op.oldValue = []byte(parts[2])
			op.newValue = []byte(strings.Join(parts[3:], ":"))
		default:
			continue
		}
		// Send operation to main routine
		select {
		case kvs.dbRequest <- op:
		case <-kvs.shutdown:
			return
		case <-cli.done:
			return
		}
		// For Get operations, wait for response and send to client
		if parts[0] == "Get" && op.response != nil {
			select {
			case values := <-op.response:
				for _, value := range values {
					message := strings.TrimSpace(string(value))
					select {
					case cli.outQueue <- fmt.Sprintf("%s:%s\n", op.key, message):
						// Successfully queued message for client
					default:
						// Output queue full, drop message (slow client handling)
					}
				}
			case <-kvs.shutdown:
				return
			case <-cli.done:
				return
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return
	}
}

// writeRoutine sends queued messages to a client
func (kvs *keyValueServer) writeRoutine(client *client) {
	defer close(client.outQueue)
	for {
		select {
		case msg, ok := <-client.outQueue:
			if !ok {
				return // Channel closed, exit routine
			}
			if _, err := client.conn.Write([]byte(msg)); err != nil {
				return // Write failed, client likely disconnected
			}
		case <-client.done:
			return
		case <-kvs.shutdown:
			return
		}
	}
}
