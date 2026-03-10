//
// raft.go
// =======
// Raft consensus implementation.
//
// Implements leader election and log replication as described in:
// "In Search of an Understandable Consensus Algorithm" - Ongaro & Ousterhout
// https://raft.github.io/raft.pdf
//

package raft

import (
	"encoding/gob"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"

	"github.com/surya/rpc"
)

func init() {
	// Register concrete types used as log commands so gob can encode
	// interface{} fields when replicating log entries over RPC.
	gob.Register(int(0))
}

// Set to false to disable debug logs completely
const kEnableDebugLogs = false

// Set to true to log to stdout instead of log files
const kLogToStdout = true

// Directory for per-peer log files (used when kLogToStdout is false)
const kLogOutputDir = "./raftlogs/"

// ApplyCommand is delivered on applyCh whenever a log entry is committed.
type ApplyCommand struct {
	Index   int
	Command interface{}
}

// LogEntry holds a single record in the Raft log.
type LogEntry struct {
	Term    int
	Command interface{}
}

// Peer role constants.
const (
	follower = iota
	candidate
	leader
)

// Raft implements a single Raft peer.
type Raft struct {
	mux    sync.Mutex
	peers  []*rpc.ClientEnd
	me     int
	logger *log.Logger

	// Persistent state (would be written to stable storage in a production system)
	currentTerm int
	votedFor    int        // -1 means no vote cast in current term
	log         []LogEntry // log[0] is a sentinel entry with term 0

	// Volatile state on all servers
	commitIndex int
	lastApplied int

	// Volatile state on leaders -- reinitialized after each election
	nextIndex  []int
	matchIndex []int

	// Internal
	state        int
	applyCh      chan ApplyCommand
	stopCh       chan struct{}
	stopOnce     sync.Once
	resetTimerCh chan struct{}
}

// GetState returns this peer's index, current term, and whether it is the leader.
func (rf *Raft) GetState() (int, int, bool) {
	rf.mux.Lock()
	defer rf.mux.Unlock()
	return rf.me, rf.currentTerm, rf.state == leader
}

// lastLogInfo returns the index and term of the last log entry.
// Must be called with rf.mux held.
func (rf *Raft) lastLogInfo() (int, int) {
	n := len(rf.log) - 1
	return n, rf.log[n].Term
}

// --- RequestVote RPC ----------------------------------------------------------

// RequestVoteArgs holds the arguments for the RequestVote RPC.
// Field names must start with capital letters.
type RequestVoteArgs struct {
	Term         int
	CandidateId  int
	LastLogIndex int
	LastLogTerm  int
}

// RequestVoteReply holds the reply for the RequestVote RPC.
type RequestVoteReply struct {
	Term        int
	VoteGranted bool
}

// RequestVote handles an incoming RequestVote RPC.
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	rf.mux.Lock()
	defer rf.mux.Unlock()

	reply.Term = rf.currentTerm
	reply.VoteGranted = false

	if args.Term < rf.currentTerm {
		return
	}
	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.state = follower
		rf.votedFor = -1
	}
	reply.Term = rf.currentTerm

	lastIdx, lastTerm := rf.lastLogInfo()
	logOK := args.LastLogTerm > lastTerm ||
		(args.LastLogTerm == lastTerm && args.LastLogIndex >= lastIdx)

	if (rf.votedFor == -1 || rf.votedFor == args.CandidateId) && logOK {
		rf.votedFor = args.CandidateId
		reply.VoteGranted = true
		rf.nudgeTimer()
	}
}

// sendRequestVote sends a RequestVote RPC to the given peer.
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	return rf.peers[server].Call("Raft.RequestVote", args, reply)
}

// --- AppendEntries RPC --------------------------------------------------------

// AppendEntriesArgs holds the arguments for the AppendEntries RPC.
// Field names must start with capital letters.
type AppendEntriesArgs struct {
	Term         int
	LeaderId     int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogEntry
	LeaderCommit int
}

// AppendEntriesReply holds the reply for the AppendEntries RPC.
type AppendEntriesReply struct {
	Term    int
	Success bool
}

// AppendEntries handles an incoming AppendEntries RPC (also serves as heartbeat).
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mux.Lock()
	defer rf.mux.Unlock()

	reply.Term = rf.currentTerm
	reply.Success = false

	if args.Term < rf.currentTerm {
		return
	}
	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.votedFor = -1
	}
	rf.state = follower
	rf.nudgeTimer()
	reply.Term = rf.currentTerm

	// Log consistency check: ensure our log contains an entry at PrevLogIndex
	// with the expected term.
	if args.PrevLogIndex >= len(rf.log) {
		return
	}
	if rf.log[args.PrevLogIndex].Term != args.PrevLogTerm {
		return
	}

	// Reconcile log entries: delete any conflicting entries and append new ones.
	for i, entry := range args.Entries {
		idx := args.PrevLogIndex + 1 + i
		if idx >= len(rf.log) {
			rf.log = append(rf.log, args.Entries[i:]...)
			break
		}
		if rf.log[idx].Term != entry.Term {
			rf.log = rf.log[:idx]
			rf.log = append(rf.log, args.Entries[i:]...)
			break
		}
	}

	// Advance commit index up to min(leaderCommit, last log index).
	if args.LeaderCommit > rf.commitIndex {
		last := len(rf.log) - 1
		if args.LeaderCommit < last {
			rf.commitIndex = args.LeaderCommit
		} else {
			rf.commitIndex = last
		}
	}

	reply.Success = true
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	return rf.peers[server].Call("Raft.AppendEntries", args, reply)
}

// --- PutCommand ---------------------------------------------------------------

// PutCommand appends a new command to the log if this peer is the leader.
// Returns the log index, current term, and whether this peer is the leader.
func (rf *Raft) PutCommand(command interface{}) (int, int, bool) {
	rf.mux.Lock()
	defer rf.mux.Unlock()

	if rf.state != leader {
		return -1, rf.currentTerm, false
	}

	index := len(rf.log)
	term := rf.currentTerm
	rf.log = append(rf.log, LogEntry{Term: term, Command: command})
	rf.matchIndex[rf.me] = index
	rf.nextIndex[rf.me] = index + 1
	rf.logger.Printf("PutCommand index=%d term=%d", index, term)
	return index, term, true
}

// --- Stop ---------------------------------------------------------------------

// Stop shuts down this Raft peer.
func (rf *Raft) Stop() {
	rf.stopOnce.Do(func() { close(rf.stopCh) })
}

// --- Election timer -----------------------------------------------------------

// nudgeTimer signals the election timer to restart. Must be called with rf.mux held.
func (rf *Raft) nudgeTimer() {
	select {
	case rf.resetTimerCh <- struct{}{}:
	default:
	}
}

// electionTimeout returns a random election timeout between 250 and 450 ms.
func electionTimeout() time.Duration {
	return time.Duration(250+rand.Intn(200)) * time.Millisecond
}

// heartbeatInterval controls how often the leader sends AppendEntries.
const heartbeatInterval = 100 * time.Millisecond

// runElectionTimer drives the election process for followers and candidates.
func (rf *Raft) runElectionTimer() {
	for {
		timeout := electionTimeout()
		select {
		case <-rf.stopCh:
			return
		case <-rf.resetTimerCh:
		// Heartbeat received or vote granted -- restart the clock.
		case <-time.After(timeout):
			rf.mux.Lock()
			if rf.state != leader {
				rf.startElection()
			}
			rf.mux.Unlock()
		}
	}
}

// startElection begins a new election from this peer.
// Must be called with rf.mux held.
func (rf *Raft) startElection() {
	rf.state = candidate
	rf.currentTerm++
	rf.votedFor = rf.me
	term := rf.currentTerm
	lastIdx, lastTerm := rf.lastLogInfo()
	rf.logger.Printf("election term=%d", term)

	majority := len(rf.peers)/2 + 1
	var mu sync.Mutex
	votes := 1
	won := false

	for i := range rf.peers {
		if i == rf.me {
			continue
		}
		go func(peer int) {
			args := &RequestVoteArgs{
				Term:         term,
				CandidateId:  rf.me,
				LastLogIndex: lastIdx,
				LastLogTerm:  lastTerm,
			}
			reply := &RequestVoteReply{}
			if !rf.sendRequestVote(peer, args, reply) {
				return
			}

			rf.mux.Lock()
			defer rf.mux.Unlock()

			if reply.Term > rf.currentTerm {
				rf.currentTerm = reply.Term
				rf.state = follower
				rf.votedFor = -1
				return
			}
			if rf.state != candidate || rf.currentTerm != term {
				return
			}
			if !reply.VoteGranted {
				return
			}

			mu.Lock()
			votes++
			v := votes
			mu.Unlock()

			if !won && v >= majority {
				won = true
				rf.becomeLeader()
			}
		}(i)
	}
}

// becomeLeader transitions this peer to leader and starts sending heartbeats.
// Must be called with rf.mux held.
func (rf *Raft) becomeLeader() {
	rf.state = leader
	rf.logger.Printf("leader term=%d", rf.currentTerm)

	rf.nextIndex = make([]int, len(rf.peers))
	rf.matchIndex = make([]int, len(rf.peers))
	nextIdx := len(rf.log)
	for i := range rf.nextIndex {
		rf.nextIndex[i] = nextIdx
	}
	rf.matchIndex[rf.me] = nextIdx - 1

	go rf.runHeartbeats()
}

// --- Log replication ----------------------------------------------------------

// runHeartbeats periodically replicates log entries to all peers while leader.
func (rf *Raft) runHeartbeats() {
	for {
		select {
		case <-rf.stopCh:
			return
		case <-time.After(heartbeatInterval):
			rf.mux.Lock()
			if rf.state != leader {
				rf.mux.Unlock()
				return
			}
			rf.broadcastAppendEntries()
			rf.mux.Unlock()
		}
	}
}

// broadcastAppendEntries sends AppendEntries to every peer.
// Must be called with rf.mux held.
func (rf *Raft) broadcastAppendEntries() {
	term := rf.currentTerm
	for i := range rf.peers {
		if i == rf.me {
			continue
		}
		go rf.replicateToPeer(i, term)
	}
}

// replicateToPeer sends an AppendEntries RPC to a single peer and processes
// the reply, advancing nextIndex/matchIndex on success or backing off on failure.
func (rf *Raft) replicateToPeer(peer, term int) {
	rf.mux.Lock()
	if rf.state != leader || rf.currentTerm != term {
		rf.mux.Unlock()
		return
	}
	ni := rf.nextIndex[peer]
	prevIdx := ni - 1
	prevTerm := rf.log[prevIdx].Term
	entries := make([]LogEntry, len(rf.log[ni:]))
	copy(entries, rf.log[ni:])
	args := &AppendEntriesArgs{
		Term:         term,
		LeaderId:     rf.me,
		PrevLogIndex: prevIdx,
		PrevLogTerm:  prevTerm,
		Entries:      entries,
		LeaderCommit: rf.commitIndex,
	}
	rf.mux.Unlock()

	reply := &AppendEntriesReply{}
	if !rf.sendAppendEntries(peer, args, reply) {
		return
	}

	rf.mux.Lock()
	defer rf.mux.Unlock()

	if reply.Term > rf.currentTerm {
		rf.currentTerm = reply.Term
		rf.state = follower
		rf.votedFor = -1
		return
	}
	if rf.state != leader || rf.currentTerm != term {
		return
	}

	if reply.Success {
		matched := prevIdx + len(entries)
		if matched > rf.matchIndex[peer] {
			rf.matchIndex[peer] = matched
			rf.nextIndex[peer] = matched + 1
		}
		rf.maybeAdvanceCommit()
	} else {
		if rf.nextIndex[peer] > 1 {
			rf.nextIndex[peer]--
		}
	}
}

// maybeAdvanceCommit checks whether commitIndex can be advanced.
// Per the Raft paper, an entry is committed once a majority of peers have
// replicated it AND it belongs to the current term. Older entries are
// committed implicitly when a current-term entry is committed.
// Must be called with rf.mux held.
func (rf *Raft) maybeAdvanceCommit() {
	majority := len(rf.peers)/2 + 1
	for n := len(rf.log) - 1; n > rf.commitIndex; n-- {
		if rf.log[n].Term != rf.currentTerm {
			break
		}
		count := 0
		for i := range rf.peers {
			if rf.matchIndex[i] >= n {
				count++
			}
		}
		if count >= majority {
			rf.commitIndex = n
			break
		}
	}
}

// --- Apply goroutine ----------------------------------------------------------

// runApplier watches commitIndex and delivers committed entries to applyCh in order.
func (rf *Raft) runApplier() {
	for {
		select {
		case <-rf.stopCh:
			return
		case <-time.After(10 * time.Millisecond):
		}

		rf.mux.Lock()
		for rf.lastApplied < rf.commitIndex {
			rf.lastApplied++
			entry := rf.log[rf.lastApplied]
			idx := rf.lastApplied
			rf.mux.Unlock()
			rf.applyCh <- ApplyCommand{Index: idx, Command: entry.Command}
			rf.mux.Lock()
		}
		rf.mux.Unlock()
	}
}

// --- NewPeer ------------------------------------------------------------------

// NewPeer creates and starts a new Raft peer.
func NewPeer(peers []*rpc.ClientEnd, me int, applyCh chan ApplyCommand) *Raft {
	rf := &Raft{
		peers:        peers,
		me:           me,
		applyCh:      applyCh,
		stopCh:       make(chan struct{}),
		resetTimerCh: make(chan struct{}, 1),
		currentTerm:  0,
		votedFor:     -1,
		log:          []LogEntry{{Term: 0}}, // sentinel entry at index 0
		commitIndex:  0,
		lastApplied:  0,
		state:        follower,
	}

	if kEnableDebugLogs {
		if kLogToStdout {
			rf.logger = log.New(os.Stdout,
				fmt.Sprintf("[%d] ", me), log.Lmicroseconds|log.Lshortfile)
		} else {
			if err := os.MkdirAll(kLogOutputDir, os.ModePerm); err != nil {
				panic(err)
			}
			f, err := os.OpenFile(
				fmt.Sprintf("%s/peer-%d.txt", kLogOutputDir, me),
				os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
			if err != nil {
				panic(err)
			}
			rf.logger = log.New(f,
				fmt.Sprintf("[%d] ", me), log.Lmicroseconds|log.Lshortfile)
		}
	} else {
		rf.logger = log.New(io.Discard, "", 0)
	}

	go rf.runElectionTimer()
	go rf.runApplier()
	return rf
}
