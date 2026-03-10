package main

import (
	"encoding/gob"
	"fmt"

	"github.com/suryatejb/actor"
)

// Example actor using the actor package.
// This implements the handout's pseudocode counter actor.

// Struct type holding a counter actor's local state.
type counterActor struct {
	context *actor.ActorContext
	count   int
}

// "Constructor" for counterActor instances.
// Must have signature `func(context *actor.ActorContext) actor.Actor`
// so that it can be passed to ActorSystem.NewActor.
// (In particular, the return type must be actor.Actor, not *counterActor,
// even though *counterActor implements actor.Actor.)
//
// context is a wrapper around the ActorSystem that the actor can use
// to send messages (with `context.Tell`). It also has context.Self,
// an actor ref for the new actor. The actor can use context.Self to send
// messages to itself (perhaps on a delay with `context.TellAfter`)
// or include it as a "Sender"/"ReplyTo" field in sent messages.
func newCounterActor(context *actor.ActorContext) actor.Actor {
	return &counterActor{
		context: context,
		count:   0,
	}
}

// Message types sent and received by the actor.

type MAdd struct {
	Value int
}

type MGet struct {
	Sender *actor.ActorRef
}

type MResult struct {
	Count int
}

func init() {
	// Register message types with encoding/gob so the ActorSystem can
	// marshal and unmarshal them.
	gob.Register(MAdd{})
	gob.Register(MGet{})
	gob.Register(MResult{})
}

// Finally, the useful part: your actor's OnMessage function, which processes
// one message. Inside OnMessage, the actor may read and write its local state
// (i.e., its struct fields) and send messages to other actors
// using actor.context.Tell / actor.context.TellAfter.
func (actor *counterActor) OnMessage(message any) error {
	// Go type switch; see https://go.dev/tour/methods/16
	switch m := message.(type) {
	case MAdd:
		actor.count += m.Value
	case MGet:
		result := MResult{actor.count}
		// Common actor pattern: reply to a message by sending a message
		// to its sender, identified by an *actor.ActorRef included in
		// the original message.
		actor.context.Tell(m.Sender, result)
	default:
		// Return an error. The ActorSystem will report this but not
		// do anything else.
		return fmt.Errorf("Unexpected counterActor message type: %T", m)
	}
	return nil
}
