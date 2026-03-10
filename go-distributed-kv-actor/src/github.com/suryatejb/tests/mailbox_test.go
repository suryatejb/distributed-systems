// Mailbox tests

package tests

import (
	"fmt"
	"testing"
	"time"

	"github.com/suryatejb/actor"
)

const (
	timeoutMs     = 100
	mailboxStress = 1000
)

func popAndCheckMsg(t *testing.T, mailbox *actor.Mailbox, expectedMsg any) bool {
	msg, ok := mailbox.Pop()
	if !ok {
		t.Log("Mailbox pop returned false.")
		return false
	} else if msg != expectedMsg {
		t.Log("Mailbox pop retrieved ", msg, ", expected ", expectedMsg)
		return false
	}
	return true
}

// === Tests

// Push and pop a message
func TestMailboxBasic1(t *testing.T) {
	fmt.Printf("=== %s: %s\n", t.Name(), "Push and pop a message")

	doneCh := make(chan bool, 0)
	timeoutCh := time.After(time.Duration(timeoutMs) * time.Millisecond)

	go func() {
		mailbox := actor.NewMailbox()
		testMsg := "test-msg"
		mailbox.Push(testMsg)
		success := popAndCheckMsg(t, mailbox, testMsg)
		mailbox.Close()
		doneCh <- success
	}()

	select {
	case success := <-doneCh:
		if !success {
			t.Fatalf("Test failed")
		}
	case <-timeoutCh:
		t.Fatalf("Test timed out after %.2f secs", float64(timeoutMs)/1000.0)
	}
}

// Multiple push/pop in series
func TestMailboxBasic2(t *testing.T) {
	fmt.Printf("=== %s: %s\n", t.Name(), "Multiple push/pop in series")

	doneCh := make(chan bool, 0)
	timeoutCh := time.After(time.Duration(timeoutMs) * time.Millisecond)

	go func() {
		mailbox := actor.NewMailbox()

		head := 0
		tail := 0
		for i := 1; i < 100; i++ {
			for j := 0; j < i; j++ {
				head++
				mailbox.Push(head)
			}
			for j := 0; j < i; j++ {
				tail++
				success := popAndCheckMsg(t, mailbox, tail)
				if !success {
					doneCh <- false
					return
				}
			}
		}
		mailbox.Close()
		doneCh <- true
	}()

	select {
	case success := <-doneCh:
		if !success {
			t.Fatalf("Test failed")
		}
	case <-timeoutCh:
		t.Fatalf("Test timed out after %.2f secs", float64(timeoutMs)/1000.0)
	}
}

// Multiple concurrent Push/Pop
func TestMailboxBasic3(t *testing.T) {
	fmt.Printf("=== %s: %s\n", t.Name(), "Multiple push/pop interleaved")

	doneCh := make(chan bool)
	timeoutCh := time.After(time.Duration(timeoutMs) * time.Millisecond)

	mailbox := actor.NewMailbox()
	loadSize := 100

	pushMessages := func() {
		for i := 0; i < loadSize; i++ {
			mailbox.Push(i)
		}
	}

	go pushMessages()
	go pushMessages()

	go func() {
		for i := 0; i < 2*loadSize; i++ {
			_, success := mailbox.Pop()
			if !success {
				doneCh <- false
				return
			}
		}

		mailbox.Close()
		doneCh <- true
	}()

	select {
	case success := <-doneCh:
		if !success {
			t.Fatalf("Test failed")
		}
	case <-timeoutCh:
		t.Fatalf("Test timed out after %.2f secs", float64(timeoutMs)/1000.0)
	}
}

// Push shouldn't block
func TestMailboxPushBlock(t *testing.T) {
	fmt.Printf("=== %s: %s\n", t.Name(), "Push shouldn't block")

	doneCh := make(chan bool, 0)
	timeoutCh := time.After(time.Duration(timeoutMs) * time.Millisecond)

	go func() {
		mailbox := actor.NewMailbox()

		for i := 0; i < 10000; i++ {
			mailbox.Push(i)
			success := popAndCheckMsg(t, mailbox, i)
			if !success {
				doneCh <- false
				return
			}
		}
		mailbox.Close()
		doneCh <- true
	}()

	select {
	case success := <-doneCh:
		if !success {
			t.Fatalf("Test failed")
		}
	case <-timeoutCh:
		t.Fatalf("Test timed out after %.2f secs", float64(timeoutMs)/1000.0)
	}
}

// Pop should block when no message, and close unblocks pop
func TestMailboxPopBlock(t *testing.T) {
	fmt.Printf("=== %s: %s\n", t.Name(), "Pop should block when no message, and close unblocks pop")

	doneCh := make(chan bool, 0)
	timeoutCh := time.After(time.Duration(timeoutMs) * time.Millisecond)

	mailbox := actor.NewMailbox()
	go func() {
		mailbox.Pop() // should block
		doneCh <- true
	}()

	select {
	case <-doneCh:
		t.Fatalf("Pop should block when no message")
	case <-timeoutCh:
		// correctly times out
	}

	timeoutCh = time.After(time.Duration(timeoutMs) * time.Millisecond)
	go func() {
		mailbox.Close()
	}()
	select {
	case <-doneCh:
		// correctly unblocked
	case <-timeoutCh:
		t.Fatalf("Test timed out after %.2f secs", float64(timeoutMs)/1000.0)
	}
}

// Push/pop after close
func TestMailboxClosePushPop(t *testing.T) {
	fmt.Printf("=== %s: %s\n", t.Name(), "Push/pop after close")

	doneCh := make(chan bool, 0)
	timeoutCh := time.After(time.Duration(timeoutMs) * time.Millisecond)

	go func() {
		mailbox := actor.NewMailbox()

		mailbox.Push(0)
		mailbox.Close()
		mailbox.Push(1) // this is okay
		msg1, ok1 := mailbox.Pop()
		msg2, ok2 := mailbox.Pop()
		if msg1 != nil || ok1 || msg2 != nil || ok2 {
			t.Log("Mailbox pop after close does not meet specifications.")
			doneCh <- false
		} else {
			doneCh <- true
		}
	}()

	select {
	case success := <-doneCh:
		if !success {
			t.Fatalf("Test failed")
		}
	case <-timeoutCh:
		t.Fatalf("Test timed out after %.2f secs", float64(timeoutMs)/1000.0)
	}
}

// TestMailboxBasic3 with multiple indie mailboxes
func TestMailboxMultiple(t *testing.T) {
	fmt.Printf("=== %s: %s\n", t.Name(), "Multiple mailboxes")

	doneCh := make(chan bool, 0)
	timeoutCh := time.After(time.Duration(timeoutMs) * time.Millisecond)

	go func() {
		mailbox1 := actor.NewMailbox()
		mailbox2 := actor.NewMailbox()

		head := 0
		tail := 0
		for i := 1; i < 100; i++ {
			for j := 0; j < i; j++ {
				head++
				mailbox1.Push(head)
				mailbox2.Push(-head)
			}
			for j := 0; j < i; j++ {
				tail++
				success := popAndCheckMsg(t, mailbox1, tail)
				success = success && popAndCheckMsg(t, mailbox2, -tail)
				if !success {
					doneCh <- false
					return
				}
			}
		}
		mailbox1.Close()
		mailbox2.Close()
		doneCh <- true
	}()

	select {
	case success := <-doneCh:
		if !success {
			t.Fatalf("Test failed")
		}
	case <-timeoutCh:
		t.Fatalf("Test timed out after %.2f secs", float64(timeoutMs)/1000.0)
	}
}
