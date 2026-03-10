package tests

import (
	"encoding/gob"
	"fmt"
	"testing"
	"time"

	"github.com/suryatejb/actor"
	"github.com/suryatejb/staff"
)

const (
	remoteTellBasicMessage = "this is a message"
	// Give remoteTell 2 round trips to succeed.
	remoteTellDeadline = time.Duration(4*remoteServerLatencyMs) * time.Millisecond
	// Message count for non-basic test.
	remoteTellCount = 5
)

// === Actors used in tests

// Actor that sends messages 1, 2, ..., Count in order to Target
// after receiving a senderActorCmd.
type sendActor struct {
	context *actor.ActorContext
}

func newSendActor(context *actor.ActorContext) actor.Actor {
	return &sendActor{context}
}

type SendActorCmd struct {
	Target *actor.ActorRef
	Count  int
}

func (actor *sendActor) OnMessage(message any) error {
	m := message.(SendActorCmd)
	for i := 1; i <= m.Count; i++ {
		actor.context.Tell(m.Target, i)
	}
	return nil
}

// Actor that receives messages 1, 2, ..., count and checks that they
// are delivered in order. On success/failure, a message is sent to
// reportRef: true if success, an informative string if failure.
type receiveActor struct {
	context    *actor.ActorContext
	next       int
	count      int
	checkOrder bool
	reportRef  *actor.ActorRef
}

func newReceiveActor(context *actor.ActorContext) actor.Actor {
	return &receiveActor{context: context, next: 1}
}

type ReceiveActorInit struct {
	Count      int
	CheckOrder bool
	ReportRef  *actor.ActorRef
}

func (actor *receiveActor) OnMessage(message any) error {
	switch m := message.(type) {
	case ReceiveActorInit:
		actor.count = m.Count
		actor.checkOrder = m.CheckOrder
		actor.reportRef = m.ReportRef
	case int:
		if actor.checkOrder && m != actor.next {
			actor.context.Tell(
				actor.reportRef,
				fmt.Sprintf("Out-of-order: expected %d, got %d", actor.next, m),
			)
			return nil
		}
		if actor.next == actor.count {
			actor.context.Tell(actor.reportRef, true)
		}
		actor.next++
	default:
		actor.context.Tell(
			actor.reportRef,
			fmt.Sprintf("Unexpected message type: %T, %#v", m, m),
		)
	}
	return nil
}

func init() {
	gob.Register(SendActorCmd{})
	gob.Register(ReceiveActorInit{})
}

// === RemoteTell test utils

func setupTestRemoteTell(t *testing.T) []*actor.ActorSystem {
	staff.SetArtiLatencyMs(remoteServerLatencyMs)
	systems := make([]*actor.ActorSystem, 2)
	for i := 0; i < len(systems); i++ {
		port := newPort()
		system, err := actor.NewActorSystem(port)
		if err != nil {
			t.Fatalf("Error in NewActorSystem: %s", err)
		}
		systems[i] = system

		// Register an error handler that fails the test.
		iCopy := i
		errorHandler := func(err error) {
			t.Errorf("ActorSystem %d reported error: %s", iCopy, err)
		}
		system.OnError(errorHandler)
	}
	return systems
}

func teardownTestRemoteTell(systems []*actor.ActorSystem) {
	staff.SetArtiLatencyMs(0)
	// Remote error handlers so we don't get spurious Close-related errors.
	for _, system := range systems {
		system.OnError(nil)
	}
	for _, system := range systems {
		system.Close()
	}
}

// === RemoteTell tests

func TestRemoteTellBasic(t *testing.T) {
	fmt.Printf("=== %s: %s\n", t.Name(), "remoteTell successfully sends & receives a remote actor message")

	systems := setupTestRemoteTell(t)
	defer teardownTestRemoteTell(systems)

	// For simplicity, instead of sending between real actors, send
	// externally using Tell and receive externally using NewChannelRef.
	receiveRef, receiveCh := systems[1].NewChannelRef()

	t.Log("Sending remote actor message from one ActorSystem to another")
	systems[0].Tell(receiveRef, remoteTellBasicMessage)

	timeoutCh := time.After(remoteTellDeadline)
	select {
	case received := <-receiveCh:
		if received != remoteTellBasicMessage {
			t.Fatalf("Sent message %#v, but received %#v", remoteTellBasicMessage, received)
		}
	case <-timeoutCh:
		t.Fatalf("Did not receive message within %s", remoteTellDeadline)
	}
}

func runTestRemoteTellOrder(t *testing.T, checkOrder bool, deadline time.Duration, desc string) {
	fmt.Printf("=== %s: %s\n", t.Name(), desc)

	systems := setupTestRemoteTell(t)
	defer teardownTestRemoteTell(systems)

	t.Log("Starting receiver actor")
	reportRef, reportCh := systems[1].NewChannelRef()
	receiverRef := systems[1].StartActor(newReceiveActor)
	systems[1].Tell(receiverRef, ReceiveActorInit{
		Count:      remoteTellCount,
		CheckOrder: checkOrder,
		ReportRef:  reportRef,
	})

	t.Logf("Sending %d actor messages from sender actor to *remote* receiver actor", remoteTellCount)
	senderRef := systems[0].StartActor(newSendActor)
	systems[0].Tell(senderRef, SendActorCmd{
		Target: receiverRef,
		Count:  remoteTellCount,
	})

	timeoutCh := time.After(deadline)
	select {
	case report := <-reportCh:
		errSt, ok := report.(string)
		if ok {
			t.Fatal(errSt)
		}
	case <-timeoutCh:
		t.Fatalf("Did not receive all messages within %s", deadline)
	}
}

func TestRemoteTellOrder(t *testing.T) {
	// For this test, we allow using the full remoteTellDeadline
	// for each message sequentially.
	runTestRemoteTellOrder(t, true, time.Duration(remoteTellCount)*remoteTellDeadline, "remoteTell delivers messages in order")
}

func TestRemoteTellNoBlock(t *testing.T) {
	// Force all messages to arrive within one remoteTellDeadline,
	// since they shouldn't block each other (see remoteTell's doc comment).
	runTestRemoteTellOrder(t, false, remoteTellDeadline, "remoteTell does not wait for a reply from the remote system before returning")
}
