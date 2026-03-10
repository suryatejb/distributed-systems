#!/bin/bash

# TestClient
go test -race -cpu 4 -timeout 10s -run=TestClientGet
go test -race -cpu 4 -timeout 10s -run=TestClientList
go test -race -cpu 4 -timeout 10s -run=TestClientPut
go test -race -cpu 4 -timeout 10s -run=TestClientBalance
go test -race -cpu 4 -timeout 10s -run=TestClientSequential
go test -race -cpu 4 -timeout 10s -run=TestClientConcurrent

# TestMailbox
go test -race -cpu 4 -timeout 10s -run=TestMailboxBasic1
go test -race -cpu 4 -timeout 10s -run=TestMailboxBasic2
go test -race -cpu 4 -timeout 10s -run=TestMailboxBasic3

go test -race -cpu 4 -timeout 10s -run=TestMailboxPushBlock
go test -race -cpu 4 -timeout 10s -run=TestMailboxPopBlock
go test -race -cpu 4 -timeout 10s -run=TestMailboxClosePush
go test -race -cpu 4 -timeout 10s -run=TestMailboxMultiple

# TestOneActor
go test -race -cpu 4 -timeout 15s -run=TestOneActorGet
go test -race -cpu 4 -timeout 15s -run=TestOneActorPut1
go test -race -cpu 4 -timeout 15s -run=TestOneActorPut2
go test -race -cpu 4 -timeout 15s -run=TestOneActorList1
go test -race -cpu 4 -timeout 15s -run=TestOneActorList2
go test -race -cpu 4 -timeout 15s -run=TestOneActorTrace

go test -race -cpu 4 -timeout 15s -run=TestOneActorMultiClient1
go test -race -cpu 4 -timeout 15s -run=TestOneActorMultiClient2
