// Command-line program demonstrating usage of the
// github.com/suryatejb/actor package with the simple
// counter actor defined in counter_actor.go.
//
// Usage: in the example/ folder, `go run .`

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/suryatejb/actor"
)

var port = flag.Int("port", 6000, "actor system port number")

func main() {
	flag.Parse()

	fmt.Println("Starting actor system...")
	system, err := actor.NewActorSystem(*port)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	fmt.Println("Starting an actor...")
	ref := system.StartActor(newCounterActor)

	fmt.Println("Sending some messages to the actor...")
	// Add 1
	system.Tell(ref, MAdd{1})
	// Add 2
	system.Tell(ref, MAdd{2})

	fmt.Println("Asking the actor for its value...")
	chanRef, respCh := system.NewChannelRef()
	system.Tell(ref, MGet{Sender: chanRef})
	ans := (<-respCh).(MResult)
	fmt.Println("Actor responded", ans.Count, "(should be 3)")

	system.Close()
}
