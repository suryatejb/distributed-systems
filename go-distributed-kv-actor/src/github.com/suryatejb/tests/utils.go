// Shared functions for tests.

package tests

import (
	"sync"
	"testing"
	"time"

	"github.com/suryatejb/actor"
	"github.com/suryatejb/kvclient"
	"github.com/suryatejb/kvserver"
)

// === Client utils.

type fixedAddressRouter struct {
	address string
}

type dynamicAddressRouter struct {
	mux       sync.Mutex
	addresses []string
	Idx       int
}

func (router fixedAddressRouter) NextAddr() string {
	return router.address
}

func (router *dynamicAddressRouter) NextAddr() string {
	router.mux.Lock()
	addr := router.addresses[router.Idx]
	router.Idx = (router.Idx + 1) % len(router.addresses)
	router.mux.Unlock()
	return addr
}

// kvclient.Client wrapper.
type clientWr struct {
	c    *kvclient.Client
	name string
}

// NewClient wrapper that takes a fixed queryActor address.
// Usually, use name to indicate the server/actor this client is connected to.
func newClient(fixedAddress string, name string) clientWr {
	c := kvclient.NewClient(fixedAddressRouter{fixedAddress})
	return clientWr{c, name}
}

// === Server utils.

var nextPort = 12005

// Returns a port that is likely unoccupied in the right range for a kvserver.
func newPort() int {
	nextPort = (nextPort + 20) % 32003
	return nextPort
}

// kvserver.Server wrapper
type serverWr struct {
	s      *kvserver.Server
	desc   string
	system *actor.ActorSystem
}

// NewServer wrapper that checks for errors and sets up test stuff.
func newServer(t *testing.T, startPort int, queryActorCount int, remoteDescs []string) serverWr {
	// Copy remoteDescs to guard against future edits.
	remoteDescsCopy := make([]string, len(remoteDescs))
	copy(remoteDescsCopy, remoteDescs)

	t.Logf("Starting server with ActorSystem on port %d and %d query actors on ports %d-%d", startPort, queryActorCount, startPort+1, startPort+queryActorCount)
	server, desc, err := kvserver.NewServer(startPort, queryActorCount, remoteDescsCopy)
	if err != nil {
		t.Fatalf("Failed to start server on ports %d-%d: %s", startPort, startPort+queryActorCount, err)
		return serverWr{nil, "", nil}
	}

	system := actor.LastActorSystem()
	if system == nil {
		t.Fatal("Not using ActorSystem? NewServer returned successfully without NewActorSystem returning successfully.")
		return serverWr{nil, "", nil}
	}

	return serverWr{server, desc, system}
}

// === Misc utils.

func waitForSync(t *testing.T, syncDeadline time.Duration) {
	t.Logf("Waiting %s for sync", syncDeadline)
	time.Sleep(syncDeadline)
}
