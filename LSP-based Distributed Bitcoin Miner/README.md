# distributed-bitcoin-miner

A distributed Bitcoin mining system built on top of a custom reliable transport protocol implemented over UDP in Go. The project is split into two layers: a reliable messaging protocol (LSP) and a distributed compute system that uses it to coordinate Bitcoin hash mining across multiple workers.

## Overview

### Part A  Layered Service Protocol (LSP)

LSP is a custom application-level protocol that provides reliable, ordered message delivery over UDP. It is implemented from scratch in `src/github.com/suryatejb/lsp/`.

**Key features:**

- **Connection management**  clients connect to the server using a randomized initial sequence number (ISN); the server assigns unique connection IDs
- **Ordered delivery**  messages are buffered and delivered in sequence order even if UDP delivers them out of order
- **Sliding window flow control**  limits the number of unacknowledged in-flight messages; configurable via `WindowSize` and `MaxUnackedMessages`
- **Automatic retransmission with exponential backoff**  unacknowledged messages are retransmitted at increasing intervals up to a configurable `MaxBackOffInterval`
- **Epoch-based heartbeats**  both client and server send heartbeat ACKs each epoch when idle, so the other side can detect connection loss
- **Connection loss detection**  if no message is received for `EpochLimit` consecutive epochs, the connection is declared lost and a read error is surfaced
- **Cumulative ACKs (CAck)**  reduces ACK traffic by acknowledging all messages up to a given sequence number at once
- **Checksum validation**  each data message carries a checksum; corrupt messages are silently dropped and retransmitted

**Architecture:**

Each server and client runs a central `dispatcher` goroutine that owns all mutable state. Network I/O, epoch ticks, and application API calls each communicate with the dispatcher via typed channels. This design avoids shared-memory data races entirely.

```
Application (Read/Write/Close)
        |
        v
   dispatcher <--- networkReader (UDP read loop)
        |       <--- epochManager (periodic ticks)
        |       <--- writeSequencer (ordered write IDs)
        v
    UDP socket
```

### Part B  Distributed Bitcoin Miner

The Bitcoin miner uses the LSP layer to distribute SHA-256 hash computation across a pool of miners coordinated by a central server.

**Components:**

| Binary | Role |
|--------|------|
| `server` | Receives mining requests from clients, splits the nonce search space, and farms work to available miners |
| `miner`  | Connects to the server, receives work chunks, computes hashes, and returns the result with the minimum hash found |
| `client` | Sends a mining request (message + max nonce) to the server and waits for the result |

**Workflow:**

```
Client  --request(msg, maxNonce)-->  Server  --chunk(msg, lo, hi)-->  Miner(s)
Client  <--result(nonce, hash)------  Server  <--result(nonce, hash)--  Miner(s)
```

## Project Structure

```
src/github.com/suryatejb/
 lsp/            # LSP protocol implementation (server + client)
 lspnet/         # UDP network abstraction with simulated packet loss/delay
 bitcoin/
    hash.go     # SHA-256 hashing utility
    message.go  # Request/Result message types
    client/     # Bitcoin mining client
    miner/      # Bitcoin mining worker
    server/     # Mining coordination server
 srunner/        # Echo server for LSP testing
 crunner/        # Echo client for LSP testing
 go.mod
```

## Running the LSP Tests

Navigate to the LSP package and run individual tests:

```bash
cd src/github.com/suryatejb/lsp

# Basic connectivity
go test -run=TestBasic1 -timeout=5s -race

# Sliding window
go test -run=TestWindow1 -timeout=5s

# Exponential backoff
go test -run=TestExpBackOff1 -timeout=60s -race

# Run all tests matching a prefix
go test -run=TestBasic -timeout=20s
```

To run the full checkpoint test suites (from the repo root):

```powershell
# Checkpoint 1 (basic + out-of-order)
.\sh\windows\run_test_checkpoint1.ps1

# Checkpoint 2 (all 61 tests)
.\sh\windows\run_test_checkpoint2.ps1
```

## Running the Bitcoin Miner

Build all binaries:

```bash
cd src/github.com/suryatejb
go install ./bitcoin/server ./bitcoin/miner ./bitcoin/client
```

Start the system:

```bash
# Terminal 1: start the server on port 6060
server 6060

# Terminal 2: start one or more miners
miner localhost:6060

# Terminal 3: submit a mining job
client localhost:6060 "hello world" 999999
```

The client will print the nonce and hash that produced the minimum SHA-256 result over the search space `[0, maxNonce]`.

## Testing with Echo Runner

`srunner` and `crunner` are simple echo server/client programs useful for manually testing the LSP layer:

```bash
# Terminal 1
go run src/github.com/suryatejb/srunner/srunner.go -port=9999 -v

# Terminal 2
go run src/github.com/suryatejb/crunner/crunner.go -port=9999
```

Available flags for both runners:

```
-elim=5               epoch limit
-ems=2000             epoch duration (ms)
-port=9999            port number
-rdrop=0              network read drop percent
-wdrop=0              network write drop percent
-wsize=1              window size
-maxUnackMessages=1   maximum unacknowledged messages allowed
-maxBackoff=0         maximum backoff interval (epochs)
-v=false              verbose logging
```

Pre-compiled reference binaries (`srunner_sols`, `crunner_sols`) are available in `bin/` for each platform  useful for testing one side in isolation.

## Configuration Parameters

LSP behaviour is controlled via `lsp.Params`:

| Parameter | Description |
|-----------|-------------|
| `EpochMillis` | Duration of each epoch in milliseconds |
| `EpochLimit` | Number of silent epochs before declaring a connection lost |
| `WindowSize` | Maximum number of in-flight unacknowledged messages |
| `MaxUnackedMessages` | Hard cap on unacknowledged messages (0 = unlimited) |
| `MaxBackOffInterval` | Maximum retransmission backoff in epochs (0 = unlimited) |

## Reading the API Docs

```bash
go install golang.org/x/tools/cmd/godoc@latest
cd src/github.com/suryatejb
godoc -http=:6060
# Open http://localhost:6060/pkg/github.com/suryatejb in a browser
```
