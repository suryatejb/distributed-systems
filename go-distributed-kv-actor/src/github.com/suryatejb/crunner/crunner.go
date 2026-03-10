// Runner for a kvclient.

package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/suryatejb/kvclient"
)

type fixedAddressRouter struct {
	address string
}

func (router fixedAddressRouter) NextAddr() string {
	return router.address
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: go run crunner.go <request actor address>")
		os.Exit(5)
	}
	address := os.Args[1]

	router := &fixedAddressRouter{address}
	cli := kvclient.NewClient(router)

	reader := bufio.NewReader(os.Stdin)
	for {
		// Read commands from the command prompt and call the corresponding
		// cli queries.
		fmt.Printf("> ")
		line, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Error:", err)
			os.Exit(4)
		}
		line = strings.TrimSpace(line)
		words := strings.Split(line, " ")
		if len(words) == 0 {
			fmt.Println("Commands are [Get, List, Put]")
			continue
		}
		switch words[0] {
		case "Get":
			if len(words) != 2 {
				fmt.Println("Usage: Get <key>")
				continue
			}
			key := words[1]
			value, ok, err := cli.Get(key)
			if err != nil {
				fmt.Println("Error:", err)
			}
			if ok {
				fmt.Printf("%q\n", value)
			} else {
				fmt.Println("Not present")
			}
		case "List":
			if len(words) > 2 {
				fmt.Println("Usage: List [key prefix]")
				continue
			}
			prefix := ""
			if len(words) == 2 {
				prefix = words[1]
			}
			entries, err := cli.List(prefix)
			if err != nil {
				fmt.Println("Error:", err)
			}
			for key, value := range entries {
				fmt.Printf("%q: %q\n", key, value)
			}
		case "Put":
			if len(words) != 3 {
				fmt.Println("Usage: Put <key> <value>")
				continue
			}
			key := words[1]
			value := words[2]
			err := cli.Put(key, value)
			if err != nil {
				fmt.Println("Error:", err)
			}
			fmt.Println("Ok")
		default:
			fmt.Println("Unknown command; commands are [Get, List, Put]")
		}
	}
}
