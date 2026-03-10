package kvserver

import (
	"encoding/gob"
	"fmt"
	"strings"
	"time"

	"github.com/suryatejb/actor"
)

const (
	// localSyncInterval is the interval between local sync operations (100ms)
	localSyncInterval = 100 * time.Millisecond
)

// Sync Strategy Documentation (for manual grading):
//
// LOCAL SYNCING:
// Each query actor periodically (every 100ms) sends its updates to all other local
// query actors on the same server. Updates are batched together to limit message overhead
// to max 10 messages/sec per actor pair. The merge function uses last-writer-wins with
// timestamp comparison and tie-breaking by ActorRef.Uid().
//
// REMOTE SYNCING:
// Query actors send their remote updates to a local sync coordinator actor.
// The coordinator accumulates updates from all local query actors, applying LWW logic
// to ensure only the latest value per key is kept. Every 500ms, the coordinator sends
// one batched MSyncBatch message to each remote coordinator (one message per remote server).
// Remote coordinators distribute received batches to all their local query actors.
// This coordinator-to-coordinator pattern keeps remote sync frequency well under the
// 10 messages/sec limit. Startup sync requests are also routed through coordinators.

// Implement your queryActor in this file.
// See example/counter_actor.go for an example actor using the
// github.com/suryatejb/actor package.

// ValueEntry stores a value with its timestamp and originating actor UID for LWW
type ValueEntry struct {
	Value     string
	Timestamp int64  // Unix timestamp in milliseconds
	ActorUID  string // For tie-breaking
}

// Message types for query operations

// MGet message for Get queries
type MGet struct {
	Key    string
	Sender *actor.ActorRef
}

// MPut message for Put queries
type MPut struct {
	Key       string
	Value     string
	Timestamp int64  // Timestamp when Put was received
	ActorUID  string // UID of actor that originally processed the Put
	Sender    *actor.ActorRef
}

// MList message for List queries
type MList struct {
	Prefix string
	Sender *actor.ActorRef
}

// MSyncBatch message for syncing multiple updates between actors
type MSyncBatch struct {
	Updates map[string]ValueEntry
}

// MInit message to initialize actor with references to other actors
type MInit struct {
	LocalActors      []*actor.ActorRef
	LocalCoordinator *actor.ActorRef // Local sync coordinator for remote syncs
}

// MTick message for periodic syncing
type MTick struct {
}

// MRequestSync message to request all data from an actor (for startup)
type MRequestSync struct {
	Sender *actor.ActorRef
}

// Response messages

// MGetReply response for Get queries
type MGetReply struct {
	Value string
	Ok    bool
}

// MPutReply response for Put queries
type MPutReply struct {
}

// MListReply response for List queries
type MListReply struct {
	Entries map[string]string
}

func init() {
	// Register message types with gob
	gob.Register(MGet{})
	gob.Register(MPut{})
	gob.Register(MList{})
	gob.Register(MGetReply{})
	gob.Register(MPutReply{})
	gob.Register(MListReply{})
	gob.Register(MSyncBatch{})
	gob.Register(MInit{})
	gob.Register(MTick{})
	gob.Register(MRequestSync{})
	gob.Register(ValueEntry{})
}

// queryActor handles Get, Put, and List operations for a subset of keys.
// It periodically syncs updates with other query actors using last-writer-wins strategy.
type queryActor struct {
	context *actor.ActorContext
	// Local hash table storing key-value pairs with timestamps
	store map[string]ValueEntry
	// Updates to sync to other actors (key -> ValueEntry)
	pendingLocalSync  map[string]ValueEntry
	pendingRemoteSync map[string]ValueEntry
	// References to other actors
	localActors      []*actor.ActorRef
	localCoordinator *actor.ActorRef // Local sync coordinator for remote syncs
	// Tick counter for local syncs
	tickCount int
}

// "Constructor" for queryActors, used in ActorSystem.StartActor.
func newQueryActor(context *actor.ActorContext) actor.Actor {
	qa := &queryActor{
		context:           context,
		store:             make(map[string]ValueEntry),
		pendingLocalSync:  make(map[string]ValueEntry),
		pendingRemoteSync: make(map[string]ValueEntry),
		localActors:       make([]*actor.ActorRef, 0),
		localCoordinator:  nil,
		tickCount:         0,
	}

	// Start periodic sync timer (every 100ms)
	qa.context.TellAfter(qa.context.Self, MTick{}, localSyncInterval)

	return qa
}

// merge applies last-writer-wins rule to merge a new entry
// Returns true if the new entry should be accepted
func (qa *queryActor) merge(key string, newEntry ValueEntry) bool {
	existing, exists := qa.store[key]

	if !exists {
		qa.store[key] = newEntry
		return true
	}

	// Compare timestamps
	if newEntry.Timestamp > existing.Timestamp {
		qa.store[key] = newEntry
		return true
	} else if newEntry.Timestamp < existing.Timestamp {
		return false
	}

	// Tie-break by ActorUID (lexicographic)
	if newEntry.ActorUID > existing.ActorUID {
		qa.store[key] = newEntry
		return true
	}

	return false
}

// OnMessage implements actor.Actor.OnMessage.
func (qa *queryActor) OnMessage(message any) error {
	switch m := message.(type) {
	case MInit:
		// Initialize actor with references to other actors
		qa.localActors = m.LocalActors
		qa.localCoordinator = m.LocalCoordinator

	case MGet:
		// Handle Get query
		entry, ok := qa.store[m.Key]
		reply := MGetReply{
			Value: entry.Value,
			Ok:    ok,
		}
		qa.context.Tell(m.Sender, reply)

	case MPut:
		// Handle Put query - create entry with timestamp and UID
		timestamp := m.Timestamp
		actorUID := m.ActorUID

		// If this is a new Put (not a sync), set timestamp and UID
		if timestamp == 0 {
			timestamp = time.Now().UnixMilli()
			actorUID = qa.context.Self.Uid()
		}

		newEntry := ValueEntry{
			Value:     m.Value,
			Timestamp: timestamp,
			ActorUID:  actorUID,
		}

		// Merge into store - only sync if merge succeeds
		if qa.merge(m.Key, newEntry) {
			// Add to pending sync batches only if the update was accepted
			qa.pendingLocalSync[m.Key] = newEntry
			qa.pendingRemoteSync[m.Key] = newEntry
		}

		// Send reply
		reply := MPutReply{}
		qa.context.Tell(m.Sender, reply)

	case MList:
		// Handle List query
		entries := make(map[string]string)
		for key, entry := range qa.store {
			if strings.HasPrefix(key, m.Prefix) {
				entries[key] = entry.Value
			}
		}
		reply := MListReply{
			Entries: entries,
		}
		qa.context.Tell(m.Sender, reply)

	case MSyncBatch:
		// Handle sync batch from another actor
		for key, entry := range m.Updates {
			qa.merge(key, entry)
		}

	case MTick:
		// Periodic sync
		qa.performSync()

		// Schedule next tick
		qa.context.TellAfter(qa.context.Self, MTick{}, localSyncInterval)

	case MRequestSync:
		// Send all current data to requester
		if len(qa.store) > 0 {
			batch := MSyncBatch{
				Updates: make(map[string]ValueEntry),
			}
			for k, v := range qa.store {
				batch.Updates[k] = v
			}
			qa.context.Tell(m.Sender, batch)
		}

	default:
		return fmt.Errorf("Unexpected queryActor message type: %T", m)
	}
	return nil
}

// performSync sends pending updates to local actors and local coordinator
func (qa *queryActor) performSync() {
	qa.tickCount++

	// Always do local sync on every tick (every 100ms)
	if len(qa.pendingLocalSync) > 0 && len(qa.localActors) > 0 {
		batch := MSyncBatch{
			Updates: make(map[string]ValueEntry),
		}
		// Copy the pending updates
		for k, v := range qa.pendingLocalSync {
			batch.Updates[k] = v
		}
		// Send to each local actor
		for _, localActor := range qa.localActors {
			qa.context.Tell(localActor, batch)
		}
		// Clear pending local sync
		qa.pendingLocalSync = make(map[string]ValueEntry)
	}

	// Send remote updates to local coordinator on every tick
	// The coordinator will batch and send to remote coordinators at its own frequency
	if len(qa.pendingRemoteSync) > 0 && qa.localCoordinator != nil {
		update := MRemoteSyncUpdate{
			Updates: make(map[string]ValueEntry),
		}
		// Copy the pending updates
		for k, v := range qa.pendingRemoteSync {
			update.Updates[k] = v
		}
		// Send to local coordinator
		qa.context.Tell(qa.localCoordinator, update)
		// Clear pending remote sync
		qa.pendingRemoteSync = make(map[string]ValueEntry)
	}
}
