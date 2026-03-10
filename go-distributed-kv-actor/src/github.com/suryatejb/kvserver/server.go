// Package kvserver implements the backend server for a
// geographically distributed, highly available, NoSQL key-value store.
package kvserver

import (
	"encoding/json"
	"fmt"
	"net"
	"net/rpc"

	"github.com/suryatejb/actor"
)

// ServerDesc contains the description of a server for remote syncing
type ServerDesc struct {
	// SyncCoordinator receives remote syncs and distributes to local actors
	SyncCoordinator *actor.ActorRef
	// QueryActors for direct query handling (for initial sync requests)
	QueryActors []*actor.ActorRef
}

// A single server in the key-value store, running some number of
// query actors - nominally one per CPU core. Each query actor
// provides a key/value storage service on its own port.
//
// Different query actors (both within this server and across connected
// servers) periodically sync updates (Puts) following an eventually
// consistent, last-writer-wins strategy.
type Server struct {
	system    *actor.ActorSystem
	listeners []net.Listener
}

// OPTIONAL: Error handler for ActorSystem.OnError.
//
// Print the error or call debug.PrintStack() in this function.
// When starting an ActorSystem, call ActorSystem.OnError(errorHandler).
// This can help debug server-side errors more easily.
func errorHandler(err error) {
}

// Starts a server running queryActorCount query actors.
//
// The server's actor system listens for remote messages (from other actor
// systems) on startPort. The server listens for RPCs from kvclient.Clients
// on ports [startPort + 1, startPort + 2, ..., startPort + queryActorCount].
// Each of these "query RPC servers" answers queries by asking a specific
// query actor.
//
// remoteDescs contains a "description" string for each existing server in the
// key-value store. Specifically, each slice entry is the desc returned by
// an existing server's own NewServer call. The description strings are opaque
// to callers, but typically an implementation uses JSON-encoded data containing,
// e.g., actor.ActorRef's that remote servers' actors should contact.
//
// Before returning, NewServer starts the ActorSystem, all query actors, and
// all query RPC servers. If there is an error starting anything, that error is
// returned instead.
func NewServer(startPort int, queryActorCount int, remoteDescs []string) (server *Server, desc string, err error) {
	// Create actor system
	system, err := actor.NewActorSystem(startPort)
	if err != nil {
		return nil, "", err
	}

	system.OnError(errorHandler)

	server = &Server{
		system:    system,
		listeners: make([]net.Listener, 0, queryActorCount),
	}

	// Create sync coordinator for this server
	syncCoordRef := system.StartActor(newSyncCoordinator)

	// Parse remote descriptions to get remote sync coordinators
	remoteSyncCoords := make([]*actor.ActorRef, 0)
	remoteQueryActors := make([]*actor.ActorRef, 0)
	for _, remoteDescStr := range remoteDescs {
		var remoteDesc ServerDesc
		err := json.Unmarshal([]byte(remoteDescStr), &remoteDesc)
		if err != nil {
			server.Close()
			return nil, "", err
		}
		remoteSyncCoords = append(remoteSyncCoords, remoteDesc.SyncCoordinator)
		remoteQueryActors = append(remoteQueryActors, remoteDesc.QueryActors...)
	}

	// Start query actors and collect their refs
	queryActorRefs := make([]*actor.ActorRef, 0, queryActorCount)
	for i := 0; i < queryActorCount; i++ {
		queryActorRef := system.StartActor(newQueryActor)
		queryActorRefs = append(queryActorRefs, queryActorRef)
	}

	// Tell sync coordinator about query actors and remote coordinators
	system.Tell(syncCoordRef, MSetQueryActors{QueryActors: queryActorRefs})
	system.Tell(syncCoordRef, MSetRemoteCoordinators{RemoteCoordinators: remoteSyncCoords})

	// Initialize each query actor with refs to other actors and local coordinator
	for i, queryActorRef := range queryActorRefs {
		// Build list of local actors (all except self)
		localActors := make([]*actor.ActorRef, 0, queryActorCount-1)
		for j, otherRef := range queryActorRefs {
			if i != j {
				localActors = append(localActors, otherRef)
			}
		}

		// Send init message with local coordinator reference
		initMsg := MInit{
			LocalActors:      localActors,
			LocalCoordinator: syncCoordRef,
		}
		system.Tell(queryActorRef, initMsg)
	}

	// Notify existing remote sync coordinators about this new server's sync coordinator
	// This allows bidirectional syncing between coordinators
	if len(remoteSyncCoords) > 0 {
		addMsg := MSetRemoteCoordinators{
			RemoteCoordinators: []*actor.ActorRef{syncCoordRef},
		}
		for _, remoteSyncCoord := range remoteSyncCoords {
			system.Tell(remoteSyncCoord, addMsg)
		}
	}

	// Request startup sync: our sync coordinator requests data from remote sync coordinators
	// This sends ONE request per remote server (not per actor), respecting frequency limits
	if len(remoteSyncCoords) > 0 {
		reqSyncMsg := MRequestSync{Sender: syncCoordRef}
		for _, remoteSyncCoord := range remoteSyncCoords {
			system.Tell(remoteSyncCoord, reqSyncMsg)
		}
	}

	// Start RPC servers for each query actor
	for i := 0; i < queryActorCount; i++ {
		// Create query receiver for this actor
		receiver := &queryReceiver{
			system:        system,
			queryActorRef: queryActorRefs[i],
		}

		// Start RPC server for this query actor
		rpcServer := rpc.NewServer()
		err := rpcServer.RegisterName("QueryReceiver", receiver)
		if err != nil {
			server.Close()
			return nil, "", err
		}

		// Listen on startPort + i + 1
		port := startPort + i + 1
		address := fmt.Sprintf("localhost:%d", port)
		ln, err := net.Listen("tcp", address)
		if err != nil {
			server.Close()
			return nil, "", err
		}
		server.listeners = append(server.listeners, ln)

		// Start goroutine to accept connections
		go func(rpcServer *rpc.Server, listener net.Listener) {
			for {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				go rpcServer.ServeConn(conn)
			}
		}(rpcServer, ln)
	}

	// Create and encode server description
	serverDesc := ServerDesc{
		SyncCoordinator: syncCoordRef,
		QueryActors:     queryActorRefs,
	}
	descBytes, err := json.Marshal(serverDesc)
	if err != nil {
		server.Close()
		return nil, "", err
	}

	return server, string(descBytes), nil
}

// OPTIONAL: Closes the server, including its actor system
// and all RPC servers.
//
// You are not required to implement this function for full credit; the tests end
// by calling Close but do not check that it does anything. However, you
// may find it useful to implement this so that you can run multiple/repeated
// tests in the same "go test" command without cross-test interference (in
// particular, old test servers' squatting on ports.)
//
// Likewise, you may find it useful to close a partially-started server's
// resources if there is an error in NewServer.
func (server *Server) Close() {
	if server.system != nil {
		server.system.Close()
	}
	for _, ln := range server.listeners {
		if ln != nil {
			ln.Close()
		}
	}
}
