package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strconv"
	"time"

	"github.com/suryatejb/bitcoin"
	"github.com/suryatejb/lsp"
)

/*
LOAD BALANCING STRATEGY: Shortest Remaining Time First (SRTF) with FIFO Tie-Breaking

This server implements a load balancing algorithm that optimizes for both efficiency
and fairness as required by the project specification.

EFFICIENCY:
The scheduler uses the Shortest Remaining Time First (SRTF) policy, which is proven
to be optimal for minimizing mean response time (as shown in the paper "A New Proof
of the Optimality of the Shortest Remaining Processing Time Discipline"). The key
insight is that prioritizing requests with less remaining work reduces the average
time all requests spend in the system.

Implementation:
- Each client request is partitioned into fixed-size chunks (10,000 nonces each)
- Jobs are enqueued as they arrive
- Before assignment, the job queue is sorted by the remaining work for each job's client
- Remaining work = TotalNonces - NoncesDone (total work minus completed work)
- Jobs belonging to clients with less remaining work are prioritized

Benefits:
- Large requests that arrive first won't block small requests that arrive later
- As large requests progress, their priority decreases, allowing other work to proceed
- Minimizes average response time across all client requests

FAIRNESS:
To ensure requests that arrive earlier have priority among requests with similar
remaining work, we use FIFO (First-In-First-Out) as a tie-breaker.

Implementation:
- Each client tracks ReceivedTime (timestamp when request arrived)
- When two jobs have equal remaining work, the job from the earlier request is chosen
- sort.SliceStable ensures stable sorting, preserving arrival order within equal groups

Benefits:
- Prevents starvation of early requests
- Among similar-sized requests, maintains arrival order
- Satisfies fairness requirement: requests of similar length finish approximately in order

WORK DISTRIBUTION:
- Jobs are assigned to any idle miner (first available)
- This minimizes miner idle time and keeps all workers busy
- Each miner processes one job at a time to avoid overwhelming any single miner

FAILURE HANDLING:
- If a miner disconnects, its in-flight job is requeued at the front for immediate reassignment
- If a client disconnects, all its pending jobs are removed from the queue
- Write failures to miners are treated as disconnections

This strategy balances the competing goals of efficiency (minimize mean response time)
and fairness (respect arrival order for similar workloads).
*/

const chunkSize = 10000

// Task represents a single mining task (chunk of work)
type Task struct {
	ClientConnID int
	Request      *bitcoin.Message
	NonceCount   uint64
}

// ClientState tracks a client's request and progress
type ClientState struct {
	ConnID          int
	WorkChunks      []*Task
	CompletedChunks int
	ProcessedNonces uint64
	LowestHash      uint64
	LowestNonce     uint64
	TotalNonces     uint64
	RequestTime     time.Time
}

// MinerState tracks a miner's connection and current work
type MinerState struct {
	ConnID     int
	ActiveTask *Task
}

type server struct {
	lspServer      lsp.Server
	minerRegistry  map[int]*MinerState
	clientRegistry map[int]*ClientState
	taskQueue      []*Task
}

// startServer creates and initializes a new Bitcoin mining server listening on the specified port.
func startServer(port int) (*server, error) {
	lspServer, err := lsp.NewServer(port, lsp.NewParams())
	if err != nil {
		return nil, err
	}

	srv := &server{
		lspServer:      lspServer,
		minerRegistry:  make(map[int]*MinerState),
		clientRegistry: make(map[int]*ClientState),
		taskQueue:      make([]*Task, 0),
	}

	return srv, nil
}

var LOGF *log.Logger

func main() {
	// You may need a logger for debug purpose
	const (
		name = "serverLog.txt"
		flag = os.O_RDWR | os.O_CREATE
		perm = os.FileMode(0666)
	)

	file, err := os.OpenFile(name, flag, perm)
	if err != nil {
		return
	}
	defer file.Close()

	LOGF = log.New(file, "", log.Lshortfile|log.Lmicroseconds)
	// Usage: LOGF.Println() or LOGF.Printf()

	const numArgs = 2
	if len(os.Args) != numArgs {
		fmt.Printf("Usage: ./%s <port>", os.Args[0])
		return
	}

	port, err := strconv.Atoi(os.Args[1])
	if err != nil {
		fmt.Println("Port must be a number:", err)
		return
	}

	srv, err := startServer(port)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	fmt.Println("Server listening on port", port)

	defer srv.lspServer.Close()

	// Main server loop
	for {
		connID, payload, err := srv.lspServer.Read()

		if err != nil {
			srv.handleDisconnect(connID)
		} else {
			srv.handleMessage(connID, payload)
		}

		srv.schedule()
	}
}

// handleMessage processes incoming Join, Request, and Result messages from clients and miners.
func (s *server) handleMessage(connID int, payload []byte) {
	var msg bitcoin.Message
	err := json.Unmarshal(payload, &msg)
	if err != nil {
		return
	}

	switch msg.Type {
	case bitcoin.Join:
		// Miner registration
		s.minerRegistry[connID] = &MinerState{
			ConnID:     connID,
			ActiveTask: nil,
		}

	case bitcoin.Request:
		// Client request - partition into chunks
		totalNonces := uint64(0)
		if msg.Upper >= msg.Lower {
			totalNonces = msg.Upper - msg.Lower + 1
		}

		client := &ClientState{
			ConnID:          connID,
			WorkChunks:      make([]*Task, 0),
			CompletedChunks: 0,
			ProcessedNonces: 0,
			LowestHash:      math.MaxUint64,
			LowestNonce:     0,
			TotalNonces:     totalNonces,
			RequestTime:     time.Now(),
		}
		s.clientRegistry[connID] = client

		// Handle empty range
		if totalNonces == 0 {
			resultMsg := bitcoin.NewResult(math.MaxUint64, msg.Lower)
			resultPayload, _ := json.Marshal(resultMsg)
			s.lspServer.Write(client.ConnID, resultPayload)
			delete(s.clientRegistry, client.ConnID)
			return
		}

		// Partition into chunks
		for lower := msg.Lower; lower <= msg.Upper; {
			upper := lower + chunkSize - 1
			if upper > msg.Upper {
				upper = msg.Upper
			}

			taskMsg := bitcoin.NewRequest(msg.Data, lower, upper)
			taskSize := upper - lower + 1
			task := &Task{
				ClientConnID: connID,
				Request:      taskMsg,
				NonceCount:   taskSize,
			}
			client.WorkChunks = append(client.WorkChunks, task)
			s.taskQueue = append(s.taskQueue, task)

			// Handle potential overflow
			if upper == math.MaxUint64 {
				break
			}
			lower = upper + 1
		}

	case bitcoin.Result:
		// Miner returning result
		miner, minerExists := s.minerRegistry[connID]
		if !minerExists || miner.ActiveTask == nil {
			return
		}

		task := miner.ActiveTask
		client, clientExists := s.clientRegistry[task.ClientConnID]
		if clientExists {
			client.CompletedChunks++
			client.ProcessedNonces += task.NonceCount

			if msg.Hash < client.LowestHash {
				client.LowestHash = msg.Hash
				client.LowestNonce = msg.Nonce
			}

			// Check if all jobs are complete
			if client.CompletedChunks == len(client.WorkChunks) {
				finalResult := bitcoin.NewResult(client.LowestHash, client.LowestNonce)
				finalPayload, _ := json.Marshal(finalResult)
				s.lspServer.Write(client.ConnID, finalPayload)
				delete(s.clientRegistry, client.ConnID)
			}
		}

		miner.ActiveTask = nil
	}
}

// handleDisconnect requeues in-flight miner work or removes pending client jobs on disconnection.
func (s *server) handleDisconnect(connID int) {
	// Check if it's a miner
	if miner, exists := s.minerRegistry[connID]; exists {
		// Requeue in-flight job
		if miner.ActiveTask != nil {
			// Add to front of queue for reassignment
			s.taskQueue = append([]*Task{miner.ActiveTask}, s.taskQueue...)
			miner.ActiveTask = nil
		}
		delete(s.minerRegistry, connID)
		return
	}

	// Check if it's a client
	if _, exists := s.clientRegistry[connID]; exists {
		// Remove client's jobs from queue
		newQueue := make([]*Task, 0)
		for _, task := range s.taskQueue {
			if task.ClientConnID != connID {
				newQueue = append(newQueue, task)
			}
		}
		s.taskQueue = newQueue
		delete(s.clientRegistry, connID)
		return
	}
}

// schedule assigns pending tasks to idle miners using SRTF with FIFO tie-breaking.
func (s *server) schedule() {
	if len(s.taskQueue) == 0 {
		return
	}

	// Sort job queue by SRTF: clients with least remaining work first
	// Tie-break by RequestTime (FIFO)
	sort.SliceStable(s.taskQueue, func(i, j int) bool {
		task1 := s.taskQueue[i]
		task2 := s.taskQueue[j]

		client1 := s.clientRegistry[task1.ClientConnID]
		client2 := s.clientRegistry[task2.ClientConnID]

		// Calculate remaining work
		remaining1 := uint64(math.MaxUint64)
		remaining2 := uint64(math.MaxUint64)

		if client1 != nil {
			if client1.TotalNonces > client1.ProcessedNonces {
				remaining1 = client1.TotalNonces - client1.ProcessedNonces
			} else {
				remaining1 = 0
			}
		}

		if client2 != nil {
			if client2.TotalNonces > client2.ProcessedNonces {
				remaining2 = client2.TotalNonces - client2.ProcessedNonces
			} else {
				remaining2 = 0
			}
		}

		// Compare remaining work
		if remaining1 != remaining2 {
			return remaining1 < remaining2
		}

		// Tie-break by RequestTime (earlier first)
		if client1 != nil && client2 != nil {
			return client1.RequestTime.Before(client2.RequestTime)
		}

		return false
	})

	// Assign jobs to idle miners
	for len(s.taskQueue) > 0 {
		// Find an idle miner
		var idleMiner *MinerState
		var idleMinerID int
		for id, miner := range s.minerRegistry {
			if miner.ActiveTask == nil {
				idleMiner = miner
				idleMinerID = id
				break
			}
		}

		if idleMiner == nil {
			// No idle miners available
			break
		}

		// Assign the first job to the idle miner
		task := s.taskQueue[0]
		s.taskQueue = s.taskQueue[1:]

		// Check if the client for this job still exists
		client := s.clientRegistry[task.ClientConnID]
		if client == nil {
			// Client disconnected, skip this job
			continue
		}

		idleMiner.ActiveTask = task
		taskPayload, err := json.Marshal(task.Request)
		if err != nil {
			idleMiner.ActiveTask = nil
			continue
		}

		err = s.lspServer.Write(idleMiner.ConnID, taskPayload)
		if err != nil {
			// Miner likely dead - remove and requeue job
			idleMiner.ActiveTask = nil
			delete(s.minerRegistry, idleMinerID)
			s.taskQueue = append([]*Task{task}, s.taskQueue...)
			continue
		}
	}
}
