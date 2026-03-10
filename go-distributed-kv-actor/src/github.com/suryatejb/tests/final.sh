#!/bin/bash

# TestOneActor
go test -race -cpu 4 -timeout 15s -run=TestOneActorGet
go test -race -cpu 4 -timeout 15s -run=TestOneActorPut1
go test -race -cpu 4 -timeout 15s -run=TestOneActorPut2
go test -race -cpu 4 -timeout 15s -run=TestOneActorList1
go test -race -cpu 4 -timeout 15s -run=TestOneActorList2
go test -race -cpu 4 -timeout 15s -run=TestOneActorTrace

go test -race -cpu 4 -timeout 15s -run=TestOneActorMultiClient1
go test -race -cpu 4 -timeout 15s -run=TestOneActorMultiClient2

# TestRemoteTell
go test -race -cpu 4 -timeout 10s -run=TestRemoteTellBasic
go test -race -cpu 4 -timeout 10s -run=TestRemoteTellOrder
go test -race -cpu 4 -timeout 15s -run=TestRemoteTellNoBlock

# TestLocalSync
go test -race -cpu 4 -timeout 15s -run=TestLocalSyncBasic1
go test -race -cpu 4 -timeout 15s -run=TestLocalSyncBasic2
go test -race -cpu 4 -timeout 15s -run=TestLocalSyncBasic3
go test -race -cpu 4 -timeout 15s -run=TestLocalSyncBasic4

go test -race -cpu 4 -timeout 30s -run=TestLocalSyncFrequency1
go test -race -cpu 4 -timeout 30s -run=TestLocalSyncFrequency2
go test -race -cpu 4 -timeout 30s -run=TestLocalSyncFrequency3
go test -race -cpu 4 -timeout 30s -run=TestLocalSyncFrequency4

go test -race -cpu 4 -timeout 30s -run=TestLocalSyncSize1
go test -race -cpu 4 -timeout 30s -run=TestLocalSyncSize2
go test -race -cpu 4 -timeout 30s -run=TestLocalSyncSize3
go test -race -cpu 4 -timeout 30s -run=TestLocalSyncSize4

go test -race -cpu 4 -timeout 15s -run=TestLocalSyncLWW1
go test -race -cpu 4 -timeout 15s -run=TestLocalSyncLWW2
go test -race -cpu 4 -timeout 15s -run=TestLocalSyncLWW3
go test -race -cpu 4 -timeout 15s -run=TestLocalSyncLWW4

go test -race -cpu 4 -timeout 40s -run=TestLocalSyncStress1
go test -race -cpu 4 -timeout 40s -run=TestLocalSyncStress2

# TestRemoteSync
go test -race -cpu 4 -timeout 20s -run=TestRemoteSyncBasic1
go test -race -cpu 4 -timeout 20s -run=TestRemoteSyncBasic2
go test -race -cpu 4 -timeout 20s -run=TestRemoteSyncBasic3
go test -race -cpu 4 -timeout 20s -run=TestRemoteSyncBasic4

go test -race -cpu 4 -timeout 20s -run=TestRemoteSyncLocal1
go test -race -cpu 4 -timeout 20s -run=TestRemoteSyncLocal2
go test -race -cpu 4 -timeout 20s -run=TestRemoteSyncLocal3
go test -race -cpu 4 -timeout 20s -run=TestRemoteSyncLocal4

go test -race -cpu 4 -timeout 60s -run=TestRemoteSyncFrequency1
go test -race -cpu 4 -timeout 60s -run=TestRemoteSyncFrequency2
go test -race -cpu 4 -timeout 60s -run=TestRemoteSyncFrequency3
go test -race -cpu 4 -timeout 60s -run=TestRemoteSyncFrequency4

go test -race -cpu 4 -timeout 40s -run=TestRemoteSyncSize1
go test -race -cpu 4 -timeout 40s -run=TestRemoteSyncSize2
go test -race -cpu 4 -timeout 40s -run=TestRemoteSyncSize3
go test -race -cpu 4 -timeout 40s -run=TestRemoteSyncSize4

go test -race -cpu 4 -timeout 20s -run=TestRemoteSyncNoBcast1
go test -race -cpu 4 -timeout 20s -run=TestRemoteSyncNoBcast2

go test -race -cpu 4 -timeout 30s -run=TestRemoteSyncLWW1
go test -race -cpu 4 -timeout 30s -run=TestRemoteSyncLWW2
go test -race -cpu 4 -timeout 30s -run=TestRemoteSyncLWW3
go test -race -cpu 4 -timeout 30s -run=TestRemoteSyncLWW4

go test -race -cpu 4 -timeout 60s -run=TestServerStartup1
go test -race -cpu 4 -timeout 60s -run=TestServerStartup2
go test -race -cpu 4 -timeout 60s -run=TestServerStartup3
go test -race -cpu 4 -timeout 60s -run=TestServerStartup4
go test -race -cpu 4 -timeout 60s -run=TestServerStartup5

go test -race -cpu 4 -timeout 60s -run=TestRemoteSyncStress1
go test -race -cpu 4 -timeout 60s -run=TestRemoteSyncStress2
