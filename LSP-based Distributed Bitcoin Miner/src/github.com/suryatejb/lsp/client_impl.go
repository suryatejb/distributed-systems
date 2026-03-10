// Package lsp implements the Layered Service Protocol for reliable communication
// over UDP networks with features including connection management, flow control,
// message ordering, and automatic retransmission.

package lsp

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/suryatejb/lspnet"
)

////////////////////////// Type Definitions //////////////////////////

// client represents the state of a client connection.
type client struct {
	connID      int
	udpConn     *lspnet.UDPConn
	serverAddr  *lspnet.UDPAddr
	params      *Params
	initialSeqN int

	// Sequence numbers
	nextSendSeq int // Next data sequence number to send
	nextRecvSeq int // Next data sequence number expected from server

	// Sending logic
	writeQueue       []*Message        // Payloads waiting to be sent
	inflightMessages map[int]*Message  // Sent but unacknowledged messages
	lastSentEpoch    map[int]int       // Epoch when each unacked message was last sent
	retryBackoff     map[int]int       // Current backoff interval for each unacked message
	writeOrderQueue  map[uint64][]byte // Out-of-order user writes
	nextWriteOrder   uint64

	// Receiving logic
	recvBuffer        map[int][]byte // Out-of-order received data
	lastContiguousSeq int            // Last contiguous sequence number received

	// Liveness and termination
	currentEpoch           int        // Current epoch count
	epochsSinceLastMessage int        // Epochs since last received message
	isConnectionLost       bool       // Whether connection is considered lost
	lostSignalSent         bool       // Whether lost signal has been sent
	isClosing              bool       // Whether connection is closing
	closedSignalSent       bool       // Whether closed signal has been sent
	closeReply             chan error // Channel to signal close completion

	// Coordination channels
	netEvents          chan *Message      // Incoming network messages
	writeRequests      chan *writeRequest // Application write requests
	readRequests       chan *readRequest  // Application read requests
	closeRequest       chan *closeRequest // Application close requests
	epochTicks         chan struct{}      // Epoch tick events
	writeOrderRequests chan chan uint64   // Write order requests
	shutdown           chan struct{}      // Shutdown signal
	connected          chan struct{}      // Signals successful connection

	// Read delivery
	pendingReads  []chan *ReadResult // Waiting application read requests
	deliveryQueue []*ReadResult      // Queued read results
}

// Encapsulates a read result for the application.
type ReadResult struct {
	Payload []byte
	Error   error
}

// Internal request types for channel communication.
type writeRequest struct {
	order   uint64
	payload []byte
}

type readRequest struct {
	reply chan *ReadResult
}

type closeRequest struct {
	reply chan error
}

////////////////////////// Constructor //////////////////////////

// NewClient creates a new client and establishes a connection to the server.
func NewClient(hostport string, initialSeqNum int, params *Params) (Client, error) {
	serverAddr, err := lspnet.ResolveUDPAddr("udp", hostport)
	if err != nil {
		return nil, err
	}
	udpConn, err := lspnet.DialUDP("udp", nil, serverAddr)
	if err != nil {
		return nil, err
	}

	c := &client{
		connID:             0, // Not connected yet
		udpConn:            udpConn,
		serverAddr:         serverAddr,
		params:             params,
		initialSeqN:        initialSeqNum,
		nextSendSeq:        initialSeqNum + 1,
		nextRecvSeq:        1,
		writeQueue:         make([]*Message, 0),
		inflightMessages:   make(map[int]*Message),
		lastSentEpoch:      make(map[int]int),
		retryBackoff:       make(map[int]int),
		writeOrderQueue:    make(map[uint64][]byte),
		nextWriteOrder:     1,
		recvBuffer:         make(map[int][]byte),
		lastContiguousSeq:  0,
		netEvents:          make(chan *Message, 1),
		writeRequests:      make(chan *writeRequest),
		readRequests:       make(chan *readRequest),
		closeRequest:       make(chan *closeRequest),
		epochTicks:         make(chan struct{}),
		writeOrderRequests: make(chan chan uint64),
		shutdown:           make(chan struct{}),
		connected:          make(chan struct{}),
		pendingReads:       make([]chan *ReadResult, 0),
		deliveryQueue:      make([]*ReadResult, 0),
	}

	go c.run()
	go c.networkReader()
	go c.epochManager()
	go c.writeSequencer()
	go c.connectionManager()

	// Wait for connection or timeout
	select {
	case <-c.connected:
		return c, nil
	case <-time.After(time.Duration(params.EpochLimit*params.EpochMillis) * time.Millisecond):
		c.Close()
		return nil, errors.New("connection timeout")
	}
}

////////////////////////// Core Goroutines //////////////////////////

// run is the main coordinator goroutine that handles client operations.
func (c *client) run() {
	defer c.cleanup()

	for {
		select {
		case msg := <-c.netEvents:
			c.handleNetEvent(msg)
		case req := <-c.writeRequests:
			c.handleWriteRequest(req)
		case req := <-c.readRequests:
			c.pendingReads = append(c.pendingReads, req.reply)
			c.flushReadDelivery()
		case req := <-c.closeRequest:
			c.isClosing = true
			c.closeReply = req.reply
			if c.tryFinishClose(req.reply) {
				return
			}
		case <-c.epochTicks:
			c.handleEpoch()
		case <-c.shutdown:
			return
		}
		if c.isClosing && c.tryFinishClose(c.closeReply) {
			return
		}
	}
}

// cleanup performs final cleanup when the client shuts down.
func (c *client) cleanup() {
	close(c.shutdown) // Signal other goroutines to stop
	c.udpConn.Close()
}

// networkReader continuously reads incoming UDP packets and forwards them to the dispatcher.
func (c *client) networkReader() {
	for {
		select {
		case <-c.shutdown:
			return
		default:
			payload := make([]byte, udpBufferSize)
			n, err := c.udpConn.Read(payload)
			if err != nil {
				continue
			}
			var msg Message
			if json.Unmarshal(payload[:n], &msg) != nil {
				continue
			}
			c.netEvents <- &msg
		}
	}
}

// epochManager sends periodic tick events for retransmissions and connection management.
func (c *client) epochManager() {
	ticker := time.NewTicker(time.Duration(c.params.EpochMillis) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.epochTicks <- struct{}{}
		case <-c.shutdown:
			return
		}
	}
}

// writeSequencer provides ordered sequence numbers for write operations.
func (c *client) writeSequencer() {
	var orderCounter uint64 = 1
	for {
		select {
		case replyCh := <-c.writeOrderRequests:
			replyCh <- orderCounter
			orderCounter++
		case <-c.shutdown:
			return
		}
	}
}

// connectionManager continuously sends connection requests until established.
func (c *client) connectionManager() {
	// Send first connection request immediately.
	c.send(NewConnect(c.initialSeqN))

	ticker := time.NewTicker(time.Duration(c.params.EpochMillis/4) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.send(NewConnect(c.initialSeqN))
		case <-c.connected:
			return
		case <-c.shutdown:
			return
		}
	}
}

////////////////////////// Message Handling //////////////////////////

// handleNetEvent processes incoming network messages and maintains connection state.
func (c *client) handleNetEvent(msg *Message) {
	c.epochsSinceLastMessage = 0
	// Don't reset connection lost state if we're closing - once lost during close, stay lost
	if c.isConnectionLost && !c.isClosing {
		c.isConnectionLost = false
		c.lostSignalSent = false
	}

	switch msg.Type {
	case MsgData:
		if c.connID == 0 || msg.ConnID != c.connID {
			return // Not for us
		}
		if len(msg.Payload) < msg.Size || CalculateChecksum(msg.ConnID, msg.SeqNum, msg.Size, msg.Payload[:msg.Size]) != msg.Checksum {
			return // Corrupt
		}

		c.send(NewAck(c.connID, msg.SeqNum))

		if msg.SeqNum < c.nextRecvSeq {
			return // Duplicate
		}

		c.recvBuffer[msg.SeqNum] = msg.Payload[:msg.Size]
		c.processContiguousData()

	case MsgAck:
		if c.connID == 0 { // Connection confirmation
			if msg.SeqNum == c.initialSeqN && msg.ConnID > 0 {
				c.connID = msg.ConnID
				close(c.connected)
			}
		} else { // Data acknowledgment
			if msg.ConnID == c.connID && msg.SeqNum > 0 {
				delete(c.inflightMessages, msg.SeqNum)
				delete(c.lastSentEpoch, msg.SeqNum)
				delete(c.retryBackoff, msg.SeqNum)
				c.fillWindow()
			}
		}

	case MsgCAck:
		if c.connID > 0 && msg.ConnID == c.connID {
			for seq := range c.inflightMessages {
				if seq <= msg.SeqNum {
					delete(c.inflightMessages, seq)
					delete(c.lastSentEpoch, seq)
					delete(c.retryBackoff, seq)
				}
			}
			c.fillWindow()
		}
	}
	c.flushReadDelivery()
}

// handleWriteRequest processes application write requests with proper ordering.
func (c *client) handleWriteRequest(req *writeRequest) {
	if req.order == c.nextWriteOrder {
		c.writeQueue = append(c.writeQueue, NewData(c.connID, 0, len(req.payload), req.payload, 0))
		c.nextWriteOrder++
		// Check for subsequent ordered writes
		for {
			payload, found := c.writeOrderQueue[c.nextWriteOrder]
			if !found {
				break
			}
			c.writeQueue = append(c.writeQueue, NewData(c.connID, 0, len(payload), payload, 0))
			delete(c.writeOrderQueue, c.nextWriteOrder)
			c.nextWriteOrder++
		}
	} else if req.order > c.nextWriteOrder {
		c.writeOrderQueue[req.order] = req.payload
	}
	c.fillWindow()
}

// handleEpoch manages retransmissions, heartbeats, and connection loss detection.
func (c *client) handleEpoch() {
	c.currentEpoch++
	c.epochsSinceLastMessage++

	if c.connID == 0 {
		return // Still trying to connect
	}

	// Loss detection
	if c.epochsSinceLastMessage >= c.params.EpochLimit {
		c.isConnectionLost = true
		if !c.lostSignalSent {
			if _, present := c.recvBuffer[c.nextRecvSeq]; !present {
				c.deliveryQueue = append(c.deliveryQueue, &ReadResult{Error: errors.New("connection lost")})
				c.lostSignalSent = true
				c.flushReadDelivery()
			}
		}
	}

	// Retransmission
	resentSomething := false
	for seq, msg := range c.inflightMessages {
		gap, ok := c.retryBackoff[seq]
		if !ok {
			gap = 1
		}
		if c.currentEpoch-c.lastSentEpoch[seq] >= gap {
			c.send(msg)
			c.lastSentEpoch[seq] = c.currentEpoch
			newGap := gap * 2
			if c.params.MaxBackOffInterval > 0 && newGap > c.params.MaxBackOffInterval {
				newGap = c.params.MaxBackOffInterval
			}
			c.retryBackoff[seq] = newGap
			resentSomething = true
		}
	}

	// Heartbeat
	if !resentSomething && len(c.inflightMessages) == 0 && !c.isConnectionLost {
		c.send(NewAck(c.connID, 0))
	}

	c.fillWindow()
}

////////////////////////// Flow Control and Transmission //////////////////////////

// fillWindow sends queued messages up to the window size limit.
func (c *client) fillWindow() {
	capacity := c.params.WindowSize
	if c.params.MaxUnackedMessages > 0 && capacity > c.params.MaxUnackedMessages {
		capacity = c.params.MaxUnackedMessages
	}

	for len(c.inflightMessages) < capacity && len(c.writeQueue) > 0 {
		msg := c.writeQueue[0]
		c.writeQueue = c.writeQueue[1:]

		msg.ConnID = c.connID
		msg.SeqNum = c.nextSendSeq
		msg.Checksum = CalculateChecksum(msg.ConnID, msg.SeqNum, msg.Size, msg.Payload)
		c.nextSendSeq++

		c.send(msg)
		c.inflightMessages[msg.SeqNum] = msg
		c.lastSentEpoch[msg.SeqNum] = c.currentEpoch
		c.retryBackoff[msg.SeqNum] = 1
	}
}

////////////////////////// Data Processing //////////////////////////

// processContiguousData moves received data to the delivery queue in order.
func (c *client) processContiguousData() {
	progressed := false
	for {
		payload, found := c.recvBuffer[c.nextRecvSeq]
		if !found {
			break
		}
		c.deliveryQueue = append(c.deliveryQueue, &ReadResult{Payload: payload})
		delete(c.recvBuffer, c.nextRecvSeq)
		c.lastContiguousSeq = c.nextRecvSeq
		c.nextRecvSeq++
		progressed = true
	}
	if progressed {
		c.send(NewCAck(c.connID, c.lastContiguousSeq))
	}
}

// flushReadDelivery delivers queued results to waiting read operations.
func (c *client) flushReadDelivery() {
	for len(c.deliveryQueue) > 0 && len(c.pendingReads) > 0 {
		result := c.deliveryQueue[0]
		c.deliveryQueue = c.deliveryQueue[1:]
		replyCh := c.pendingReads[0]
		c.pendingReads = c.pendingReads[1:]
		replyCh <- result
	}
}

////////////////////////// Connection Management //////////////////////////

// tryFinishClose checks if the client can be safely closed.
func (c *client) tryFinishClose(reply chan error) bool {
	if c.isConnectionLost {
		if !c.lostSignalSent {
			c.deliveryQueue = append(c.deliveryQueue, &ReadResult{Error: errors.New("connection lost")})
			c.lostSignalSent = true
		}
		c.flushReadDelivery()
		if reply != nil {
			reply <- errors.New("connection lost")
		}
		return true
	}

	if len(c.inflightMessages) == 0 && len(c.writeQueue) == 0 && len(c.writeOrderQueue) == 0 {
		if !c.closedSignalSent {
			c.deliveryQueue = append(c.deliveryQueue, &ReadResult{Error: errors.New("connection closed")})
			c.closedSignalSent = true
		}
		c.flushReadDelivery()
		if reply != nil {
			reply <- nil
		}
		return true
	}
	return false
}

////////////////////////// Helper Methods //////////////////////////

// send marshals a message to JSON and transmits it over UDP.
func (c *client) send(msg *Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	c.udpConn.Write(data)
}

//////////////////////////// Public API methods ////////////////////////////

// ConnID returns the connection ID assigned by the server.
func (c *client) ConnID() int {
	if c.connID == 0 {
		<-c.connected // Block until connected
	}
	return c.connID
}

// Read blocks until data is available or an error occurs.
func (c *client) Read() ([]byte, error) {
	replyCh := make(chan *ReadResult, 1)
	c.readRequests <- &readRequest{reply: replyCh}
	result := <-replyCh
	return result.Payload, result.Error
}

// Write queues a payload for transmission to the server.
func (c *client) Write(payload []byte) error {
	if c.isClosing || c.isConnectionLost {
		return errors.New("connection is closing or lost")
	}
	orderCh := make(chan uint64, 1)
	c.writeOrderRequests <- orderCh
	order := <-orderCh
	c.writeRequests <- &writeRequest{order: order, payload: payload}
	return nil
}

// Close gracefully terminates the client connection.
func (c *client) Close() error {
	replyCh := make(chan error, 1)
	c.closeRequest <- &closeRequest{reply: replyCh}
	return <-replyCh
}