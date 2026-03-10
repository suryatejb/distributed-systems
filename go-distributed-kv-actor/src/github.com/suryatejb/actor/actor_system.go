// MODIFICATIONS IGNORED ON GRADESCOPE!

// Package actor provides ActorSystem, a basic Go actor system inspired by
// Akka (https://doc.akka.io/docs/akka/current/index.html).
package actor

import (
	"bufio"
	"encoding/gob"
	"fmt"
	"math"
	"net"
	"net/rpc"
	"sync"
	"sync/atomic"
	"time"

	"github.com/suryatejb/staff"
)

// Immutable, so access doesn't need a mutex.
type actorRefInfo struct {
	// Non-nil if a local actor.
	// Message type: []byte
	mailbox *Mailbox
	// Non-nil if a local actor.
	// We have a separate context per actor so we can track per-actor
	// stats in it.
	context *ActorContext
	// Non-nil if a response channel (from NewChannelRef).
	respCh chan any
}

// Stores remote messages in ActorSystem.remotes' mailboxes.
type remoteMessage struct {
	mars []byte
	ref  *ActorRef
}

// An actor system that manages a group of actors.
//
// ActorSystem is responsible for starting and running actors,
// delivering messages to actors' mailboxes, and sending and receiving messages to/from actors
// in remote ActorSystems. It also has functions to communicate with actors from outside
// the actor system (Tell, TellAfter, NewChannelRef). See the example/ folder
// for an example usage.
//
// Typically, you will have one ActorSystem instance per process, running
// all actors in that process.
//
// All methods on an ActorSystem are thread-safe.
type ActorSystem struct {
	address string
	ln      net.Listener
	// Locked when creating new actors and in Close().
	newActorMux *sync.Mutex
	nextCounter int
	// Maps from actor counter to its info: map[int]*actorRefInfo.
	infos           *sync.Map
	errorHandler    func(err error)
	errorHandlerMux *sync.Mutex
	// Mailboxes for sending to remote systems, keyed by address.
	// Message type: remoteMessage
	remotes    map[string]*Mailbox
	remotesMux *sync.Mutex
	closed     bool
	// Atomic int32s for Stats().
	messagesSentActor    int32
	messagesSentExternal int32
	bytesSent            int32
	remoteBytesReceived  int32
	channelRefsUsed      int32
}

// A reference to an Actor, either local or remote, that can be used to
// send that actor a message (via ActorSystem.Tell/TellAfter or
// ActorContext.Tell/TellAfter).
//
// ActorRef's are JSON serializable and make sense cross-network, i.e.,
// you may send an ActorRef to a remote actor and the remote actor can
// use it to send a message.
type ActorRef struct {
	// The actor's ActorSystem's RPC server's address.
	Address string
	// A counter is used to distinguish the actor from others within
	// the same ActorSystem.
	Counter int
}

// Return a unique ID string for ref, useful for tiebreakers.
func (ref *ActorRef) Uid() string {
	return fmt.Sprintf("%s/%d", ref.Address, ref.Counter)
}

// Create and returns a new ActorSystem.
//
// The system listens for messages from remote ActorSystems with an rpc.Server
// on the given port, which is started before returning. If there is an error
// starting the server, (nil, the error) is returned instead.
func NewActorSystem(port int) (*ActorSystem, error) {
	// In a real implementation, address would be an external IP address
	// instead of "localhost". For this assignment, it's okay because all
	// ActorSystems are run on the same machine.
	address := fmt.Sprintf("localhost:%d", port)

	system := &ActorSystem{
		address:         address,
		ln:              nil,
		newActorMux:     &sync.Mutex{},
		nextCounter:     0,
		infos:           &sync.Map{},
		errorHandler:    nil,
		errorHandlerMux: &sync.Mutex{},
		remotes:         make(map[string]*Mailbox),
		remotesMux:      &sync.Mutex{},
		closed:          false,
	}

	// Listen for remote Tell calls (as RPCs).
	server := rpc.NewServer()
	err := registerRemoteTells(system, server)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	system.ln = ln
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go system.rpcServeConnSeq(server, conn)
		}
	}()

	// Tracking for LastActorSystem().
	lastActorSystemMux.Lock()
	lastActorSystem = system
	lastActorSystemMux.Unlock()

	return system, nil
}

// rpcServeConnSeq is functionally the same as rpc.Server.ServeConn, except
// that it performs the (local) RPC calls sequentially instead of spawning
// each in a goroutine. We use this for the remoteTell RPC server to ensure
// that remote Tells received from the same external ActorSystem are processed
// in order.
func (system *ActorSystem) rpcServeConnSeq(server *rpc.Server, conn net.Conn) {
	// Modified from https://cs.opensource.google/go/go/+/refs/tags/go1.18.5:src/net/rpc/server.go;l=445
	buf := bufio.NewWriter(conn)
	codec := &gobServerCodec{
		rwc:    conn,
		dec:    gob.NewDecoder(conn),
		enc:    gob.NewEncoder(buf),
		encBuf: buf,
	}
	for {
		err := server.ServeRequest(codec)
		if err != nil {
			system.reportError(err)
			return
		}
	}

}

// Closes the ActorSystem.
//
// All actors are terminated, and the rpc.Server for receiving remote messages
// is closed. Any future messages sent to actors in this system are dropped.
func (system *ActorSystem) Close() {
	system.newActorMux.Lock()
	defer system.newActorMux.Unlock()
	system.remotesMux.Lock()
	defer system.remotesMux.Unlock()

	if system.closed {
		return
	}

	system.closed = true
	system.ln.Close()
	// Close local actor mailboxes.
	system.infos.Range(func(key, value any) bool {
		info := value.(*actorRefInfo)
		if info.mailbox != nil {
			info.mailbox.Close()
		}
		return true
	})
	// Close remote RPC clients.
	for _, mailbox := range system.remotes {
		mailbox.Close()
		// remoteSendRoutine's will Close() their client upon seeing
		// the mailbox closure.
	}
}

// Returns whether ref points to a local actor, i.e., an
// actor in this ActorSystem.
func (system *ActorSystem) IsLocal(ref *ActorRef) bool {
	return ref.Address == system.address
}

// Call this function to register an errorHandler that receives
// system-internal errors on a best-effort basis.
//
// Usually, you will log such errors for debugging purposes. For this
// assignment, we do not expect you to actually address or recover from
// the errors dynamically.
//
// There can only be one errorHandler; an existing one is overwritten.
func (system *ActorSystem) OnError(errorHandler func(err error)) {
	system.errorHandlerMux.Lock()
	system.errorHandler = errorHandler
	system.errorHandlerMux.Unlock()
}

func (system *ActorSystem) reportError(err error) {
	system.errorHandlerMux.Lock()
	if system.errorHandler != nil {
		system.errorHandler(err)
	}
	system.errorHandlerMux.Unlock()
}

// Starts a new local actor and returns a reference to it.
//
// newActor is a "constructor" for the desired actor type, as described in
// the docs for the Actor interface. Its signature is fixed to prevent you
// from passing pointers, channels, etc. to an actor that could allow you to
// coordinate with the actor other than through message passing (which is
// not allowed). Please do not circumvent these safeguards, e.g., by accessing
// mutable global variables or closure variables inside an actor or its
// constructor. To pass initial data to an actor, instead send it a message.
func (system *ActorSystem) StartActor(newActor func(context *ActorContext) Actor) *ActorRef {
	system.newActorMux.Lock()
	defer system.newActorMux.Unlock()

	if system.closed {
		// Return fake ref. Messages to it will be dropped.
		return &ActorRef{system.address, -1}
	}

	id := system.nextCounter
	system.nextCounter++
	ref := &ActorRef{system.address, id}
	mailbox := NewMailbox()
	context := newActorContext(system, ref)
	system.infos.Store(id, &actorRefInfo{mailbox: mailbox, context: context})

	actor := newActor(context)
	go system.runActor(actor, mailbox)
	return ref
}

func (system *ActorSystem) runActor(actor Actor, mailbox *Mailbox) {
	for {
		mars, ok := mailbox.Pop()
		if !ok {
			return
		}
		message, err := unmarshal(mars.([]byte))
		if err != nil {
			system.reportError(err)
			continue
		}
		err = actor.OnMessage(message)
		if err != nil {
			system.reportError(err)
			continue
		}
	}
}

// Returns a synthetic ActorRef and a channel corresponding to that ActorRef.
// The first message sent to the returned ActorRef (either from a local or
// remote actor) is delivered on the returned channel.
//
// This lets you receive
// a message from an actor despite being outside the actor system: send
// the ActorRef in a message to an actor (using Tell or TellAfter); that
// actor can then send a message to the ActorRef, causing it to appear
// on the channel.
//
// Messages sent to the ActorRef after the first are dropped.
//
// See https://doc.akka.io/docs/akka/current/typed/interaction-patterns.html#request-response-with-ask-from-outside-an-actor
// for a related concept in the Akka actor system.
func (system *ActorSystem) NewChannelRef() (*ActorRef, <-chan any) {
	system.newActorMux.Lock()
	defer system.newActorMux.Unlock()

	if system.closed {
		// Return fake ref. Messages to it will be dropped.
		return &ActorRef{system.address, -1}, make(chan any, 1)
	}

	id := system.nextCounter
	system.nextCounter++
	respCh := make(chan any, 1)
	system.infos.Store(id, &actorRefInfo{respCh: respCh})

	return &ActorRef{system.address, id}, respCh
}

// Sends a message to the actor identified by ref, which may be local or
// remote. Use this function to send a message to an actor from outside the
// actor system (i.e., from a non-actor).
//
// This function does not block; the message will be placed in the target
// actor's mailbox. As described in the handout, messages are delivered
// at-most-once with no failure
// notification. (For this project, you can ignore failures, i.e., you do
// not need to resend dropped messages.)
//
// Also as described in the handout, message must be marshallable with
// Go's encoding/gob package. In particular:
//
// - All struct types, struct fields, and nested struct fields must be exported (Capitalized).
//
// - Messages must not contain Go channels, functions, or similar non-marshallable types.
// Pointers are okay, but the contents they point to will be copied, not shared as a
// pointer.
//
// - We recommend defining message types as plain structs, not pointers to structs.
//
// - Any message type that is a struct must be registered with gob.Register. We
// recommend doing this in an init function in the same file where the target
// actor is defined (example in example/counter actor.go).
func (system *ActorSystem) Tell(ref *ActorRef, message any) {
	system.tellInternal(ref, message, false)
}

// Implements tell and additionally inputs fromActor.
//
// fromActor is true if the message comes from an actor (including
// a remote actor), false if it comes from an external Tell
// call. It is used for Stats.
func (system *ActorSystem) tellInternal(ref *ActorRef, message any, fromActor bool) {
	// Marshal here so that if it's expensive, the caller (usually an actor
	// pays for it.
	// We marshal even for local message tells, to prevent
	// sharing disallowed data (e.g. pointers or channels)
	// that could be used for non-actor-style synchronization.
	mars, err := marshal(message)
	if err != nil {
		system.reportError(err)
		return
	}
	system.tellMarshalled(ref, mars, fromActor, false)
}

// Calls Tell after duration d (non-blocking).
func (system *ActorSystem) TellAfter(ref *ActorRef, message any, d time.Duration) {
	system.tellAfterInternal(ref, message, d, false)
}

// Implements tellAfter and additionally inputs fromActor.
//
// fromActor is true if the message comes from an actor (including
// a remote actor), false if it comes from an external TellAfter
// call. It is used for Stats.
func (system *ActorSystem) tellAfterInternal(ref *ActorRef, message any, d time.Duration, fromActor bool) {
	// Marshal here so that if it's expensive, the caller pays for it.
	// We marshal even for local message tells, to prevent
	// sharing disallowed data (e.g. pointers or channels)
	// that could be used for non-actor-style synchronization.
	mars, err := marshal(message)
	if err != nil {
		system.reportError(err)
		return
	}
	// Do the actual sleep-and-tell in a goroutine to avoid blocking on actors'
	// critical paths.
	go func() {
		time.Sleep(d)
		system.tellMarshalled(ref, mars, fromActor, false)
	}()
}

// Handler for messages received from remote ActorSystems, via ./remote_tell.go.
//
// ref and mars are as in the remote ActorSystem's remoteTell call
// (in ./remote_tell.go).
func (system *ActorSystem) tellFromRemote(ref *ActorRef, mars []byte) {
	// Stats
	atomic.AddInt32(&system.remoteBytesReceived, int32(len(mars)))

	system.tellMarshalled(ref, mars, true, true)
}

// Sends a marshalled message to the given ref.
//
// fromActor is true if the message comes from an actor (including
// a remote actor), false if it comes from an external Tell or TellAfter
// call. It is used for Stats.
func (system *ActorSystem) tellMarshalled(ref *ActorRef, mars []byte, fromActor bool, fromRemote bool) {
	// Stats
	if !fromRemote {
		if fromActor {
			atomic.AddInt32(&system.messagesSentActor, 1)
		} else {
			atomic.AddInt32(&system.messagesSentExternal, 1)
		}
		atomic.AddInt32(&system.bytesSent, int32(len(mars)))
	}

	if ref.Address == system.address {
		// Local ref.
		infoAny, ok := system.infos.Load(ref.Counter)
		if !ok {
			// Invalid target - dropped.
			system.reportError(fmt.Errorf("Tell called for invalid local ActorRef, or ChannelRef used twice (id %d)", ref.Counter))
			return
		}

		info := infoAny.(*actorRefInfo)
		if info.mailbox != nil {
			// Literal actor ref.
			info.mailbox.Push(mars)
		} else {
			// ChannelRef.
			// respCh is only used once, then info is deleted.
			// Re-check presence in case someone else used first.
			_, ok = system.infos.LoadAndDelete(ref.Counter)
			if !ok {
				// Invalid target - dropped.
				system.reportError(fmt.Errorf("ChannelRef used twice (id %d)", ref.Counter))
				return
			}

			message, err := unmarshal(mars)
			if err != nil {
				system.reportError(err)
				return
			}
			atomic.AddInt32(&system.channelRefsUsed, 1)
			info.respCh <- message
		}
	} else {
		// Remote ref.
		// Put the message on a mailbox for the whole remote system,
		// starting an RPC client if needed.
		system.remotesMux.Lock()
		mailbox, ok := system.remotes[ref.Address]
		if !ok {
			if system.closed {
				// Don't start a new RPC client, just drop the message.
				system.remotesMux.Unlock()
				return
			}
			mailbox = NewMailbox()
			system.remotes[ref.Address] = mailbox
			go system.remoteSendRoutine(ref.Address, mailbox)
		}
		system.remotesMux.Unlock()

		mailbox.Push(remoteMessage{mars, ref})
	}
}

// Goroutine that sends messages from a system.remotes mailbox.
func (system *ActorSystem) remoteSendRoutine(address string, mailbox *Mailbox) {
	// For testing, we subject remoteTell's to test-configured latency.
	client, err := staff.DialWithLatency(address)
	if err != nil {
		mailbox.Close()
		system.reportError(err)
		return
	}

	for {
		messageAny, ok := mailbox.Pop()
		if !ok {
			return
		}

		message := messageAny.(remoteMessage)
		remoteTell(client, message.ref, message.mars)
	}
}

// For testing use: stores ActorSystem stats.
type Stats struct {
	// Number of messages sent (not necessarily delivered).
	MessagesSent int
	// Number of messages sent by external Tell/TellAfter calls (not by actors).
	MessagesSentExternal int
	// Number of messages sent by actors.
	MessagesSentActor int
	// Number of marshalled bytes sent in messages.
	BytesSent int
	// Number of marshalled bytes received in *remote* messages.
	RemoteBytesReceived int
	// Number of ChannelRefs that actually sent a message.
	ChannelRefsUsed int
	// Max messages/second for any actor->receiver pair, computed generously.
	MaxMessageRate float64
}

// For testing use: returns system stats.
func (system *ActorSystem) Stats() Stats {
	stats := Stats{
		MessagesSentExternal: int(atomic.LoadInt32(&system.messagesSentExternal)),
		MessagesSentActor:    int(atomic.LoadInt32(&system.messagesSentActor)),
		BytesSent:            int(atomic.LoadInt32(&system.bytesSent)),
		RemoteBytesReceived:  int(atomic.LoadInt32(&system.remoteBytesReceived)),
		ChannelRefsUsed:      int(atomic.LoadInt32(&system.channelRefsUsed)),
	}
	stats.MessagesSent = stats.MessagesSentExternal + stats.MessagesSentActor

	stats.MaxMessageRate = 0
	system.infos.Range(func(key, value any) bool {
		info := value.(*actorRefInfo)
		if info.context != nil {
			info.context.sendsMux.Lock()
			// Seconds the actor has been alive.
			secs := time.Now().Sub(info.context.startTime).Seconds()
			// Round up with a little leeway.
			secs = math.Floor(secs + 1.1)
			// For each recipient, take the max with stats.MaxMessageRate.
			for _, count := range info.context.sends {
				stats.MaxMessageRate = math.Max(stats.MaxMessageRate, float64(count)/secs)
			}
			info.context.sendsMux.Unlock()
		}
		return true
	})

	return stats
}

// For tests, we sometimes want to access the ActorSystem associated to
// a kvserver.Server.
var (
	lastActorSystem    *ActorSystem = nil
	lastActorSystemMux              = &sync.Mutex{}
)

// For testing use: returns the last created ActorSystem, also
// clearing it.
func LastActorSystem() *ActorSystem {
	lastActorSystemMux.Lock()
	ans := lastActorSystem
	lastActorSystem = nil
	lastActorSystemMux.Unlock()
	return ans
}
