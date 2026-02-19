# Key-Value Messaging System & Testing - Go Concurrency

A distributed systems project demonstrating advanced Go concurrency patterns through a multi-client key-value database server and concurrent data processing pipeline.

## 📋 Table of Contents

- [Overview](#overview)
- [Project Structure](#project-structure)
- [Part A: Key-Value Messaging System](#part-a-key-value-messaging-system)
- [Part B: Concurrent Squarer Testing](#part-b-concurrent-squarer-testing)
- [Architecture & Design](#architecture--design)
- [Concurrency Patterns](#concurrency-patterns)
- [Features](#features)
- [Setup & Installation](#setup--installation)
- [Running Tests](#running-tests)
- [Usage Examples](#usage-examples)

---

## 🎯 Overview

This project implements a **concurrent key-value messaging system** in Go, showcasing:

- **Multi-client TCP server** supporting concurrent connections
- **Thread-safe database operations** using channels and goroutines
- **Backpressure handling** for slow clients
- **Graceful shutdown** with proper resource cleanup
- **Concurrent data processing** pipeline with unit testing

### Technologies Used
- **Language**: Go 1.25+
- **Concurrency**: Goroutines, Channels
- **Networking**: TCP sockets (net package)
- **Testing**: Go's built-in testing framework

---

## 📁 Project Structure

```
├── src/github.com/cmu440/
│   ├── p0partA/                  # Key-Value Server Implementation
│   │   ├── server_api.go         # Server interface definition
│   │   ├── server_impl.go        # Server implementation with concurrency
│   │   ├── server_test.go        # Comprehensive server tests
│   │   └── kvstore/
│   │       ├── kv_api.go         # Key-Value store interface
│   │       └── kv_impl.go        # In-memory KV store implementation
│   │
│   ├── p0partB/                  # Concurrent Squarer Testing
│   │   ├── squarer_api.go        # Squarer interface
│   │   ├── squarer_impl.go       # Squarer implementation
│   │   └── squarer_test.go       # Custom test suite
│   │
│   ├── srunner/                  # Server runner utility
│   │   └── srunner.go            # Standalone server launcher
│   │
│   └── crunner/                  # Client runner utility (template)
│       └── crunner.go
```

---

## 🗄️ Part A: Key-Value Messaging System

### Overview

A **concurrent TCP server** that manages multiple client connections and provides thread-safe access to a centralized key-value database. The server handles thousands of concurrent operations while maintaining data consistency.

### Features

#### 1. **Multi-Client Support**
- Accepts unlimited concurrent TCP connections
- Each client handled by dedicated goroutines (reader + writer)
- Independent message processing per client

#### 2. **Database Operations**
Supports four atomic operations via text-based protocol:

| Operation | Format | Description |
|-----------|--------|-------------|
| **Put** | `Put:key:value` | Insert or append value to key |
| **Get** | `Get:key` | Retrieve all values for key |
| **Delete** | `Delete:key` | Remove all values for key |
| **Update** | `Update:key:oldValue:newValue` | Replace old value with new |

#### 3. **Slow Client Handling**
- **Buffered output queues** (500 messages per client)
- **Automatic dropping** of messages when client can't keep up
- Prevents one slow client from blocking others

#### 4. **Connection Management**
- Tracks active client count in real-time
- Counts dropped clients
- Clean disconnect handling

#### 5. **Graceful Shutdown**
- Closes all client connections
- Terminates all goroutines properly
- No goroutine leaks or deadlocks

### Architecture

```
┌──────────────────────────────────────────────────────────┐
│                   KeyValueServer                         │
├──────────────────────────────────────────────────────────┤
│                                                          │
│  ┌────────────┐    ┌─────────────┐    ┌──────────────┐ │
│  │  Accept    │───>│    Main     │<───│  Count       │ │
│  │  Routine   │    │   Routine   │    │  Queries     │ │
│  └────────────┘    └─────────────┘    └──────────────┘ │
│         │                 │                             │
│         v                 v                             │
│  ┌────────────────────────────────────┐                │
│  │     Client Connections (Map)       │                │
│  └────────────────────────────────────┘                │
│         │                                               │
│         v                                               │
│  ┌─────────────┐  ┌─────────────┐                     │
│  │   Read      │  │   Write     │  (per client)       │
│  │   Routine   │  │   Routine   │                     │
│  └─────────────┘  └─────────────┘                     │
│                                                          │
│  ┌────────────────────────────────────┐                │
│  │         KVStore Backend            │                │
│  │   (Thread-safe via channels)       │                │
│  └────────────────────────────────────┘                │
└──────────────────────────────────────────────────────────┘
```

### Implementation Highlights

**Channel-Based Synchronization:**
```go
addClient    chan net.Conn      // New connections
removeClient chan net.Conn      // Disconnections
dbRequest    chan *dbOperation  // Database ops
countRequest chan *countQuery   // Stat queries
shutdown     chan struct{}      // Shutdown signal
```

**Per-Client Goroutines:**
- `readRoutine()`: Parses incoming commands, submits to DB
- `writeRoutine()`: Sends queued responses to client

**Central Coordinator:**
- `mainRoutine()`: Single point of synchronization
- Serializes all database operations (prevents race conditions)
- Manages client lifecycle

---

## 🧮 Part B: Concurrent Squarer Testing

### Overview

A **concurrent data processing pipeline** that squares integers from an input channel. Demonstrates proper goroutine lifecycle management and channel-based communication.

### Features

- **Non-blocking initialization**: Returns immediately with output channel
- **Sequential processing**: Maintains input order in output
- **Clean shutdown**: Ensures all goroutines terminate on `Close()`
- **No resource leaks**: Proper cleanup verified by tests

### Test Suite

Custom tests verify:

1. **Basic Correctness**: Input → Square → Output
2. **Sequential Processing**: Order preservation (1,2,3 → 1,4,9)
3. **Blocking After Close**: Input channel blocks post-cleanup

### Implementation Pattern

Uses a state machine with channel multiplexing:

```go
func (sq *SquarerImpl) work() {
    var toPush int
    dummy := make(chan int)
    pushOn := dummy      // Initially disabled
    pullOn := sq.input   // Initially enabled
    
    for {
        select {
        case unsquared := <-pullOn:
            toPush = unsquared * unsquared
            pushOn = sq.output  // Enable push
            pullOn = nil        // Disable pull
        case pushOn <- toPush:
            pushOn = dummy      // Disable push
            pullOn = sq.input   // Enable pull
        case <-sq.close:
            sq.closed <- true
            return
        }
    }
}
```

This ensures:
- Only one value processed at a time
- No dropped inputs while pushing output
- Deterministic ordering

---

## 🏗️ Architecture & Design

### Concurrency Model

The project follows **Communicating Sequential Processes (CSP)** principles:

> _"Don't communicate by sharing memory; share memory by communicating."_

All shared state is accessed exclusively through channels, eliminating traditional locking.

### Design Patterns

1. **Producer-Consumer**: Accept routine → Main routine
2. **Fan-Out**: One main routine → Many client goroutines
3. **Worker Pool**: Multiple client readers → Single DB coordinator
4. **Pipeline**: Input channel → Squarer → Output channel

### Race Condition Prevention

✅ **Single writer** to shared state (main routine)  
✅ **Channel-based coordination** (no mutexes needed)  
✅ **Immutable messages** between goroutines  
✅ **Tested with** `go test -race`

---

## ✨ Features

### Core Capabilities

- [x] **Concurrent client handling** (tested with 100+ simultaneous clients)
- [x] **Thread-safe database operations** (Put, Get, Delete, Update)
- [x] **Backpressure management** (buffered queues + message dropping)
- [x] **Clean shutdown** (no goroutine leaks)
- [x] **Comprehensive testing** (unit + integration + race detection)

### Production-Ready Qualities

- **Error handling**: Graceful degradation on client disconnect
- **Resource limits**: Per-client queue size limits
- **Monitoring**: Real-time active/dropped client metrics
- **Testability**: Extensive test coverage with edge cases

---

## 🚀 Setup & Installation

### Prerequisites

- **Go 1.25+** ([Download](https://go.dev/dl/))
- **Linux/WSL** (Part A tests use Linux-specific syscalls)

### Windows Users

Install **Windows Subsystem for Linux (WSL 2)**:

```powershell
wsl --install
```

Then install Go in WSL:

```bash
# In WSL terminal
cd /tmp
wget https://go.dev/dl/go1.25.0.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.25.0.linux-amd64.tar.gz

# Add to PATH
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc

# Verify installation
go version
```

---

## 🧪 Running Tests

### Part A: Server Tests

```bash
cd src/github.com/cmu440/p0partA

# Run all tests
go test

# With race detector
go test -race

# Run specific test
go test -race -test.run TestBasic1

# Verbose output
go test -v
```

**Test Coverage:**
- `TestBasic1-6`: Varying client counts and operation volumes
- `TestCount1-2`: Client connection/disconnection scenarios
- `TestSlowClient1-2`: Slow client drop/backpressure behavior
- Race condition detection across all tests

### Part B: Squarer Tests

```bash
cd src/github.com/cmu440/p0partB

# Run all tests
go test

# Verbose output
go test -v
```

**Custom Tests:**
- ✅ Basic correctness (2 → 4)
- ✅ Sequential processing (order preservation)
- ✅ Blocking after close (proper cleanup)

---

## 💡 Usage Examples

### Running the Server Manually

```bash
# Build server runner
cd src/github.com/cmu440
go install github.com/suryatejb/distributed-systems/srunner

# Start server (default port 9999)
$HOME/go/bin/srunner
```

### Testing with Netcat

```bash
# In another terminal
nc localhost 9999

# Send commands
Put:greeting:Hello World
Put:greeting:Hi there
Get:greeting
# Server responds:
# greeting:Hello World
# greeting:Hi there

Delete:greeting
Get:greeting
# (no response - key deleted)
```

### Protocol Examples

```
# Insert value
> Put:user:alice
< (no response)

# Retrieve value(s)
> Get:user
< user:alice

# Update value
> Put:user:bob
> Update:user:alice:charlie
> Get:user  
< user:charlie
< user:bob

# Delete key
> Delete:user
```

---

## 📊 Resource Characteristics

### Goroutine Model
- **2 goroutines per client**: one reader, one writer
- **2 server-level goroutines**: accept routine + main coordinator

### Memory
- **Per-client queue**: up to 500 buffered messages (`maxQueueSize = 500`)
- **KV store**: in-memory `map[string][][]byte` — unbounded

### File Descriptors
- **1 TCP connection per client** — bounded by OS limits (tested via `syscall.Getrlimit`)

---

## 🔒 Thread Safety

All operations are **thread-safe** without explicit locking:

1. **Database access** serialized through `dbRequest` channel
2. **Client map** modified only by main routine
3. **Counters** updated only in main routine
4. **Per-client state** owned by respective goroutines

Verified with `go test -race` (no data races detected).

---

## 🛠️ Development Tools

### Server Runner (`srunner`)

Launch a standalone server instance:

```bash
go install github.com/suryatejb/distributed-systems/srunner
$HOME/go/bin/srunner
```

---

## 🎓 Skills Demonstrated

This project demonstrates proficiency in:

1. **Goroutine management**: Spawning, coordinating, and terminating
2. **Channel patterns**: Buffered/unbuffered, select, timeouts
3. **Network programming**: TCP listeners, connection handling
4. **Concurrency debugging**: Race detection, deadlock prevention
5. **Test-driven development**: Unit tests, integration tests, property tests

---

## 📝 License

MIT License

---

## 🤝 Contributing

Feel free to open issues or pull requests for improvements and suggestions.

---

## ✅ Testing Checklist

- [x] All `TestBasic*` tests pass
- [x] All `TestCount*` tests pass
- [x] All `TestSlowClient*` tests pass
- [x] All `Squarer` tests pass
- [x] No race conditions (`go test -race`)
- [x] No goroutine leaks
- [x] Clean shutdown works correctly
- [x] Handles slow clients gracefully
- [x] Concurrent operations maintain data consistency

---

**Built with ❤️ using Go's powerful concurrency primitives**
