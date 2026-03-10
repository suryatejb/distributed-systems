package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/suryatejb/bitcoin"
	"github.com/suryatejb/lsp"
)

var LOGF *log.Logger

func main() {
	// You may need a logger for debug purpose
	const (
		name = "clientLog.txt"
		flag = os.O_RDWR | os.O_CREATE
		perm = os.FileMode(0666)
	)

	file, err := os.OpenFile(name, flag, perm)
	if err != nil {
		return
	}
	defer file.Close()

	const numArgs = 4
	if len(os.Args) != numArgs {
		fmt.Printf("Usage: ./%s <hostport> <message> <maxNonce>", os.Args[0])
		return
	}
	hostport := os.Args[1]
	message := os.Args[2]
	maxNonce, err := strconv.ParseUint(os.Args[3], 10, 64)
	if err != nil {
		fmt.Printf("%s is not a number.\n", os.Args[3])
		return
	}
	seed := rand.NewSource(time.Now().UnixNano())
	isn := rand.New(seed).Intn(int(math.Pow(2, 8)))

	client, err := lsp.NewClient(hostport, isn, lsp.NewParams())
	if err != nil {
		fmt.Println("Failed to connect to server:", err)
		return
	}

	defer client.Close()

	// Create and send the mining request
	requestMsg := bitcoin.NewRequest(message, 0, maxNonce)
	payload, err := json.Marshal(requestMsg)
	if err != nil {
		printDisconnected()
		return
	}

	err = client.Write(payload)
	if err != nil {
		printDisconnected()
		return
	}

	// Wait for the result from the server
	responsePayload, err := client.Read()
	if err != nil {
		printDisconnected()
		return
	}

	// Unmarshal the result
	var resultMsg bitcoin.Message
	err = json.Unmarshal(responsePayload, &resultMsg)
	if err != nil {
		printDisconnected()
		return
	}

	// Print the result
	printResult(resultMsg.Hash, resultMsg.Nonce)
}

// printResult prints the final mining result to stdout in format "Result <hash> <nonce>".
func printResult(hash, nonce uint64) {
	fmt.Println("Result", hash, nonce)
}

// printDisconnected prints "Disconnected" to stdout when the client loses connection to the server.
func printDisconnected() {
	fmt.Println("Disconnected")
}
