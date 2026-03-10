// Trace functions perform some queries on given clients.

package tests

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// Type for a trace function, i.e.,
// a function that does some queries with the given clients.
// Usually, each client is fixed to a specific query actor.
type traceFunc = func(
	t *testing.T,
	clients []clientWr,
	// The deadline for Puts to sync to all clients.
	syncDeadline time.Duration,
	// A string identifier for this "round" (some tests compare multiple rounds).
	// Use it to make distinct keys for each round.
	round string,
)

// === Basic traces.

// Put on one client, Get on all.
func tracePutGet(t *testing.T, clients []clientWr, syncDeadline time.Duration, round string) {
	put(t, true, clients[0], "topping"+round, "cheese")

	waitForSync(t, syncDeadline)

	for i := 0; i < len(clients); i++ {
		get(t, true, clients[i], "topping"+round, "cheese", true)
	}
}

// Put on all clients concurrently (distinct keys), Get on all.
func tracePutAllGet(t *testing.T, clients []clientWr, syncDeadline time.Duration, round string) {
	for i := 0; i < len(clients); i++ {
		put(t, true, clients[i], fmt.Sprintf("key%s_%d", round, i), fmt.Sprintf("value%s_%d", round, i))
	}

	waitForSync(t, syncDeadline)

	for i := 0; i < len(clients); i++ {
		for j := 0; j < len(clients); j++ {
			get(t, true, clients[j], fmt.Sprintf("key%s_%d", round, i), fmt.Sprintf("value%s_%d", round, i), true)
		}
	}
}

// Put on one client, Get on all, every freqChallengeInterval.
func traceFrequentPutGet(t *testing.T, clients []clientWr, syncDeadline time.Duration, round string) {
	t.Logf("Calling Put on client 0 every %s for %s", freqChallengeInterval, freqChallengeLen)
	done := make(chan bool, freqChallengeCount)
	for j := 0; j < freqChallengeCount; j++ {
		// Run these in goroutines so we're not waiting on the round trip to
		// the server each time.
		go func(jCopy int) {
			put(t, false, clients[0], fmt.Sprintf("topping%s_%d", round, jCopy), fmt.Sprintf("cheese_%d", jCopy))
			done <- true
		}(j)
		time.Sleep(freqChallengeInterval)
	}

	t.Log("Waiting for all Puts to return")
	for j := 0; j < freqChallengeCount; j++ {
		<-done
	}

	waitForSync(t, syncDeadline)

	t.Log("Checking List on all clients")
	ans := make(map[string]string)
	for j := 0; j < freqChallengeCount; j++ {
		ans[fmt.Sprintf("topping%s_%d", round, j)] = fmt.Sprintf("cheese_%d", j)
	}
	for i := 0; i < len(clients); i++ {
		list(t, false, clients[i], "", ans)
	}
}

// === Frequency traces.

// For frequency tests, we send more often than the allowed interval (100ms)
// for several seconds
// to make sure the frequency limit is respected.
const freqChallengeInterval = time.Duration(10) * time.Millisecond
const freqChallengeCount = 300
const freqChallengeLen = time.Duration(freqChallengeCount) * freqChallengeInterval

// Put on all clients concurrently (distinct keys), Get on all, every freqChallengeInterval.
func traceFrequentPutAllGet(t *testing.T, clients []clientWr, syncDeadline time.Duration, round string) {
	t.Logf("Calling Put on all clients every %s for %s", freqChallengeInterval, freqChallengeLen)
	done := make(chan bool, freqChallengeCount*len(clients))
	for j := 0; j < freqChallengeCount; j++ {
		for i := 0; i < len(clients); i++ {
			// Run these in goroutines so we're not waiting on the round trip to
			// the server each time.
			go func(iCopy, jCopy int) {
				put(t, false, clients[iCopy], fmt.Sprintf("key%s_%d_%d", round, iCopy, jCopy), fmt.Sprintf("value%s_%d_%d", round, iCopy, jCopy))
				done <- true
			}(i, j)
		}
		time.Sleep(freqChallengeInterval)
	}

	t.Log("Waiting for all Puts to return")
	for j := 0; j < freqChallengeCount*len(clients); j++ {
		<-done
	}

	waitForSync(t, syncDeadline)

	t.Log("Checking List on all clients")
	ans := make(map[string]string)
	for j := 0; j < freqChallengeCount; j++ {
		for i := 0; i < len(clients); i++ {
			ans[fmt.Sprintf("key%s_%d_%d", round, i, j)] = fmt.Sprintf("value%s_%d_%d", round, i, j)
		}
	}
	for i := 0; i < len(clients); i++ {
		list(t, false, clients[i], "", ans)
	}
}

// === LWW traces

// We assume that Puts initiated this far apart should be distinguished by
// LWW, given that all servers' clocks are perfectly synchronized.
const lwwGran = time.Duration(10) * time.Millisecond

// Put on two actors with small offset (<< sync interval), verify LWW.
func traceLWW1(t *testing.T, clients []clientWr, syncDeadline time.Duration, round string) {
	t.Logf("Putting values ~concurrently on two clients with different timestamps")
	// Do in goroutines so we are close in time even with slow responses.
	done := make(chan bool, 2)
	go func() {
		put(t, true, clients[0], "topping"+round, "cheese")
		done <- true
	}()
	go func() {
		time.Sleep(lwwGran)
		put(t, true, clients[1], "topping"+round, "mustard")
		done <- true
	}()
	<-done
	<-done

	waitForSync(t, syncDeadline)

	t.Logf("Checking LWW: all clients see the later Put")
	for i := 0; i < len(clients); i++ {
		get(t, true, clients[i], "topping"+round, "mustard", true)
	}
}

// Put on two actors concurrently, then causally overwrite on one.
func traceLWW2(t *testing.T, clients []clientWr, syncDeadline time.Duration, round string) {
	t.Logf("Putting values ~concurrently on two clients with different timestamps")
	// Do in goroutines so we are concurrent.
	done := make(chan bool, 2)
	go func() {
		put(t, true, clients[0], "topping"+round, "cheese")
		done <- true
	}()
	go func() {
		time.Sleep(lwwGran)
		put(t, true, clients[1], "topping"+round, "mustard")
		done <- true
	}()
	<-done
	<-done

	waitForSync(t, syncDeadline)

	t.Logf("Putting a new value on the winning client")
	put(t, true, clients[1], "topping"+round, "pickles")

	waitForSync(t, syncDeadline)

	t.Logf("Checking all clients see the (causally) last Put")
	for i := 0; i < len(clients); i++ {
		get(t, true, clients[i], "topping"+round, "pickles", true)
	}
}

// Put on two actors concurrently, then causally overwrite on a third.
func traceLWW3(t *testing.T, clients []clientWr, syncDeadline time.Duration, round string) {
	t.Logf("Putting values ~concurrently on two clients with different timestamps")
	// Do in goroutines so we are concurrent.
	done := make(chan bool, 2)
	go func() {
		put(t, true, clients[0], "topping"+round, "cheese")
		done <- true
	}()
	go func() {
		time.Sleep(lwwGran)
		put(t, true, clients[1], "topping"+round, "mustard")
		done <- true
	}()
	<-done
	<-done

	waitForSync(t, syncDeadline)

	t.Logf("Putting a new value on a third client")
	put(t, true, clients[2], "topping"+round, "pickles")

	waitForSync(t, syncDeadline)

	t.Logf("Checking all clients see the (causally) last Put")
	for i := 0; i < len(clients); i++ {
		get(t, true, clients[i], "topping"+round, "pickles", true)
	}
}

// Check that a re-Put of the *same* value gets a new timestamp.
func traceLWW4(t *testing.T, clients []clientWr, syncDeadline time.Duration, round string) {
	t.Logf("Putting values ~concurrently on two clients with different timestamps")
	// Do in goroutines so we are concurrent.
	done := make(chan bool, 2)
	go func() {
		put(t, true, clients[0], "topping"+round, "cheese")
		time.Sleep(lwwGran)
		time.Sleep(lwwGran)
		t.Logf("Re-Putting the losing value, later than all others")
		put(t, true, clients[0], "topping"+round, "cheese")
		done <- true
	}()
	go func() {
		time.Sleep(lwwGran)
		put(t, true, clients[1], "topping"+round, "mustard")
		done <- true
	}()
	<-done
	<-done

	waitForSync(t, syncDeadline)

	t.Logf("Checking all clients see the re-Put")
	for i := 0; i < len(clients); i++ {
		get(t, true, clients[i], "topping"+round, "cheese", true)
	}
}

// === Stress trace

type randomOp struct {
	// Get, Put, or List
	query string
	// key or prefix
	key string
	// value if Put
	value string
}

// Perform 100 random ops on each client, checking results as well
// as possible given timing uncertainty.
// Runners will time this to verify sufficient throughput.
func traceStress(t *testing.T, clients []clientWr, syncDeadline time.Duration, round string) {
	// Generate random ops for each client. Do it now to ensure a consistent
	// order.
	rand.Seed(42)
	ops := make([][]randomOp, len(clients))
	allKeys := []string{}
	gets := 0
	puts := 0
	lists := 0
	for i := 0; i < len(clients); i++ {
		ops[i] = make([]randomOp, 100)
		for j := 0; j < len(ops[i]); j++ {
			var op randomOp
			switch rand.Intn(3) {
			case 0:
				op = randomOp{"Get", fmt.Sprintf("key%d", rand.Intn(50)), ""}
				gets++
			case 1:
				key := fmt.Sprintf("key%d", rand.Intn(50))
				op = randomOp{
					"Put",
					key,
					fmt.Sprintf("value%d", rand.Intn(10)),
				}
				allKeys = append(allKeys, key)
				puts++
			case 2:
				op = randomOp{"List", fmt.Sprintf("key%d", rand.Intn(5)), ""}
				lists++
			}
			ops[i][j] = op
		}
	}

	// Perform the random ops concurrently on each client, with pauses
	// between so it lasts for at least 2 syncDeadlines.
	pause := syncDeadline / time.Duration(50)
	t.Logf("Performing %d Gets, %d Puts, and %d Lists on %d clients concurrently, with %s pauses between ops", gets, puts, lists, len(clients), pause)
	done := make(chan bool, len(clients))
	for i := 0; i < len(clients); i++ {
		go func(iCopy int) {
			client := clients[iCopy]
			for _, op := range ops[iCopy] {
				success := true
				switch op.query {
				case "Get":
					// Due to the flexible syncDeadline, we don't know
					// exactly what value to expect. Instead just check
					// the Get doesn't error.
					_, _, err := client.c.Get(op.key)
					if err != nil {
						t.Errorf("[ERROR] (%s) Get(%q) returned error: %s", client.name, op.key, err)
						success = false
					}
				case "Put":
					success = put(t, false, client, op.key, op.value)
				case "List":
					// Due to the flexible syncDeadline, we don't know
					// exactly what value to expect. Instead just check
					// the List doesn't error.
					_, err := client.c.List(op.key)
					if err != nil {
						t.Errorf("[ERROR] (%s) List(%q) returned error: %s", client.name, op.key, err)
						success = false
					}
				}
				if !success {
					done <- false
					return
				}
				time.Sleep(pause)
			}
			done <- true
		}(i)
	}

	t.Log("Waiting for queries to finish")
	failed := false
	for i := 0; i < len(clients); i++ {
		if !<-done {
			// The test already failed, so skip checking eventual consistency.
			failed = true
		}
	}
	if failed {
		return
	}

	waitForSync(t, syncDeadline)

	t.Log("Checking clients for eventual consistency")
	entries, err := clients[0].c.List("")
	if err != nil {
		t.Errorf("[ERROR] (%s) List(%q) returned error: %s", clients[0].name, "", err)
		return
	}
	// For efficiency and a bit more stress, check all other clients concurrently.
	done = make(chan bool, len(clients)-1)
	for i := 1; i < len(clients); i++ {
		go func(iCopy int) {
			list(t, false, clients[iCopy], "", entries)
			done <- true
		}(i)
	}
	for i := 1; i < len(clients); i++ {
		<-done
	}

	t.Log("Checking that key set is correct")
	// Due to timing uncertainties (exacerbated by possible slowness), we don't
	// know exactly what values to expect, but we do know what keys to expect.
	if len(entries) > len(allKeys) {
		t.Errorf("List(\"\") returned a key that was never used")
		return
	}
	for _, key := range allKeys {
		if _, ok := entries[key]; !ok {
			t.Errorf("Key %q was Put but is not in final state", key)
			return
		}
	}
}
