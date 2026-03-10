// Key-value store server startup tests.

package tests

import (
	"fmt"
	"testing"
	"time"

	"github.com/suryatejb/staff"
)

func runServerStartupTest(t *testing.T, timeoutMs int, trace func(), desc string) {
	fmt.Printf("=== %s: %s\n", t.Name(), desc)

	staff.SetArtiLatencyMs(remoteServerLatencyMs)
	defer staff.SetArtiLatencyMs(0)

	doneCh := make(chan bool, 0)
	timeoutCh := time.After(time.Duration(timeoutMs) * time.Millisecond)

	go func() {
		trace()
		doneCh <- true
	}()

	select {
	case <-doneCh:
	case <-timeoutCh:
		t.Fatalf("Test timed out after %.2f secs", float64(timeoutMs)/1000.0)
	}
}

func TestServerStartup1(t *testing.T) {
	trace := func() {
		t.Log("Starting one server and Putting a value")
		port0 := newPort()
		server0 := newServer(t, port0, 1, []string{})
		if server0.s == nil {
			return
		}
		defer server0.s.Close()

		client0 := newClient(fmt.Sprintf("localhost:%d", port0+1), "server 0")
		defer client0.c.Close()
		put(t, true, client0, "cat/name", "charlie")

		t.Log("Starting a second server")
		port1 := newPort()
		server1 := newServer(t, port1, 1, []string{server0.desc})
		if server1.s == nil {
			return
		}
		defer server1.s.Close()

		client1 := newClient(fmt.Sprintf("localhost:%d", port1+1), "server 1")
		defer client1.c.Close()

		waitForSync(t, remoteSyncDeadline)

		t.Log("Checking for value on second server")
		get(t, true, client1, "cat/name", "charlie", true)
	}

	runServerStartupTest(t, 10000, trace, "2 servers x 1 actor: values on old server make it to new server")
}

func TestServerStartup2(t *testing.T) {
	trace := func() {
		t.Log("Starting one server and Putting a value")
		port0 := newPort()
		server0 := newServer(t, port0, 4, []string{})
		defer server0.s.Close()
		if server0.s == nil {
			return
		}

		clients0 := make([]clientWr, 4)
		for i := 0; i < len(clients0); i++ {
			clients0[i] = newClient(fmt.Sprintf("localhost:%d", port0+1+i), fmt.Sprintf("server 0, actor %d", i))
			defer clients0[i].c.Close()
		}
		put(t, true, clients0[0], "cat/name", "charlie")

		t.Log("Starting a second server")
		port1 := newPort()
		server1 := newServer(t, port1, 4, []string{server0.desc})
		if server1.s == nil {
			return
		}
		defer server1.s.Close()

		clients1 := make([]clientWr, 4)
		for i := 0; i < len(clients1); i++ {
			clients1[i] = newClient(fmt.Sprintf("localhost:%d", port1+1+i), fmt.Sprintf("server 1, actor %d", i))
			defer clients1[i].c.Close()
		}

		waitForSync(t, remoteSyncDeadline)

		t.Log("Checking for value on second server's actors")
		for i := 0; i < len(clients1); i++ {
			get(t, true, clients1[i], "cat/name", "charlie", true)
		}
	}

	runServerStartupTest(t, 10000, trace, "2 servers x 4 actors: values on old server make it to new server")
}

func TestServerStartup3(t *testing.T) {
	trace := func() {
		t.Log("Starting one server")
		port0 := newPort()
		server0 := newServer(t, port0, 1, []string{})
		if server0.s == nil {
			return
		}
		defer server0.s.Close()

		client0 := newClient(fmt.Sprintf("localhost:%d", port0+1), "server 0")
		defer client0.c.Close()

		t.Log("Starting a second server")
		port1 := newPort()
		server1 := newServer(t, port1, 1, []string{server0.desc})
		if server1.s == nil {
			return
		}
		defer server1.s.Close()

		client1 := newClient(fmt.Sprintf("localhost:%d", port1+1), "server 1")
		defer client1.c.Close()

		t.Log("Putting a value on the second server while it connects to the first")
		time.Sleep(remoteServerLatencyMs)
		put(t, true, client1, "cat/name", "charlie")

		waitForSync(t, remoteSyncDeadline)

		t.Log("Checking for value on first server")
		get(t, true, client1, "cat/name", "charlie", true)
	}

	runServerStartupTest(t, 10000, trace, "2 servers x 1 actor: Put on new server during startup makes it to old server")
}

func TestServerStartup4(t *testing.T) {
	trace := func() {
		t.Log("Starting one server and putting a value on each actor")
		port0 := newPort()
		server0 := newServer(t, port0, 4, []string{})
		defer server0.s.Close()
		if server0.s == nil {
			return
		}

		clients0 := make([]clientWr, 4)
		for i := 0; i < len(clients0); i++ {
			clients0[i] = newClient(fmt.Sprintf("localhost:%d", port0+1+i), fmt.Sprintf("server 0, actor %d", i))
			defer clients0[i].c.Close()
			put(t, true, clients0[0], fmt.Sprintf("key0_%d", i), fmt.Sprintf("value0_%d", i))
		}

		t.Log("Starting a second server")
		port1 := newPort()
		server1 := newServer(t, port1, 4, []string{server0.desc})
		if server1.s == nil {
			return
		}
		defer server1.s.Close()

		clients1 := make([]clientWr, 4)
		for i := 0; i < len(clients1); i++ {
			clients1[i] = newClient(fmt.Sprintf("localhost:%d", port1+1+i), fmt.Sprintf("server 1, actor %d", i))
			defer clients1[i].c.Close()
		}

		t.Log("Doing some concurrent writes while second server connects to first")
		done := make(chan bool)
		go func() {
			// Overwrite clients0[1]'s value.
			put(t, true, clients1[0], "key0_1", "overwrite0")
			done <- true
		}()
		go func() {
			// Put a new key on clients1[1].
			put(t, true, clients1[1], "newKey_1", "newValue_1")
			done <- true
		}()
		go func() {
			// Update clients0[2]'s value.
			time.Sleep(lwwGran)
			put(t, true, clients0[2], "key0_2", "newValue_2")
			done <- true
		}()
		go func() {
			// Put a new key on clients1[2], then LWW overwrite on clients0[3].
			put(t, true, clients1[2], "newKey_2", "first2")
			time.Sleep(lwwGran)
			put(t, true, clients0[3], "newKey_2", "last2")
			done <- true
		}()
		go func() {
			// Put a new key on clients0[2], then LWW overwrite on clients1[3].
			put(t, true, clients0[2], "newKey_3", "first3")
			time.Sleep(lwwGran)
			put(t, true, clients1[3], "newKey_3", "last3")
			done <- true
		}()
		for i := 0; i < 5; i++ {
			<-done
		}

		waitForSync(t, remoteSyncDeadline)

		t.Log("Checking for LWW values on all actors")
		allClients := make([]clientWr, 0)
		allClients = append(allClients, clients0...)
		allClients = append(allClients, clients1...)
		ans := map[string]string{
			"key0_0":   "value0_0",
			"key0_1":   "overwrite0",
			"key0_2":   "newValue_2",
			"key0_3":   "value0_3",
			"newKey_1": "newValue_1",
			"newKey_2": "last2",
			"newKey_3": "last3",
		}
		for i := 0; i < len(allClients); i++ {
			list(t, false, allClients[i], "", ans)
		}
	}

	runServerStartupTest(t, 15000, trace, "2 servers x 4 actors: various Puts around startup time give LWW result")
}

func TestServerStartup5(t *testing.T) {
	trace := func() {
		// Each time a server starts up, we put a key on all existing
		// servers of the form key<server>_<round>.
		servers := make([]serverWr, 4)
		clients := make([][]clientWr, 4)
		remoteDescs := []string{}
		ans := make(map[string]string)
		for round := 0; round < 4; round++ {
			t.Logf("Starting server %d", round)
			port := newPort()
			servers[round] = newServer(t, port, 4, remoteDescs)
			if servers[round].s == nil {
				return
			}
			defer servers[round].s.Close()
			remoteDescs = append(remoteDescs, servers[round].desc)

			// Clients for the new server.
			clients[round] = make([]clientWr, 4)
			for j := 0; j < 4; j++ {
				address := fmt.Sprintf("localhost:%d", port+1+j)
				clients[round][j] = newClient(address, fmt.Sprintf("server %d, actor %d", round, j))
				defer clients[round][j].c.Close()
			}

			t.Log("Putting a value on each server while new server connects to old")
			for i := 0; i < round; i++ {
				// Choose an actor arbitrarily.
				key := fmt.Sprintf("key%d_%d", i, round)
				value := fmt.Sprintf("value%d_%d", i, round)
				put(t, true, clients[i][(round+i)%4], key, value)
				ans[key] = value
			}

			waitForSync(t, remoteSyncDeadline)
		}

		t.Log("Checking that all actors have all entries")
		for i := 0; i < len(clients); i++ {
			for j := 0; j < len(clients[i]); j++ {
				list(t, false, clients[i][j], "", ans)
			}
		}
	}

	runServerStartupTest(t, 25000, trace, "4 servers x 4 actors: Puts at various times during startup make it to all actors")
}
