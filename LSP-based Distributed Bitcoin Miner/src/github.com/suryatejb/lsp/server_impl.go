// Package lsp implements the Layered Service Protocol for reliable communication
// over UDP networks with features including connection management, flow control,
// message ordering, and automatic retransmission.

package lsp

import (
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/suryatejb/lspnet"
)

// udpBufferSize is the maximum UDP packet size; 1500 is the typical Ethernet MTU size.
const udpBufferSize = 1500

////////////////////////// Type Definitions //////////////////////////

// server manages all client connections and server-side logic.
type server struct {
	params         *Params                  // Server parameters
	udpConn        *lspnet.UDPConn          // Underlying UDP connection
	nextConnID     int                      // Next connection ID to assign
	connsByID      map[int]*connection      // Active connections by ID
	connsByAddr    map[string]int           // Active connections by address
	netEvents      chan *netEvent           // Incoming network events
	readRequests   chan *readRequestSrv     // Application read requests
	writeRequests  chan *writeRequestSrv    // Application write requests
	closeConnReqs  chan *closeConnRequest   // Application close connection requests
	closeServerReq chan *closeServerRequest // Application close server requests
	epochTicks     chan struct{}            // Epoch tick events
	fastEpochTicks chan struct{}            // Fast epoch tick events for closing
	orderRequests  chan *orderRequest       // Write order requests
	pendingWrites  chan int                 // Tracks number of pending writes
	queryPending   chan chan int            // Queries number of pending writes
	shutdown       chan struct{}            // Shutdown signal
	deliveryQueue  []*ReadResultSrv         // Queued read results
	pendingReads   []chan *ReadResultSrv    // Waiting application read requests
	currentEpoch   int                      // Current epoch count
	isClosing      bool                     // Whether server is closing
	anyConnLost    bool                     // Whether any connection was lost
	closeReply     chan error               // Channel to signal close completion
}

// connection represents the state of a single client connection.
type connection struct {
	id               int               // Connection ID
	addr             *lspnet.UDPAddr   // Client address
	clientISN        int               // Client initial sequence number
	nextRecvSeq      int               // Next expected receive sequence number
	recvBuffer       map[int][]byte    // Out-of-order received data
	lastContiguous   int               // Last contiguous sequence number received
	nextSendSeq      int               // Next sequence number to send
	writeQueue       []*Message        // Messages queued for sending
	inflightMessages map[int]*Message  // Messages sent but not yet acknowledged
	lastSentEpoch    map[int]int       // Epoch when each message was last sent
	retryBackoff     map[int]int       // Backoff interval for each message
	nextWriteOrder   uint64            // Next write order number expected
	writeOrderQueue  map[uint64][]byte // Out-of-order write payloads
	writesIssued     uint64            // Total writes issued
	silentEpochs     int               // Epochs since last received message
	isLost           bool              // Whether connection is considered lost
	lostSignalSent   bool              // Whether lost signal has been sent
	isClosing        bool              // Whether connection is closing
	closeAfterOrder  uint64            // Close after this write order
	closedSignalSent bool              // Whether closed signal has been sent
}

// Internal event/request types for channel communication.
type netEvent struct {
	addr *lspnet.UDPAddr
	msg  *Message
}

type readRequestSrv struct {
	reply chan *ReadResultSrv
}

type writeRequestSrv struct {
	connID  int
	order   uint64
	payload []byte
}

type closeConnRequest struct {
	connID      int
	lastOrderID uint64
}

type closeServerRequest struct {
	reply chan error
}

type orderRequest struct {
	connID int
	reply  chan uint64
}

// ReadResultSrv encapsulates a read result for the application.
type ReadResultSrv struct {
	ConnID  int
	Payload []byte
	Error   error
}

////////////////////////// Constructor //////////////////////////

// NewServer creates a new server and starts listening on the specified port.
func NewServer(port int, params *Params) (Server, error) {
	addr, err := lspnet.ResolveUDPAddr("udp", ":"+strconv.Itoa(port))
	if err != nil {
		return nil, err
	}
	udpConn, err := lspnet.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}

	s := &server{
		params:         params,
		udpConn:        udpConn,
		nextConnID:     1,
		connsByID:      make(map[int]*connection),
		connsByAddr:    make(map[string]int),
		netEvents:      make(chan *netEvent, 1),
		readRequests:   make(chan *readRequestSrv),
		writeRequests:  make(chan *writeRequestSrv),
		closeConnReqs:  make(chan *closeConnRequest),
		closeServerReq: make(chan *closeServerRequest),
		epochTicks:     make(chan struct{}),
		fastEpochTicks: make(chan struct{}),
		orderRequests:  make(chan *orderRequest),
		pendingWrites:  make(chan int),
		queryPending:   make(chan chan int),
		shutdown:       make(chan struct{}),
		deliveryQueue:  make([]*ReadResultSrv, 0),
		pendingReads:   make([]chan *ReadResultSrv, 0),
	}

	go s.dispatcher()
	go s.networkReader()
	go s.epochManager(params.EpochMillis, s.epochTicks)       // regular epoch manager
	go s.epochManager(params.EpochMillis/4, s.fastEpochTicks) // fast epoch manager
	go s.writeSequencer()
	go s.pendingWriteTracker()

	return s, nil
}

////////////////////////// Core Goroutines //////////////////////////

// dispatcher is the main coordinator goroutine that manages server state.
func (s *server) dispatcher() {
	defer s.cleanup()

	for {
		select {
		case ev := <-s.netEvents:
			s.handleNetEvent(ev)
			s.flushReadDelivery()
		case req := <-s.writeRequests:
			if c, ok := s.connsByID[req.connID]; ok && c.addr != nil && !c.isLost {
				if s.enqueueWrite(c, req.order, req.payload) {
					s.trySend(c)
				}
			}
		case req := <-s.readRequests:
			// Return error immediately if server is closing
			if s.isClosing {
				req.reply <- &ReadResultSrv{Error: errors.New("server closed")}
			} else {
				s.pendingReads = append(s.pendingReads, req.reply)
				s.flushReadDelivery()
			}
		case req := <-s.closeConnReqs:
			if c, ok := s.connsByID[req.connID]; ok {
				s.markConnClosing(c, req.lastOrderID)
				s.tryFinishConnection(c)
			}
		case req := <-s.closeServerReq:
			s.isClosing = true
			s.closeReply = req.reply
			for _, c := range s.connsByID {
				c.isClosing = true
				c.closeAfterOrder = c.writesIssued
				s.retransmitAll(c)
			}
		case <-s.epochTicks:
			s.currentEpoch++
			s.handleEpoch()
		case <-s.fastEpochTicks:
			if s.isClosing {
				for _, c := range s.connsByID {
					s.retransmitAll(c)
				}
			}
		case <-s.shutdown:
			return
		}
		if s.isClosing && s.tryFinishAll() {
			return
		}
	}
}

func (s *server) cleanup() {
	close(s.shutdown)
	s.udpConn.Close()
}

// networkReader continuously reads incoming UDP packets and forwards them to the dispatcher.
func (s *server) networkReader() {
	for {
		select {
		case <-s.shutdown:
			return
		default:
			payload := make([]byte, udpBufferSize)
			n, addr, err := s.udpConn.ReadFromUDP(payload)
			if err != nil {
				continue
			}
			var msg Message
			if json.Unmarshal(payload[:n], &msg) != nil {
				continue
			}
			s.netEvents <- &netEvent{addr: addr, msg: &msg}
		}
	}
}

// epochManager sends periodic tick events for retransmissions and connection management.
func (s *server) epochManager(interval int, tickChan chan<- struct{}) {
	ticker := time.NewTicker(time.Duration(interval) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			tickChan <- struct{}{}
		case <-s.shutdown:
			return
		}
	}
}

// writeSequencer provides ordered sequence numbers for write operations.
func (s *server) writeSequencer() {
	for {
		select {
		case req := <-s.orderRequests:
			if c, ok := s.connsByID[req.connID]; ok && !c.isClosing && c.addr != nil {
				c.writesIssued++
				req.reply <- c.writesIssued
			} else {
				req.reply <- 0 // Indicate failure
			}
		case <-s.shutdown:
			return
		}
	}
}

// pendingWriteTracker maintains a count of outstanding write operations.
func (s *server) pendingWriteTracker() {
	var count int
	for {
		select {
		case delta := <-s.pendingWrites:
			count += delta
		case replyCh := <-s.queryPending:
			replyCh <- count
		case <-s.shutdown:
			return
		}
	}
}

////////////////////////// Message Handling //////////////////////////

// handleNetEvent processes incoming network messages and routes them appropriately.
func (s *server) handleNetEvent(ev *netEvent) {
	if ev.msg.Type == MsgConnect {
		s.handleConnect(ev.addr, ev.msg)
		return
	}

	c := s.lookupConnection(ev.addr, ev.msg.ConnID)
	if c == nil {
		return // No connection found
	}

	c.silentEpochs = 0
	if c.isLost {
		c.isLost = false
		c.lostSignalSent = false
	}

	switch ev.msg.Type {
	case MsgData:
		if len(ev.msg.Payload) < ev.msg.Size || CalculateChecksum(ev.msg.ConnID, ev.msg.SeqNum, ev.msg.Size, ev.msg.Payload[:ev.msg.Size]) != ev.msg.Checksum {
			return // Corrupt
		}
		s.send(c.addr, NewAck(c.id, ev.msg.SeqNum))
		if ev.msg.SeqNum < c.nextRecvSeq {
			return // Duplicate
		}
		// Drop data for closing connections to comply with CloseConn spec
		if c.isClosing {
			return
		}
		c.recvBuffer[ev.msg.SeqNum] = ev.msg.Payload[:ev.msg.Size]
		if s.processContiguousData(c) {
			s.send(c.addr, NewCAck(c.id, c.lastContiguous))
		}
	case MsgAck:
		if ev.msg.SeqNum > 0 {
			delete(c.inflightMessages, ev.msg.SeqNum)
			delete(c.lastSentEpoch, ev.msg.SeqNum)
			delete(c.retryBackoff, ev.msg.SeqNum)
			s.trySend(c)
			s.tryFinishConnection(c)
		}
	case MsgCAck:
		for seq := range c.inflightMessages {
			if seq <= ev.msg.SeqNum {
				delete(c.inflightMessages, seq)
				delete(c.lastSentEpoch, seq)
				delete(c.retryBackoff, seq)
			}
		}
		s.trySend(c)
		s.tryFinishConnection(c)
	}
}

// handleConnect establishes new client connections and assigns connection IDs.
func (s *server) handleConnect(addr *lspnet.UDPAddr, msg *Message) {
	if s.isClosing {
		return // Do not accept new connections
	}
	addrStr := addr.String()
	if connID, ok := s.connsByAddr[addrStr]; ok {
		if c, ok := s.connsByID[connID]; ok && !c.isLost {
			c.silentEpochs = 0
			s.send(addr, NewAck(c.id, msg.SeqNum))
			return
		}
	}

	connID := s.nextConnID
	s.nextConnID++
	c := &connection{
		id:               connID,
		addr:             addr,
		clientISN:        msg.SeqNum,
		nextRecvSeq:      msg.SeqNum + 1,
		lastContiguous:   msg.SeqNum,
		nextSendSeq:      1,
		recvBuffer:       make(map[int][]byte),
		writeQueue:       make([]*Message, 0),
		inflightMessages: make(map[int]*Message),
		lastSentEpoch:    make(map[int]int),
		retryBackoff:     make(map[int]int),
		writeOrderQueue:  make(map[uint64][]byte),
		nextWriteOrder:   1,
	}
	s.connsByID[connID] = c
	s.connsByAddr[addrStr] = connID
	s.send(addr, NewAck(connID, msg.SeqNum))
}

// handleEpoch performs periodic maintenance for all connections.
func (s *server) handleEpoch() {
	for _, c := range s.connsByID {
		if c.addr == nil {
			continue // Already torn down
		}
		c.silentEpochs++
		if s.params.EpochLimit > 0 && c.silentEpochs >= s.params.EpochLimit {
			if len(c.inflightMessages) == 0 && len(c.writeQueue) == 0 && len(c.recvBuffer) == 0 {
				c.isLost = true
			}
			if c.isLost && !c.lostSignalSent {
				s.deliveryQueue = append(s.deliveryQueue, &ReadResultSrv{ConnID: c.id, Error: errors.New("connection lost")})
				c.lostSignalSent = true
				s.flushReadDelivery()
			}
		}

		resentSomething := s.retransmitAll(c)

		if !resentSomething && len(c.inflightMessages) == 0 && len(c.writeQueue) == 0 && !c.isClosing {
			s.send(c.addr, NewAck(c.id, 0)) // Heartbeat
		}
		s.trySend(c)
	}
}

////////////////////////// Flow Control and Transmission //////////////////////////

// retransmitAll resends unacknowledged messages using exponential backoff.
func (s *server) retransmitAll(c *connection) bool {
	resentSomething := false
	for seq, msg := range c.inflightMessages {
		gap, ok := c.retryBackoff[seq]
		if !ok || s.isClosing {
			gap = 1
		}
		if s.currentEpoch-c.lastSentEpoch[seq] >= gap {
			s.send(c.addr, msg)
			c.lastSentEpoch[seq] = s.currentEpoch
			newGap := gap * 2
			if s.params.MaxBackOffInterval > 0 && newGap > s.params.MaxBackOffInterval {
				newGap = s.params.MaxBackOffInterval
			}
			c.retryBackoff[seq] = newGap
			resentSomething = true
		}
	}
	return resentSomething
}

// enqueueWrite adds a payload to the connection's write queue with proper ordering.
func (s *server) enqueueWrite(c *connection, order uint64, payload []byte) bool {
	if order == c.nextWriteOrder {
		c.writeQueue = append(c.writeQueue, NewData(c.id, 0, len(payload), payload, 0))
		c.nextWriteOrder++
		for {
			p, ok := c.writeOrderQueue[c.nextWriteOrder]
			if !ok {
				break
			}
			c.writeQueue = append(c.writeQueue, NewData(c.id, 0, len(p), p, 0))
			delete(c.writeOrderQueue, c.nextWriteOrder)
			c.nextWriteOrder++
		}
		return true
	} else if order > c.nextWriteOrder {
		c.writeOrderQueue[order] = payload
	}
	return false
}

// trySend transmits queued messages up to the window size limit.
func (s *server) trySend(c *connection) {
	capacity := s.params.WindowSize
	if s.params.MaxUnackedMessages > 0 && capacity > s.params.MaxUnackedMessages {
		capacity = s.params.MaxUnackedMessages
	}
	for len(c.inflightMessages) < capacity && len(c.writeQueue) > 0 {
		msg := c.writeQueue[0]
		c.writeQueue = c.writeQueue[1:]
		msg.SeqNum = c.nextSendSeq
		msg.Checksum = CalculateChecksum(msg.ConnID, msg.SeqNum, msg.Size, msg.Payload)
		c.nextSendSeq++
		s.send(c.addr, msg)
		c.inflightMessages[msg.SeqNum] = msg
		c.lastSentEpoch[msg.SeqNum] = s.currentEpoch
		c.retryBackoff[msg.SeqNum] = 1
	}
}

////////////////////////// Data Processing //////////////////////////

// processContiguousData moves received data to the delivery queue in order.
func (s *server) processContiguousData(c *connection) bool {
	progressed := false
	for {
		payload, ok := c.recvBuffer[c.nextRecvSeq]
		if !ok {
			break
		}
		s.deliveryQueue = append(s.deliveryQueue, &ReadResultSrv{ConnID: c.id, Payload: payload})
		delete(c.recvBuffer, c.nextRecvSeq)
		c.lastContiguous = c.nextRecvSeq
		c.nextRecvSeq++
		progressed = true
	}
	return progressed
}

// flushReadDelivery delivers queued results to waiting read operations.
func (s *server) flushReadDelivery() {
	for len(s.deliveryQueue) > 0 && len(s.pendingReads) > 0 {
		result := s.deliveryQueue[0]
		s.deliveryQueue = s.deliveryQueue[1:]
		replyCh := s.pendingReads[0]
		s.pendingReads = s.pendingReads[1:]
		replyCh <- result
	}
}

////////////////////////// Connection Management //////////////////////////

// markConnClosing initiates connection closure and purges buffered data.
func (s *server) markConnClosing(c *connection, lastOrderID uint64) {
	if lastOrderID == 0 {
		lastOrderID = c.writesIssued
	}
	if lastOrderID > c.closeAfterOrder {
		c.closeAfterOrder = lastOrderID
	}
	c.isClosing = true

	// Purge existing data and enqueue close error to comply with CloseConn spec
	s.purgeConnectionData(c)
}

// purgeConnectionData removes buffered data and enqueues connection closed errors.
func (s *server) purgeConnectionData(c *connection) {
	// Clear receive buffer - no more data should be delivered
	c.recvBuffer = make(map[int][]byte)

	// Remove any pending data for this connection from delivery queue
	newQueue := make([]*ReadResultSrv, 0)
	for _, item := range s.deliveryQueue {
		if item.ConnID != c.id {
			newQueue = append(newQueue, item)
		}
	}
	s.deliveryQueue = newQueue

	// Add connection closed error to delivery queue
	s.deliveryQueue = append(s.deliveryQueue, &ReadResultSrv{
		ConnID: c.id,
		Error:  errors.New("connection closed"),
	})
}

// tryFinishConnection checks if a connection can be safely torn down.
func (s *server) tryFinishConnection(c *connection) {
	if c.isClosing && c.addr != nil &&
		len(c.inflightMessages) == 0 && len(c.writeQueue) == 0 && len(c.writeOrderQueue) == 0 &&
		c.nextWriteOrder > c.closeAfterOrder {

		if !c.closedSignalSent {
			if _, ok := c.recvBuffer[c.nextRecvSeq]; !ok {
				s.deliveryQueue = append(s.deliveryQueue, &ReadResultSrv{ConnID: c.id, Error: errors.New("connection closed")})
				c.closedSignalSent = true
				s.flushReadDelivery()
			}
		}
		delete(s.connsByAddr, c.addr.String())
		c.addr = nil // Mark as torn down
	}
}

// tryFinishAll checks if all connections are closed and finalizes server shutdown.
func (s *server) tryFinishAll() bool {
	for _, c := range s.connsByID {
		s.tryFinishConnection(c)
		if c.addr != nil {
			return false // At least one connection is still active
		}
		if c.isLost {
			s.anyConnLost = true
		}
	}
	if s.closeReply != nil {
		if s.anyConnLost {
			s.closeReply <- errors.New("one or more connections were lost")
		} else {
			s.closeReply <- nil
		}
	}
	return true
}

////////////////////////// Helper Methods //////////////////////////

// lookupConnection finds an active connection by address or connection ID.
func (s *server) lookupConnection(addr *lspnet.UDPAddr, connID int) *connection {
	addrStr := addr.String()
	if id, ok := s.connsByAddr[addrStr]; ok {
		return s.connsByID[id]
	}
	if c, ok := s.connsByID[connID]; ok {
		// Prevent resurrection of torn-down or closing connections
		if c.addr == nil || c.isClosing {
			return nil
		}
		if c.addr != nil {
			delete(s.connsByAddr, c.addr.String())
		}
		c.addr = addr
		s.connsByAddr[addrStr] = connID
		return c
	}
	return nil
}

// send marshals a message to JSON and transmits it over UDP.
func (s *server) send(addr *lspnet.UDPAddr, msg *Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	s.udpConn.WriteToUDP(data, addr)
}

////////////////////////// Public API methods //////////////////////////

// Read blocks until a message is available from any client connection,
// returning the connection ID, payload, and any error that occurred.
func (s *server) Read() (int, []byte, error) {
	replyCh := make(chan *ReadResultSrv, 1)
	s.readRequests <- &readRequestSrv{reply: replyCh}
	result := <-replyCh
	return result.ConnID, result.Payload, result.Error
}

// Write queues a payload for transmission to the specified client connection,
// maintaining proper message ordering and flow control constraints.
func (s *server) Write(connID int, payload []byte) error {
	s.pendingWrites <- 1
	defer func() { s.pendingWrites <- -1 }()

	orderCh := make(chan uint64, 1)
	s.orderRequests <- &orderRequest{connID: connID, reply: orderCh}
	order := <-orderCh
	if order == 0 {
		return errors.New("connection not found or closing")
	}
	s.writeRequests <- &writeRequestSrv{connID: connID, order: order, payload: payload}
	return nil
}

// CloseConn initiates graceful closure of the specified client connection.
// It prevents new data from being delivered and prepares for connection teardown.
func (s *server) CloseConn(connID int) error {
	s.closeConnReqs <- &closeConnRequest{connID: connID}
	return nil
}

// Close gracefully shuts down the server, waiting for all connections
// to be properly closed and all pending operations to complete.
func (s *server) Close() error {
	// Wait for pending writes to be submitted to the dispatcher
	queryCh := make(chan int)
	for {
		s.queryPending <- queryCh
		if <-queryCh == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	replyCh := make(chan error, 1)
	s.closeServerReq <- &closeServerRequest{reply: replyCh}
	return <-replyCh
}