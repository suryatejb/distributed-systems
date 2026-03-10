// Key-value store client-server tests with multiple servers.

package tests

import (
	"fmt"
	"testing"
	"time"

	"github.com/suryatejb/actor"
	"github.com/suryatejb/staff"
)

const (
	remoteSyncDeadline = time.Duration(2000) * time.Millisecond
	// 1-way latency between kvservers. Handout mandates functionality
	// for up to 250ms latency, but we'll be generous.
	// 200ms is a somewhat high but still reasonable value for
	// servers on different continents.
	remoteServerLatencyMs = 200
)

func setupTestRemoteSync(t *testing.T, serverCount, queryActorsPerServer int) ([]clientWr, []serverWr) {
	staff.SetArtiLatencyMs(remoteServerLatencyMs)
	servers := make([]serverWr, serverCount)
	descsSoFar := []string{}
	clients := make([]clientWr, serverCount*queryActorsPerServer)
	for i := 0; i < serverCount; i++ {
		port := newPort()
		servers[i] = newServer(t, port, queryActorsPerServer, descsSoFar)
		descsSoFar = append(descsSoFar, servers[i].desc)

		// Use a separate client per query actor.
		for j := 0; j < queryActorsPerServer; j++ {
			address := fmt.Sprintf("localhost:%d", port+1+j)
			clients[queryActorsPerServer*i+j] = newClient(address, fmt.Sprintf("server %d, actor %d", i, j))
		}
	}

	return clients, servers
}

func teardownTestRemoteSync(clients []clientWr, servers []serverWr) {
	staff.SetArtiLatencyMs(0)
	for i := 0; i < len(clients); i++ {
		clients[i].c.Close()
	}
	for i := 0; i < len(servers); i++ {
		servers[i].s.Close()
	}
}

// Call on a given server's stats *if* that server must have received
// a remote actor message (remote Tell).
func checkRemoteTellUsed(t *testing.T, stats actor.Stats) {
	if stats.RemoteBytesReceived == 0 {
		t.Fatal("Not syncing remotes through actor system? 0 bytes received through system.tellFromRemote")
	}
}

// Runs a remote sync test with the given trace, involving
// multiple servers, sometimes with multiple actors per server.
func runTestRemoteSync(
	t *testing.T,
	timeoutMs int,
	serverCount int,
	queryActorsPerServer int,
	trace traceFunc,
	// The number of client queries performed by the trace.
	queryCount int,
	// The minimum number of actor->actor messages sent.
	minSyncs int,
	desc string,
) {
	fmt.Printf("=== %s: %s\n", t.Name(), desc)

	clients, servers := setupTestRemoteSync(t, serverCount, queryActorsPerServer)
	defer teardownTestRemoteSync(clients, servers)

	t.Log("Waiting a few round trips for servers to start up")
	time.Sleep(time.Duration(4*remoteServerLatencyMs) * time.Millisecond)

	doneCh := make(chan bool, 0)
	timeoutCh := time.After(time.Duration(timeoutMs) * time.Millisecond)

	go func() {
		trace(t, clients, remoteSyncDeadline, "")
		doneCh <- true
	}()

	select {
	case <-doneCh:
	case <-timeoutCh:
		t.Fatalf("Test timed out after %.2f secs", float64(timeoutMs)/1000.0)
	}

	messagesSentExternal := 0
	channelRefsUsed := 0
	messagesSentActor := 0

	for i, server := range servers {
		stats := server.system.Stats()

		messagesSentExternal += stats.MessagesSentExternal
		channelRefsUsed += stats.ChannelRefsUsed
		messagesSentActor += stats.MessagesSentActor

		// In all of our runTestRemoteSync calls' traces, server 1 must
		// receive at least one remote message, from server 0.
		// Check that this indeed happens and the kvserver is not syncing
		// outside of the actor system (or actor/remote_tell.go calls a
		// function other than system.tellFromRemote).
		if i == 1 && stats.RemoteBytesReceived == 0 {
			t.Fatal("Not syncing remotes through actor system? 0 bytes received through system.tellFromRemote.")
		}
	}

	if messagesSentExternal < queryCount {
		t.Fatalf("Not using ActorSystem? Only %d external Tell calls for %d queries.", messagesSentExternal, queryCount)
	}
	if channelRefsUsed < queryCount {
		t.Fatalf("Not using ActorSystem? Only %d ChannelRefs used for %d queries.", channelRefsUsed, queryCount)
	}
	if messagesSentActor < minSyncs {
		t.Fatalf("Not syncing through actor system? Only %d actor-actor messages used but %d syncs are needed.", messagesSentActor, minSyncs)
	}
}

func bytesSent(servers []serverWr) int {
	ans := 0
	for _, server := range servers {
		ans += server.system.Stats().BytesSent
	}
	return ans
}

// Runs a remote sync *frequency* test with the given trace, involving
// multiple servers, sometimes with multiple actors per server.
func runTestRemoteSyncFrequency(
	t *testing.T,
	timeoutMs int,
	serverCount int,
	queryActorsPerServer int,
	trace traceFunc,
	desc string,
) {
	fmt.Printf("=== %s: %s\n", t.Name(), desc)

	clients, servers := setupTestRemoteSync(t, serverCount, queryActorsPerServer)
	defer teardownTestRemoteSync(clients, servers)

	t.Log("Waiting a few round trips for servers to start up")
	time.Sleep(time.Duration(4*remoteServerLatencyMs) * time.Millisecond)

	doneCh := make(chan bool, 0)
	timeoutCh := time.After(time.Duration(timeoutMs) * time.Millisecond)

	go func() {
		trace(t, clients, remoteSyncDeadline, "")
		doneCh <- true
	}()

	select {
	case <-doneCh:
	case <-timeoutCh:
		t.Fatalf("Test timed out after %.2f secs", float64(timeoutMs)/1000.0)
	}

	for i, server := range servers {
		stats := server.system.Stats()

		// Check the frequency requirement.
		t.Logf("(Server %d) Max message rate: %.2f/sec", i, stats.MaxMessageRate)
		if stats.MaxMessageRate > maxMessageRate {
			t.Errorf("(Server %d) Max message rate per sender->receiver pair exceeded: %.2f > %.2f/sec", i, stats.MaxMessageRate, maxMessageRate)
		}
	}
}

// Runs a remote sync *size* test with the given trace, involving
// multiple actors on the same server.
func runTestRemoteSyncSize(
	t *testing.T,
	timeoutMs int,
	serverCount int,
	queryActorsPerServer int,
	trace traceFunc,
	desc string,
) {
	fmt.Printf("=== %s: %s\n", t.Name(), desc)

	clients, servers := setupTestRemoteSync(t, serverCount, queryActorsPerServer)
	defer teardownTestRemoteSync(clients, servers)

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
		startBytes := bytesSent(servers)
		trace(t, clients, remoteSyncDeadline, "r1")
		firstRoundBytes = bytesSent(servers) - startBytes
		t.Log("Round 1 bytes:", firstRoundBytes)

		t.Log("Filling with 1,000 entries on client 0")
		fillerAns := make(map[string]string)
		for i := 0; i < 1000; i++ {
			key := fmt.Sprintf("filler%d", i)
			value := fmt.Sprintf("%d marshmallows", i)
			put(t, false, clients[0], key, value)
			fillerAns[key] = value
		}
		waitForSync(t, remoteSyncDeadline)
		t.Log("Checking filler entries on client 1")
		list(t, false, clients[1], "filler", fillerAns)

		t.Log("Round 2")
		startBytes = bytesSent(servers)
		trace(t, clients, remoteSyncDeadline, "r2")
		secondRoundBytes = bytesSent(servers) - startBytes
		t.Log("Round 2 bytes:", secondRoundBytes)
	}()

	select {
	case <-doneCh:
	case <-timeoutCh:
		t.Fatalf("Test timed out after %.2f secs", float64(timeoutMs)/1000.0)
	}

	// Check the frequency requirement.
	for i, server := range servers {
		stats := server.system.Stats()
		t.Logf("(Server %d) Max message rate: %.2f/sec", i, stats.MaxMessageRate)
		if stats.MaxMessageRate > maxMessageRate {
			t.Errorf("(Server %d) Max message rate per sender->receiver pair exceeded: %.2f > %.2f/sec", i, stats.MaxMessageRate, maxMessageRate)
		}
	}

	// Check the size requirement.
	if float64(secondRoundBytes) > 2*float64(firstRoundBytes) {
		t.Errorf("Size increase limit exceeded: %d >> %d", secondRoundBytes, firstRoundBytes)
	}
}

// Runs a remote sync test where we check that updates are not broadcast
// separately to each remote actor. Specifically, we send updates to
// remote servers with 1 and 4 actors and check that they receive
// similar amounts of traffic.
func runTestRemoteSyncNoBcast(
	t *testing.T,
	timeoutMs int,
	senderQueryActors int,
	trace traceFunc,
	desc string,
) {
	fmt.Printf("=== %s: %s\n", t.Name(), desc)

	// Need to do our own setup to get different # actors/server.
	staff.SetArtiLatencyMs(remoteServerLatencyMs)
	defer staff.SetArtiLatencyMs(0)

	actorCounts := []int{senderQueryActors, 1, 4}
	servers := make([]serverWr, len(actorCounts))
	descsSoFar := []string{}
	clients := []clientWr{}
	for i := 0; i < len(actorCounts); i++ {
		port := newPort()
		servers[i] = newServer(t, port, actorCounts[i], descsSoFar)
		defer servers[i].s.Close()
		descsSoFar = append(descsSoFar, servers[i].desc)

		// Use a separate client per query actor.
		for j := 0; j < actorCounts[i]; j++ {
			address := fmt.Sprintf("localhost:%d", port+1+j)
			client := newClient(address, fmt.Sprintf("server %d, actor %d", i, j))
			defer client.c.Close()
			clients = append(clients, client)
		}
	}

	doneCh := make(chan bool, 0)
	timeoutCh := time.After(time.Duration(timeoutMs) * time.Millisecond)

	go func() {
		trace(t, clients, remoteSyncDeadline, "")
		doneCh <- true
	}()

	select {
	case <-doneCh:
	case <-timeoutCh:
		t.Fatalf("Test timed out after %.2f secs", float64(timeoutMs)/1000.0)
	}

	// Check the no-broadcast requirement: the remote servers with 1 and
	// 4 actors should have received similar amounts of traffic.
	stats1 := servers[1].system.Stats()
	stats4 := servers[2].system.Stats()
	t.Logf("Bytes sent to 1-actor server: %d", stats1.RemoteBytesReceived)
	t.Logf("Bytes sent to 4-actor server: %d", stats4.RemoteBytesReceived)
	if stats4.RemoteBytesReceived > 2*stats1.RemoteBytesReceived {
		t.Errorf("No-broadcast requirement violated: 1-actor server received %d bytes, 4-actor server received %d bytes, expected these to be similar", stats1.RemoteBytesReceived, stats4.RemoteBytesReceived)
	}
}

// Like runTestRemoteSync, but with multiple clients per actor.
// We also skip stats checks.
func runTestRemoteSyncStress(
	t *testing.T,
	timeoutMs int,
	serverCount int,
	queryActorsPerServer int,
	clientsPerActor int,
	trace traceFunc,
	desc string,
) {
	fmt.Printf("=== %s: %s\n", t.Name(), desc)

	// Need to do our own setup to get multiple clients per actor.
	staff.SetArtiLatencyMs(remoteServerLatencyMs)
	defer staff.SetArtiLatencyMs(0)
	descsSoFar := []string{}
	clients := make([]clientWr, serverCount*queryActorsPerServer*clientsPerActor)
	c := 0
	for s := 0; s < serverCount; s++ {
		port := newPort()
		server := newServer(t, port, queryActorsPerServer, descsSoFar)
		defer server.s.Close()
		descsSoFar = append(descsSoFar, server.desc)

		for i := 0; i < queryActorsPerServer; i++ {
			address := fmt.Sprintf("localhost:%d", port+1+i)
			for j := 0; j < clientsPerActor; j++ {
				clients[c] = newClient(address, fmt.Sprintf("server %d, actor %d, client %d", s, i, j))
				defer clients[4*i+j].c.Close()
				c++
			}
		}
	}

	doneCh := make(chan bool, 0)
	timeoutCh := time.After(time.Duration(timeoutMs) * time.Millisecond)

	go func() {
		trace(t, clients, remoteSyncDeadline, "")
		doneCh <- true
	}()

	select {
	case <-doneCh:
	case <-timeoutCh:
		t.Fatalf("Test timed out after %.2f secs", float64(timeoutMs)/1000.0)
	}
}

// === Remote sync basic

func TestRemoteSyncBasic1(t *testing.T) {
	runTestRemoteSync(t, 10000, 2, 1, tracePutGet, 3, 1, "2 servers x 1 actor: Put on one actor, Get on all")
}

func TestRemoteSyncBasic2(t *testing.T) {
	runTestRemoteSync(t, 10000, 2, 1, tracePutAllGet, 6, 2, "2 servers x 1 actor: Put on all actors concurrently (distinct keys), Get on all")
}

func TestRemoteSyncBasic3(t *testing.T) {
	runTestRemoteSync(t, 10000, 4, 1, tracePutGet, 5, 3, "4 servers x 1 actor: Put on one actor, Get on all")
}

func TestRemoteSyncBasic4(t *testing.T) {
	runTestRemoteSync(t, 10000, 4, 1, tracePutAllGet, 4*5, 4*3, "4 servers x 1 actor: Put on all actors concurrently (distinct keys), Get on all")
}

// === Remote + local sync

func TestRemoteSyncLocal1(t *testing.T) {
	runTestRemoteSync(t, 10000, 2, 4, tracePutGet, 9, 7, "2 servers x 4 actors: Put on one actor, Get on all")
}

func TestRemoteSyncLocal2(t *testing.T) {
	runTestRemoteSync(t, 10000, 2, 4, tracePutAllGet, 8*9, 8*7, "2 servers x 4 actors: Put on all actors concurrently (distinct keys), Get on all")
}

func TestRemoteSyncLocal3(t *testing.T) {
	runTestRemoteSync(t, 10000, 4, 4, tracePutGet, 17, 15, "4 servers x 4 actors: Put on one actor, Get on all")
}

func TestRemoteSyncLocal4(t *testing.T) {
	runTestRemoteSync(t, 10000, 4, 4, tracePutAllGet, 16*17, 16*15, "4 servers x 4 actors: Put on all actors concurrently (distinct keys), Get on all")
}

// === Remote + local sync, w/ frequency limit

func TestRemoteSyncFrequency1(t *testing.T) {
	runTestRemoteSyncFrequency(t, 20000, 2, 4, traceFrequentPutGet, "2 servers x 4 actors: Put on one actor, Get on all w/ frequency limit")
}

func TestRemoteSyncFrequency2(t *testing.T) {
	runTestRemoteSyncFrequency(t, 20000, 2, 4, traceFrequentPutAllGet, "2 servers x 4 actors: Put on all actors concurrently (distinct keys), Get on all w/ frequency limit")
}

func TestRemoteSyncFrequency3(t *testing.T) {
	runTestRemoteSyncFrequency(t, 20000, 4, 4, traceFrequentPutGet, "4 servers x 4 actors: Put on one actor, Get on all w/ frequency limit")
}

func TestRemoteSyncFrequency4(t *testing.T) {
	// This one can be slow due to the time to Get all values sequentially at the end.
	runTestRemoteSyncFrequency(t, 30000, 4, 4, traceFrequentPutAllGet, "4 servers x 4 actors: Put on all actors concurrently (distinct keys), Get on all w/ frequency limit")
}

// === Remote + local sync, w/ size increase limit

func TestRemoteSyncSize1(t *testing.T) {
	runTestRemoteSyncSize(t, 30000, 2, 4, tracePutGet, "2 servers x 4 actors: Put on one actor, Get on all w/ size increase limit")
}

func TestRemoteSyncSize2(t *testing.T) {
	runTestRemoteSyncSize(t, 30000, 2, 4, tracePutAllGet, "2 servers x 4 actors: Put on all actors concurrently (distinct keys), Get on all w/ size increase limit")
}

func TestRemoteSyncSize3(t *testing.T) {
	runTestRemoteSyncSize(t, 30000, 4, 4, tracePutGet, "4 servers x 4 actors: Put on one actor, Get on all w/ size increase limit")
}

func TestRemoteSyncSize4(t *testing.T) {
	runTestRemoteSyncSize(t, 30000, 4, 4, tracePutAllGet, "4 servers x 4 actors: Put on all actors concurrently (distinct keys), Get on all w/ size increase limit")
}

// === Remote + local sync LWW

func TestRemoteSyncLWW1(t *testing.T) {
	runTestRemoteSync(t, 10000, 4, 1, traceLWW1, 6, 6, "4 servers x 1 actor: Put on two actors with small offset (<< sync interval), verify LWW")
}

func TestRemoteSyncLWW2(t *testing.T) {
	runTestRemoteSync(t, 12000, 4, 1, traceLWW2, 7, 9, "4 servers x 1 actor: Put on two actors concurrently, then causally overwrite on one")
}

func TestRemoteSyncLWW3(t *testing.T) {
	runTestRemoteSync(t, 12000, 4, 1, traceLWW3, 7, 9, "4 servers x 1 actor: Put on two actors concurrently, then causally overwrite on a third")
}

func TestRemoteSyncLWW4(t *testing.T) {
	// Only 6 syncs required b/c the two client 0 writes may be coalesced.
	runTestRemoteSync(t, 10000, 4, 1, traceLWW4, 7, 6, "4 servers x 1 actor: Check that a re-Put of the *same* value gets a new timestamp")
}

// === Remote sync, check no broadcast

func TestRemoteSyncNoBcast1(t *testing.T) {
	runTestRemoteSyncNoBcast(t, 10000, 1, tracePutGet, "Remote sync from 1 actor to (1 vs 4) actors should have similar network usage")
}

func TestRemoteSyncNoBcast2(t *testing.T) {
	runTestRemoteSyncNoBcast(t, 10000, 4, tracePutGet, "Remote sync from 4 actors to (1 vs 4) actors should have similar network usage")
}

// === Remote sync stress tests

func TestRemoteSyncStress1(t *testing.T) {
	runTestRemoteSyncStress(t, 30000, 2, 4, 4, traceStress, "2 servers x 4 actors x 4 clients: stress test")
}

func TestRemoteSyncStress2(t *testing.T) {
	runTestRemoteSyncStress(t, 30000, 4, 4, 4, traceStress, "4 servers x 4 actors x 4 clients: stress test")
}
