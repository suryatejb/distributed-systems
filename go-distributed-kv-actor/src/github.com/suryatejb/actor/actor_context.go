// MODIFICATIONS IGNORED ON GRADESCOPE!

package actor

import (
	"sync"
	"time"
)

// Context given to an Actor instance.
//
// It exposes a subset of ActorSystem functionality to an actor
// - in particular, function Tell and the Self ActorRef.
type ActorContext struct {
	// The ActorRef of this actor.
	Self *ActorRef

	system *ActorSystem
	// Per-actor stats:
	// Count of sends from this actor to each ActorRef.
	// ActorRef is a non-pointer so map keys are compared by-value.
	sends map[ActorRef]int
	// Mutex for sends, needed because we access stats from a different
	// goroutine. (Note writes always come from the same goroutine.)
	sendsMux  *sync.Mutex
	startTime time.Time
}

func newActorContext(system *ActorSystem, self *ActorRef) *ActorContext {
	return &ActorContext{
		self,
		system,
		make(map[ActorRef]int),
		&sync.Mutex{},
		time.Now(),
	}
}

// Returns whether ref points to a local actor, i.e., an
// actor in the same ActorSystem as the actor who received
// this context.
func (context *ActorContext) IsLocal(ref *ActorRef) bool {
	return context.system.IsLocal(ref)
}

// Sends a message to the actor identified by ref, which may be local or
// remote.
//
// Messages are delivered in-order per sender-receiver pair, i.e.,
// all messages you send to the same ref will be delivered in the order you
// sent them (if at all).
//
// See ActorSystem.Tell for more info.
func (context *ActorContext) Tell(ref *ActorRef, message any) {
	// Record the send for stats.
	context.sendsMux.Lock()
	context.sends[*ref] = context.sends[*ref] + 1
	context.sendsMux.Unlock()

	context.system.tellInternal(ref, message, true)
}

// Calls Tell after duration d (non-blocking).
//
// This is useful for emulating time.Ticker: send yourself a message
// with TellAfter, then when processing that message, send it again with
// TellAfter, etc.
func (context *ActorContext) TellAfter(ref *ActorRef, message any, d time.Duration) {
	context.system.tellAfterInternal(ref, message, d, true)

	// Record the send for stats.
	context.sendsMux.Lock()
	context.sends[*ref] = context.sends[*ref] + 1
	context.sendsMux.Unlock()
}
