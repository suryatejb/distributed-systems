package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"time"

	"github.com/suryatejb/bitcoin"
	"github.com/suryatejb/lsp"
)

// joinWithServer connects to the mining server and sends a Join message to register as a miner.
func joinWithServer(hostport string) (lsp.Client, error) {
	// You will need this for randomized isn
	seed := rand.NewSource(time.Now().UnixNano())
	isn := rand.New(seed).Intn(int(math.Pow(2, 8)))

	// Connect to the server
	client, err := lsp.NewClient(hostport, isn, lsp.NewParams())
	if err != nil {
		return nil, err
	}

	// Send Join message to register as a miner
	joinMsg := bitcoin.NewJoin()
	payload, err := json.Marshal(joinMsg)
	if err != nil {
		client.Close()
		return nil, err
	}

	err = client.Write(payload)
	if err != nil {
		client.Close()
		return nil, err
	}

	return client, nil
}

var LOGF *log.Logger

func main() {
	// You may need a logger for debug purpose
	const (
		name = "minerLog.txt"
		flag = os.O_RDWR | os.O_CREATE
		perm = os.FileMode(0666)
	)

	file, err := os.OpenFile(name, flag, perm)
	if err != nil {
		return
	}
	defer file.Close()

	const numArgs = 2
	if len(os.Args) != numArgs {
		fmt.Printf("Usage: ./%s <hostport>", os.Args[0])
		return
	}

	hostport := os.Args[1]
	miner, err := joinWithServer(hostport)
	if err != nil {
		fmt.Println("Failed to join with server:", err)
		return
	}

	defer miner.Close()

	// Main mining loop: receive jobs, compute hashes, send results
	for {
		// Read job request from server
		payload, err := miner.Read()
		if err != nil {
			// Server closed or network error - exit
			return
		}

		// Unmarshal the request
		var jobRequest bitcoin.Message
		err = json.Unmarshal(payload, &jobRequest)
		if err != nil {
			continue
		}

		// Compute the minimum hash over the given range
		minHash := uint64(math.MaxUint64)
		minNonce := uint64(0)

		for nonce := jobRequest.Lower; nonce <= jobRequest.Upper; nonce++ {
			hash := bitcoin.Hash(jobRequest.Data, nonce)
			if hash < minHash {
				minHash = hash
				minNonce = nonce
			}
			// Break if we've reached the upper bound to avoid wraparound
			if nonce == jobRequest.Upper {
				break
			}
		}

		// Send result back to server
		resultMsg := bitcoin.NewResult(minHash, minNonce)
		resultPayload, err := json.Marshal(resultMsg)
		if err != nil {
			return
		}

		err = miner.Write(resultPayload)
		if err != nil {
			// Write error - exit
			return
		}
	}
}
