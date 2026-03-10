// Key-value store client-server tests with multiple query actors on one server.

package tests

import (
	"fmt"
	"testing"
	"time"
)

const (
	localSyncDeadline = time.Duration(500) * time.Millisecond
	maxMessageRate    = 10.0
)

func setupTestLocalSync(t *testing.T, queryActorCount int) ([]clientWr, serverWr) {
	port := newPort()
	server := newServer(t, port, queryActorCount, []string{})

	// Use a separate client per query actor.
	clients := make([]clientWr, queryActorCount)
	for i := 0; i < queryActorCount; i++ {
		address := fmt.Sprintf("localhost:%d", port+1+i)
		clients[i] = newClient(address, fmt.Sprintf("actor %d", i))
	}

	return clients, server
}

func teardownTestLocalSync(clients []clientWr, server serverWr) {
	for i := 0; i < len(clients); i++ {
		clients[i].c.Close()
	}
	server.s.Close()
}

// Runs a local sync test with the given trace, involving
// multiple actors on the same server.
func runTestLocalSync(
	t *testing.T,
	timeoutMs int,
	queryActorCount int,
	trace traceFunc,
	queryCount int,
	minSyncs int,
	desc string,
) {
	fmt.Printf("=== %s: %s\n", t.Name(), desc)

	clients, server := setupTestLocalSync(t, queryActorCount)
	defer teardownTestLocalSync(clients, server)

	doneCh := make(chan bool, 0)
	timeoutCh := time.After(time.Duration(timeoutMs) * time.Millisecond)

	go func() {
		trace(t, clients, localSyncDeadline, "")
		doneCh <- true
	}()

	select {
	case <-doneCh:
	case <-timeoutCh:
		t.Fatalf("Test timed out after %.2f secs", float64(timeoutMs)/1000.0)
	}

	stats := server.system.Stats()

	// Check that the ActorSystem was used enough.
	if stats.MessagesSentExternal < queryCount {
		t.Errorf("Not using ActorSystem? Only %d external Tell calls for %d queries.", stats.MessagesSentExternal, queryCount)
	}
	if stats.ChannelRefsUsed < queryCount {
		t.Errorf("Not using ActorSystem? Only %d ChannelRefs used for %d queries.", stats.ChannelRefsUsed, queryCount)
	}
	if stats.MessagesSentActor < minSyncs {
		t.Errorf("Not syncing through actor system? Only %d actor-actor messages used but %d syncs are needed.", stats.MessagesSentActor, minSyncs)
	}
}

// Runs a local sync *frequency* test with the given trace, involving
// multiple actors on the same server.
func runTestLocalSyncFrequency(
	t *testing.T,
	timeoutMs int,
	queryActorCount int,
	trace traceFunc,
	desc string,
) {
	fmt.Printf("=== %s: %s\n", t.Name(), desc)

	clients, server := setupTestLocalSync(t, queryActorCount)
	defer teardownTestLocalSync(clients, server)

	doneCh := make(chan bool, 0)
	timeoutCh := time.After(time.Duration(timeoutMs) * time.Millisecond)

	go func() {
		trace(t, clients, localSyncDeadline, "")
		doneCh <- true
	}()

	select {
	case <-doneCh:
	case <-timeoutCh:
		t.Fatalf("Test timed out after %.2f secs", float64(timeoutMs)/1000.0)
	}

	stats := server.system.Stats()

	// Check the frequency requirement.
	t.Logf("Max message rate: %.2f/sec", stats.MaxMessageRate)
	if stats.MaxMessageRate > maxMessageRate {
		t.Errorf("Max message rate per sender->receiver pair exceeded: %.2f > %.2f/sec", stats.MaxMessageRate, maxMessageRate)
	}
}

// Runs a local sync *size* test with the given trace, involving
// multiple actors on the same server.
func runTestLocalSyncSize(
	t *testing.T,
	timeoutMs int,
	queryActorCount int,
	trace traceFunc,
	desc string,
) {
	fmt.Printf("=== %s: %s\n", t.Name(), desc)

	clients, server := setupTestLocalSync(t, queryActorCount)
	defer teardownTestLocalSync(clients, server)

	firstRoundBytes := 0
	secondRoundBytes := 0

	doneCh := make(chan bool, 0)
	timeoutCh := time.After(time.Duration(timeoutMs) * time.Millisecond)

	go func() {
		defer func() {
			doneCh <- true
		}()

		// Run two "rounds", the second
		// time after putting 1000 more entries in the store.
		// Check that the second round's bytesSent is not much larger
		// than the first's.
		t.Log("Round 1")
		startBytes := server.system.Stats().BytesSent
		trace(t, clients, localSyncDeadline, "r1")
		firstRoundBytes = server.system.Stats().BytesSent - startBytes
		t.Log("Round 1 bytes:", firstRoundBytes)

		t.Log("Filling with 1,000 entries on client 0")
		fillerAns := make(map[string]string)
		for i := 0; i < 1000; i++ {
			key := fmt.Sprintf("filler%d", i)
			value := fmt.Sprintf("%d marshmallows", i)
			put(t, false, clients[0], key, value)
			fillerAns[key] = value
		}
		waitForSync(t, localSyncDeadline)
		t.Log("Checking filler entries on client 1")
		list(t, false, clients[1], "filler", fillerAns)

		t.Log("Round 2")
		startBytes = server.system.Stats().BytesSent
		trace(t, clients, localSyncDeadline, "r2")
		secondRoundBytes = server.system.Stats().BytesSent - startBytes
		t.Log("Round 2 bytes:", secondRoundBytes)
	}()

	select {
	case <-doneCh:
	case <-timeoutCh:
		t.Fatalf("Test timed out after %.2f secs", float64(timeoutMs)/1000.0)
	}

	stats := server.system.Stats()

	// Check the frequency requirement.
	t.Logf("Max message rate: %.2f/sec", stats.MaxMessageRate)
	if stats.MaxMessageRate > maxMessageRate {
		t.Errorf("Max message rate per sender->receiver pair exceeded: %.2f > %.2f/sec", stats.MaxMessageRate, maxMessageRate)
	}

	// Check the size requirement.
	if float64(secondRoundBytes) > 2*float64(firstRoundBytes) {
		t.Errorf("Size increase limit exceeded: %d >> %d", secondRoundBytes, firstRoundBytes)
	}
}

// Like runTestLocalSync, but with multiple clients per actor.
// We also skip stats checks.
func runTestLocalSyncStress(
	t *testing.T,
	timeoutMs int,
	queryActorCount int,
	clientsPerActor int,
	trace traceFunc,
	desc string,
) {
	fmt.Printf("=== %s: %s\n", t.Name(), desc)

	// Need to do our own setup to get multiple clients per actor.
	port := newPort()
	server := newServer(t, port, queryActorCount, []string{})
	defer server.s.Close()

	clients := make([]clientWr, queryActorCount*clientsPerActor)
	c := 0
	for i := 0; i < queryActorCount; i++ {
		address := fmt.Sprintf("localhost:%d", port+1+i)
		for j := 0; j < clientsPerActor; j++ {
			clients[c] = newClient(address, fmt.Sprintf("actor %d, client %d", i, j))
			defer clients[4*i+j].c.Close()
			c++
		}
	}

	doneCh := make(chan bool, 0)
	timeoutCh := time.After(time.Duration(timeoutMs) * time.Millisecond)

	go func() {
		trace(t, clients, localSyncDeadline, "")
		doneCh <- true
	}()

	select {
	case <-doneCh:
	case <-timeoutCh:
		t.Fatalf("Test timed out after %.2f secs", float64(timeoutMs)/1000.0)
	}
}

// === Local sync

func TestLocalSyncBasic1(t *testing.T) {
	runTestLocalSync(t, 5000, 2, tracePutGet, 3, 1, "2 actors: Put on one actor, Get on all")
}

func TestLocalSyncBasic2(t *testing.T) {
	runTestLocalSync(t, 5000, 2, tracePutAllGet, 6, 2, "2 actors: Put on all actors concurrently (distinct keys), Get on all")
}

func TestLocalSyncBasic3(t *testing.T) {
	runTestLocalSync(t, 5000, 4, tracePutGet, 5, 3, "4 actors: Put on one actor, Get on all")
}

func TestLocalSyncBasic4(t *testing.T) {
	runTestLocalSync(t, 5000, 4, tracePutAllGet, 20, 12, "4 actors: Put on all actors concurrently (distinct keys), Get on all")
}

// === Local sync, w/ frequency limit

func TestLocalSyncFrequency1(t *testing.T) {
	runTestLocalSyncFrequency(t, 15000, 2, traceFrequentPutGet, "2 actors: Put on one actor, Get on all w/ frequency limit")
}

func TestLocalSyncFrequency2(t *testing.T) {
	runTestLocalSyncFrequency(t, 15000, 2, traceFrequentPutAllGet, "2 actors: Put on all actors concurrently (distinct keys), Get on all w/ frequency limit")
}

func TestLocalSyncFrequency3(t *testing.T) {
	runTestLocalSyncFrequency(t, 15000, 4, traceFrequentPutGet, "4 actors: Put on one actor, Get on all w/ frequency limit")
}

func TestLocalSyncFrequency4(t *testing.T) {
	runTestLocalSyncFrequency(t, 15000, 4, traceFrequentPutAllGet, "4 actors: Put on all actors concurrently (distinct keys), Get on all w/ frequency limit")
}

// === Local sync, w/ size increase limit

func TestLocalSyncSize1(t *testing.T) {
	runTestLocalSyncSize(t, 15000, 2, tracePutGet, "2 actors: Put on one actor, Get on all w/ size increase limit")
}

func TestLocalSyncSize2(t *testing.T) {
	runTestLocalSyncSize(t, 15000, 2, tracePutAllGet, "2 actors: Put on two actors concurrently (distinct keys), Get on all w/ size increase limit")
}

func TestLocalSyncSize3(t *testing.T) {
	runTestLocalSyncSize(t, 15000, 4, tracePutGet, "4 actors: Put on one actor, Get on all w/ size increase limit")
}

func TestLocalSyncSize4(t *testing.T) {
	runTestLocalSyncSize(t, 15000, 4, tracePutAllGet, "4 actors: Put on all actors concurrently (distinct keys), Get on all w/ size increase limit")
}

// === Local sync LWW

func TestLocalSyncLWW1(t *testing.T) {
	runTestLocalSync(t, 5000, 4, traceLWW1, 6, 6, "4 actors: Put on two actors with small offset (<< sync interval), verify LWW")
}

func TestLocalSyncLWW2(t *testing.T) {
	runTestLocalSync(t, 6000, 4, traceLWW2, 7, 9, "4 actors: Put on two actors concurrently, then causally overwrite on one")
}

func TestLocalSyncLWW3(t *testing.T) {
	runTestLocalSync(t, 6000, 4, traceLWW3, 7, 9, "4 actors: Put on two actors concurrently, then causally overwrite on a third")
}

func TestLocalSyncLWW4(t *testing.T) {
	// Only 6 syncs required b/c the two client 0 writes may be coalesced.
	runTestLocalSync(t, 5000, 4, traceLWW4, 7, 6, "4 actors: Check that a re-Put of the *same* value gets a new timestamp")
}

// === Local sync stress tests

func TestLocalSyncStress1(t *testing.T) {
	runTestLocalSyncStress(t, 20000, 2, 4, traceStress, "2 actors x 4 clients: stress test")
}

func TestLocalSyncStress2(t *testing.T) {
	runTestLocalSyncStress(t, 20000, 4, 4, traceStress, "4 actors x 4 clients: stress test")
}
