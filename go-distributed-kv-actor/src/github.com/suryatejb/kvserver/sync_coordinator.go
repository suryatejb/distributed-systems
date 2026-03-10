package kvserver

import (
	"encoding/gob"
	"time"

	"github.com/suryatejb/actor"
)

const (
	// remoteSyncInterval is the interval between remote sync operations (500ms)
	remoteSyncInterval = 500 * time.Millisecond
)

// syncCoordinator manages remote synchronization between servers.
// It collects updates from local query actors and periodically sends them to remote
// coordinators, and distributes incoming remote updates to local query actors.
type syncCoordinator struct {
	context            *actor.ActorContext
	queryActors        []*actor.ActorRef
	remoteCoordinators []*actor.ActorRef
	pendingRemoteSync  map[string]ValueEntry // Batched updates to send remotely
}

// MSetQueryActors message to set the query actors this coordinator manages
type MSetQueryActors struct {
	QueryActors []*actor.ActorRef
}

// MSetRemoteCoordinators message to set the remote coordinators to sync with
type MSetRemoteCoordinators struct {
	RemoteCoordinators []*actor.ActorRef
}

// MRemoteSyncUpdate message from query actor to coordinator with updates to sync remotely
type MRemoteSyncUpdate struct {
	Updates map[string]ValueEntry
}

// MCoordinatorTick for periodic remote syncing
type MCoordinatorTick struct {
}

func init() {
	gob.Register(MSetQueryActors{})
	gob.Register(MSetRemoteCoordinators{})
	gob.Register(MRemoteSyncUpdate{})
	gob.Register(MCoordinatorTick{})
}

// newSyncCoordinator creates a new sync coordinator actor
func newSyncCoordinator(context *actor.ActorContext) actor.Actor {
	sc := &syncCoordinator{
		context:            context,
		queryActors:        make([]*actor.ActorRef, 0),
		remoteCoordinators: make([]*actor.ActorRef, 0),
		pendingRemoteSync:  make(map[string]ValueEntry),
	}

	// Start periodic tick for remote syncing
	context.TellAfter(context.Self, MCoordinatorTick{}, remoteSyncInterval)

	return sc
}

// OnMessage implements actor.Actor.OnMessage
func (sc *syncCoordinator) OnMessage(message any) error {
	switch m := message.(type) {
	case MSetQueryActors:
		// Set the query actors to distribute syncs to
		sc.queryActors = m.QueryActors

	case MSetRemoteCoordinators:
		// Set or append remote coordinators to sync with
		if len(sc.remoteCoordinators) == 0 {
			// Initial setup
			sc.remoteCoordinators = m.RemoteCoordinators
		} else {
			// New server joining - append the new coordinators
			sc.remoteCoordinators = append(sc.remoteCoordinators, m.RemoteCoordinators...)
		}

	case MRemoteSyncUpdate:
		// Received updates from a local query actor to sync remotely.
		// Accumulate them in our pending batch, applying LWW logic to keep only
		// the latest value per key (by timestamp, with ActorUID tie-breaking).
		for key, newValue := range m.Updates {
			existing, exists := sc.pendingRemoteSync[key]
			if !exists {
				sc.pendingRemoteSync[key] = newValue
			} else {
				// Keep the value with the later timestamp
				if newValue.Timestamp > existing.Timestamp {
					sc.pendingRemoteSync[key] = newValue
				} else if newValue.Timestamp == existing.Timestamp {
					// Tie-break by ActorUID
					if newValue.ActorUID > existing.ActorUID {
						sc.pendingRemoteSync[key] = newValue
					}
				}
				// If newValue is older, keep existing
			}
		}

	case MSyncBatch:
		// Received a sync batch from a remote coordinator
		// Distribute it to all local query actors
		for _, queryActor := range sc.queryActors {
			sc.context.Tell(queryActor, m)
		}

	case MRequestSync:
		// Forward the sync request to all query actors
		// They will each respond with their data
		for _, queryActor := range sc.queryActors {
			sc.context.Tell(queryActor, m)
		}

	case MCoordinatorTick:
		// Periodic tick - send accumulated updates to remote coordinators
		if len(sc.pendingRemoteSync) > 0 && len(sc.remoteCoordinators) > 0 {
			batch := MSyncBatch{
				Updates: make(map[string]ValueEntry),
			}
			// Copy pending updates
			for k, v := range sc.pendingRemoteSync {
				batch.Updates[k] = v
			}
			// Send to each remote coordinator (ONE message per remote server)
			for _, remoteCoord := range sc.remoteCoordinators {
				sc.context.Tell(remoteCoord, batch)
			}
			// Clear pending updates
			sc.pendingRemoteSync = make(map[string]ValueEntry)
		}

		// Schedule next tick
		sc.context.TellAfter(sc.context.Self, MCoordinatorTick{}, remoteSyncInterval)
	}
	return nil
}
