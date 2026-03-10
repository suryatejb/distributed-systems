# Raft Consensus — Go

A personal implementation of the [Raft distributed consensus algorithm](https://raft.github.io/raft.pdf) in Go. Raft is a leader-based consensus protocol designed to be understandable. It handles leader election and log replication across a cluster of nodes, providing fault tolerance as long as a majority of nodes remain available.

## Project Structure

```
src/github.com/surya/
  raft/       Raft implementation and tests
  rpc/        Channel-based RPC library (simulated network with packet loss/delay)
```

## Implementation

### Part 1 — Leader Election

- Peers start as **followers** and transition to **candidate** when an election timeout fires
- A candidate requests votes from all peers via `RequestVote` RPCs
- A peer wins the election if it receives votes from a majority and becomes **leader**
- Leaders send periodic heartbeat `AppendEntries` RPCs to prevent new elections
- If a leader or candidate discovers a higher term, it reverts to follower

### Part 2 — Log Replication

- Clients submit commands via `PutCommand`; only the leader accepts them
- The leader appends the entry to its log and replicates it via `AppendEntries` RPCs
- An entry is **committed** once a majority of peers have written it to their log
- Each peer delivers committed entries in order through the `applyCh` channel as `ApplyCommand` messages
- Log consistency is enforced: followers reject entries whose preceding log term/index does not match

## API

| Symbol | Description |
|---|---|
| `NewPeer(peers, me, applyCh)` | Create and start a Raft peer |
| `rf.PutCommand(command)` | Append a command to the log; returns `(index, term, isLeader)` |
| `rf.GetState()` | Returns `(me, term, isLeader)` |
| `rf.Stop()` | Shut down the peer |
| `ApplyCommand{Index, Command}` | Delivered on `applyCh` when an entry is committed |

## Getting Started

### Prerequisites

- Go 1.18+

### Running the Tests

From the `src/github.com/surya/raft/` directory:

```sh
# Run all tests
go test

# Run only leader election tests
go test -run Election

# Run only log replication tests
go test -run Agree

# Run with race detector
go test -race
```

### Test Coverage

| Test | What it checks |
|---|---|
| `TestInitialElection` | A single leader is elected in a 3-node cluster |
| `TestReElection` | A new leader is elected after the current leader disconnects |
| `TestBasicAgree` | Commands are committed when all 5 nodes are connected |
| `TestFailAgree` | Agreement continues with one disconnected follower; log catches up on reconnect |
| `TestFailNoAgree` | No commit happens when fewer than a majority of nodes are reachable |
| `TestConcurrentPutCommands` | Concurrent `PutCommand` calls all appear in the committed log |

## Logging

Debug logging is controlled by constants at the top of `raft.go`:

```go
const kEnableDebugLogs = true   // set false to silence all logs
const kLogToStdout    = true    // set false to write per-peer log files under ./raftlogs/
```

## References

- [Raft paper — In Search of an Understandable Consensus Algorithm](https://raft.github.io/raft.pdf)
- [The Secret Lives of Data — Raft visualization](http://thesecretlivesofdata.com/raft/)
- [Raft author's website](https://raft.github.io/)

### Browsing docs locally

```sh
go install golang.org/x/tools/cmd/godoc@latest
# run from inside src/github.com/surya/
godoc -http=:6060
# open http://localhost:6060/pkg/github.com/surya
```