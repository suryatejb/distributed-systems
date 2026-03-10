# go-distributed-kv-actor

A distributed, eventually consistent key-value store built in Go, using an **actor model** for concurrent message passing. The system supports multiple query actors per server, remote actor communication across machines, and last-writer-wins (LWW) conflict resolution for synchronization across nodes.

## Architecture

```
src/github.com/suryatejb/
  actor/       Actor system: mailbox (FIFO message queue), actor context, remote tell (RPC-based inter-node messaging)
  app/         CMUD game client — a multi-user dungeon demo that exercises the key-value store
  crunner/     Simple interactive kvclient runner
  example/     "Counter actor" example demonstrating the actor package
  kvclient/    Key-value store client library
  kvcommon/    Shared RPC types between kvclient and kvserver
  kvserver/    Key-value store server: query actors, sync coordinator, eventually consistent replication
  srunner/     Simple kvserver runner
  staff/       Network condition utilities (latency injection) used in tests
  tests/       Integration and unit tests
```

### Key Components

- **Actor system** (`actor/`): A lightweight actor runtime. Each actor has a private mailbox (thread-safe unbounded FIFO queue). Actors communicate only by sending messages — no shared memory. Remote actors on other machines are reached via RPC (`remote_tell.go`).
- **Key-value server** (`kvserver/`): Each server runs _N_ query actors (one per CPU core is typical). A `SyncCoordinator` actor handles incoming sync messages from remote servers and broadcasts updates to local query actors. Replication follows an eventually consistent, last-writer-wins strategy using logical timestamps.
- **Key-value client** (`kvclient/`): Connects to a query actor's RPC port and issues `Get`, `Put`, and `List` operations.
- **CMUD** (`app/`): A text-based multi-user dungeon game that uses the key-value store as its backend, useful for end-to-end testing.

## Getting Started

### Prerequisites

- Go 1.21+

### Running a server

Navigate to `src/github.com/suryatejb/srunner` and start a server with one query actor on the default port (6001):

```bash
go run srunner.go
```

Start a server with multiple query actors:

```bash
go run srunner.go -count 4 -port 6000
```

Available flags:

```
Usage:
    srunner [options] <existing server descs...>
where options are:
  -count int
        request actor count (default 1)
  -port int
        starting port number (default 6000)
```

### Running a client

Navigate to `src/github.com/suryatejb/crunner` and connect to a query actor:

```bash
go run crunner.go localhost:6001
```

### Playing CMUD

Start a server with 5 actors:

```bash
# in src/github.com/suryatejb/srunner
go run srunner.go -count 5
```

Then launch the game client:

```bash
# in src/github.com/suryatejb/app
go run cmud.go localhost:6001
```

### Running the counter actor example

Requires `actor/mailbox.go` to be implemented. Then in `src/github.com/suryatejb/example/`:

```bash
go run .
```

## Testing

All tests are in `src/github.com/suryatejb/tests/`. Run individual tests with:

```bash
go test -race -cpu 4 -timeout <timeout> -run=<TestName>
```

For example:

```bash
go test -race -cpu 4 -timeout 15s -run=TestOneActorGet
go test -race -cpu 4 -timeout 15s -run=TestLocalSyncBasic1
go test -race -cpu 4 -timeout 20s -run=TestRemoteSyncBasic1
```

Two convenience shell scripts are provided:

- `tests/checkpoint.sh` — runs the core client, mailbox, and single-actor tests
- `tests/final.sh` — runs the full test suite including remote tell, local sync, and remote sync tests

## API Documentation

Install `godoc`, then inside `src/github.com/suryatejb/`:

```bash
godoc -http=:5050
```

Navigate to [localhost:5050/pkg/github.com/surya](http://localhost:5050/pkg/github.com/surya).

## Design Notes

- **Mailbox**: A mutex + condition variable backed unbounded queue. `Push` is non-blocking; `Pop` blocks until a message is available or the mailbox is closed.
- **Remote tell**: Uses Go's `net/rpc` with async `client.Go()` calls so the sender does not block waiting for acknowledgment, while still guaranteeing in-order delivery per destination.
- **Sync coordinator**: Each server runs a single coordinator actor that receives sync payloads from remote servers and fans them out to local query actors, reducing cross-node coupling.
- **Last-writer-wins**: Each `Put` carries a logical timestamp. When two replicas disagree on a value, the higher timestamp wins, ensuring eventual consistency without coordination overhead.

