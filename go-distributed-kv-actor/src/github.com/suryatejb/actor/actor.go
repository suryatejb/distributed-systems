package actor

// An actor in the actor model concurrency pattern.
//
// An actor may have local state, but may not communicate with
// the rest of the program except by receiving messages in OnMessage
// and sending messages to other actors.
//
// To use an actor with an ActorSystem, define a "constructor" function
// with signature
//
//	func(context *ActorContext) Actor
//
// that returns a new instance of your actor type.
// Then call system.StartActor(constructor function).
// (Note that the constructor's return value must be literally
// actor.Actor, not a struct or pointer-to-struct implementing the
// Actor interface.)
//
// Your will probably want to store the constructor's context argument in your
// actor. You can then use context.Tell and context.TellAfter to send
// messages to other actors (or yourself in the future),
// and context.Self to refer to yourself in messages.
//
// See example/counter_actor.go for an example implementation.
type Actor interface {
	// Processes a single message from the mailbox.
	//
	// This function may only read/write the actor's local state and
	// send messages to other actors using the ActorContext.
	// In particular, it may not access shared memory, use Go channels,
	// spawn goroutines, perform I/O (except logging), or block.
	//
	// If an error occurs, you may return it. The ActorSystem will report
	// the error but not act on it. Unlike in some real actor systems,
	// returning an error will not cause the actor to die or be restarted.
	OnMessage(message any) error
}
