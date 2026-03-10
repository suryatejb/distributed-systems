// Key-value store client-server tests with one query actor on one server.

package tests

import (
	"fmt"
	"testing"
	"time"
)

// === Test runners

func setupOneActor(t *testing.T, clientCount int) ([]clientWr, serverWr) {
	port := newPort()
	server := newServer(t, port, 1, []string{})

	address := fmt.Sprintf("localhost:%d", port+1)
	clients := make([]clientWr, clientCount)
	for i := 0; i < clientCount; i++ {
		clients[i] = newClient(address, fmt.Sprintf("client %d", i))
	}

	return clients, server
}

func teardownOneActor(clients []clientWr, server serverWr) {
	for _, client := range clients {
		client.c.Close()
	}
	server.s.Close()
}

// Type for a one-actor trace function.
type oneActorTraceFunc = func(t *testing.T, clients []clientWr)

// Tests the given trace with one actor and clientCount clients.
func runTestOneActor(t *testing.T, timeoutMs int, clientCount int, trace oneActorTraceFunc, queryCount int, desc string) {
	fmt.Printf("=== %s: %s\n", t.Name(), desc)

	clients, server := setupOneActor(t, clientCount)
	defer teardownOneActor(clients, server)

	doneCh := make(chan bool, 0)
	timeoutCh := time.After(time.Duration(timeoutMs) * time.Millisecond)

	go func() {
		trace(t, clients)
		doneCh <- true
	}()

	select {
	case <-doneCh:
	case <-timeoutCh:
		t.Fatalf("Test timed out after %.2f secs", float64(timeoutMs)/1000.0)
	}

	stats := server.system.Stats()
	if stats.MessagesSentExternal < queryCount {
		t.Fatalf("Not using ActorSystem? Only %d external Tell calls for %d queries.", stats.MessagesSentExternal, queryCount)
	}
	if stats.ChannelRefsUsed < queryCount {
		t.Fatalf("Not using ActorSystem? Only %d ChannelRefs used for %d queries.", stats.ChannelRefsUsed, queryCount)
	}
}

// One actor, one client tests

func TestOneActorGet(t *testing.T) {
	trace := func(t *testing.T, clients []clientWr) {
		client := clients[0]
		get(t, true, client, "foo", "", false)
	}
	runTestOneActor(t, 5000, 1, trace, 1, "Get from empty store")
}

func TestOneActorPut1(t *testing.T) {
	trace := func(t *testing.T, clients []clientWr) {
		client := clients[0]
		put(t, true, client, "topping", "cheese")
		get(t, true, client, "topping", "cheese", true)
	}
	runTestOneActor(t, 5000, 1, trace, 2, "Put then get")
}

func TestOneActorPut2(t *testing.T) {
	trace := func(t *testing.T, clients []clientWr) {
		client := clients[0]
		for i := 0; i < 10; i++ {
			put(t, true, client, "theKey", fmt.Sprintf("value%d", i))
			get(t, true, client, "theKey", fmt.Sprintf("value%d", i), true)
		}
	}
	runTestOneActor(t, 5000, 1, trace, 20, "Put that overwrites prior put")
}

func TestOneActorList1(t *testing.T) {
	trace := func(t *testing.T, clients []clientWr) {
		client := clients[0]
		put(t, true, client, "topping", "cheese")
		list(t, true, client, "", map[string]string{"topping": "cheese"})
		list(t, true, client, "filling/", map[string]string{})
	}
	runTestOneActor(t, 5000, 1, trace, 3, "Put then list")
}

func TestOneActorList2(t *testing.T) {
	trace := func(t *testing.T, clients []clientWr) {
		client := clients[0]
		put(t, true, client, "cat/size", "medium")
		put(t, true, client, "cat/color", "varied")
		put(t, true, client, "cat/life/wild", "7 years")
		put(t, true, client, "cat/life/pet", "15 years")

		put(t, true, client, "frog/size", "small")
		put(t, true, client, "frog/color", "green")
		put(t, true, client, "frog/life/wild", "5 years")
		put(t, true, client, "frog/life/pet", "10 years")

		list(t, true, client, "cat/", map[string]string{
			"cat/size":      "medium",
			"cat/color":     "varied",
			"cat/life/wild": "7 years",
			"cat/life/pet":  "15 years",
		})
		list(t, true, client, "frog/", map[string]string{
			"frog/size":      "small",
			"frog/color":     "green",
			"frog/life/wild": "5 years",
			"frog/life/pet":  "10 years",
		})
		list(t, true, client, "dog/", map[string]string{})
	}
	runTestOneActor(t, 5000, 1, trace, 11, "Multiple puts, then multiple lists")
}

func TestOneActorTrace(t *testing.T) {
	trace := func(t *testing.T, clients []clientWr) {
		client := clients[0]
		get(t, true, client, "test/a", "", false)
		put(t, true, client, "test/a", "foo")
		put(t, true, client, "test/b", "bar")
		get(t, true, client, "test/a", "foo", true)
		list(t, true, client, "test/", map[string]string{
			"test/a": "foo",
			"test/b": "bar",
		})
		list(t, true, client, "", map[string]string{
			"test/a": "foo",
			"test/b": "bar",
		})
		list(t, true, client, "other/", map[string]string{})

		put(t, true, client, "test/a", "another")
		list(t, true, client, "", map[string]string{
			"test/a": "another",
			"test/b": "bar",
		})
		get(t, true, client, "test/a", "another", true)
	}
	runTestOneActor(t, 5000, 1, trace, 7, "Series of get/put/list ops")
}

// === One actor, multi-client tests

func TestOneActorMultiClient1(t *testing.T) {
	trace := func(t *testing.T, clients []clientWr) {
		put(t, true, clients[0], "topping", "cheese")
		get(t, true, clients[1], "topping", "cheese", true)
	}
	runTestOneActor(t, 5000, 2, trace, 2, "Put from one client, get from another")
}

func TestOneActorMultiClient2(t *testing.T) {
	trace := func(t *testing.T, clients []clientWr) {
		t.Logf("Putting values from %d clients concurrently (on same actor)", len(clients))
		done := make(chan bool, len(clients))
		for i := 0; i < len(clients); i++ {
			// Do in goroutines so we are concurrent.
			// Note that these touch distinct keys (easier).
			go func(iCopy int) {
				put(t, true, clients[iCopy], fmt.Sprintf("key%d", iCopy), fmt.Sprintf("value%d", iCopy))
				done <- true
			}(i)
		}

		t.Log("Waiting for Puts to complete")
		for i := 0; i < len(clients); i++ {
			<-done
		}

		t.Log("Checking with concurrent Gets from all clients")
		done = make(chan bool, len(clients))
		for i := 0; i < len(clients); i++ {
			go func(iCopy int) {
				for j := 0; j < len(clients); j++ {
					get(t, true, clients[iCopy], fmt.Sprintf("key%d", j), fmt.Sprintf("value%d", j), true)
				}
				done <- true
			}(i)
		}

		t.Log("Waiting for Gets to complete")
		for i := 0; i < len(clients); i++ {
			<-done
		}
	}
	runTestOneActor(t, 5000, 4, trace, 4*5, "Put from 4 clients concurrently, get from all")
}
