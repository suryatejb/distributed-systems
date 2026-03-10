// Runner for one kvserver.

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/suryatejb/kvserver"
)

var (
	port  = flag.Int("port", 6000, "starting port number")
	count = flag.Int("count", 1, "request actor count")
)

func init() {
	// Usage string.
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage:\n\tsrunner [options] <existing server descs...>\nwhere options are:\n")
		flag.PrintDefaults()
	}
}

func main() {
	flag.Parse()
	fmt.Println("Starting server...")
	_, desc, err := kvserver.NewServer(*port, *count, flag.Args())
	if err != nil {
		fmt.Printf("Failed to start Server on ports %d-%d: %s\n", *port, *port+*count, err)
		os.Exit(3)
	}
	fmt.Printf("Actor system running on port %d\n", *port)
	fmt.Printf("Request servers running on ports %d-%d\n", *port+1, *port+*count)
	fmt.Printf("Description for future servers: %q\n", desc)
	// Run forever
	select {}
}
