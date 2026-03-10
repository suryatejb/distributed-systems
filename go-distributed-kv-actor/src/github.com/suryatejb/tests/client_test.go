// MODIFICATIONS IGNORED ON GRADESCOPE!

package tests

import (
	"fmt"
	"net"
	"net/rpc"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/suryatejb/kvclient"
	"github.com/suryatejb/kvcommon"
)

const clientTestTimeout = time.Duration(5) * time.Second

// === simServer

type rpcCall struct {
	// "Get", "Put", or "List"
	method string
	// GetArgs, ListArgs, or PutArgs
	args any
	// Send the GetReply/ListReply/PutReply on this channel.
	replyCh chan any
}

// kvcommon.QueryReceiver implementation used to simulate a working kvserver.
type simServer struct {
	ln     net.Listener
	t      *testing.T
	callCh chan rpcCall
}

func (server *simServer) Get(args kvcommon.GetArgs, reply *kvcommon.GetReply) error {
	replyCh := make(chan any)
	server.t.Logf("Received Get RPC with args %+v", args)
	server.callCh <- rpcCall{"Get", args, replyCh}
	*reply = (<-replyCh).(kvcommon.GetReply)
	server.t.Logf("Replying to Get RPC with %+v", *reply)
	return nil
}

func (server *simServer) List(args kvcommon.ListArgs, reply *kvcommon.ListReply) error {
	replyCh := make(chan any)
	server.t.Logf("Received List RPC with args %+v", args)
	server.callCh <- rpcCall{"List", args, replyCh}
	*reply = (<-replyCh).(kvcommon.ListReply)
	server.t.Logf("Replying to List RPC with %+v", *reply)
	return nil
}

func (server *simServer) Put(args kvcommon.PutArgs, reply *kvcommon.PutReply) error {
	replyCh := make(chan any)
	server.t.Logf("Received Put RPC with args %+v", args)
	server.callCh <- rpcCall{"Put", args, replyCh}
	*reply = (<-replyCh).(kvcommon.PutReply)
	server.t.Logf("Replying to Put RPC with %+v", *reply)
	return nil
}

// === Test utils

func newSimServer(t *testing.T, address string) *simServer {
	server := &simServer{nil, t, make(chan rpcCall)}
	rpcServer := rpc.NewServer()
	err := rpcServer.RegisterName("QueryReceiver", server)
	if err != nil {
		t.Fatalf("Error while starting simulated RPC server: %s", err)
	}
	ln, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("Error while starting simulated RPC server: %s", err)
	}
	server.ln = ln
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				// Assume it's due to ln.Close().
				return
			}
			go rpcServer.ServeConn(conn)
		}
	}()
	return server
}

func setupTestClient(t *testing.T) (*kvclient.Client, *simServer) {
	port := newPort()
	address := fmt.Sprintf("localhost:%d", port)
	server := newSimServer(t, address)

	// fixedAddressRouter is defined in utils.go.
	client := kvclient.NewClient(fixedAddressRouter{address})

	return client, server
}

func setUpClientManyServers(t *testing.T, num int) (*kvclient.Client, []*simServer) {
	var addresses []string
	var servers []*simServer
	for i := 1; i <= num; i++ {
		port := newPort()
		address := fmt.Sprintf("localhost:%d", port)
		server := newSimServer(t, address)

		addresses = append(addresses, address)
		servers = append(servers, server)
	}

	// dynamicAddressRouter is defined in utils.go.
	router := dynamicAddressRouter{addresses: addresses, Idx: 0}
	client := kvclient.NewClient(&router)

	return client, servers
}

func teardownTestClient(client *kvclient.Client, server *simServer) {
	client.Close()
	server.ln.Close()
}

func testSingleGet(t *testing.T, client *kvclient.Client, server *simServer, doneCh chan bool) {
	// Respond to the Get call when it comes.
	respondedCh := make(chan bool, 1)
	go func() {
		call := <-server.callCh
		if call.method != "Get" {
			t.Errorf("Unexpected call: %s", call.method)
		}
		args, ok := call.args.(kvcommon.GetArgs)
		if !ok || args.Key != "foo" {
			t.Errorf("Unexpected args: %+v", call.args)
		}

		respondedCh <- true

		call.replyCh <- kvcommon.GetReply{"bar", true}
	}()

	defer func() {
		doneCh <- true
	}()

	t.Log("Calling client.Get(\"foo\")")
	value, ok, err := client.Get("foo")
	if err != nil {
		t.Errorf("Get returned error: %s", err)
		return
	}
	if !ok {
		t.Errorf("Get returned ok=false but expected value \"bar\"")
		return
	}
	if value != "bar" {
		t.Errorf("Get returned incorrect value %q (expected \"bar\")", value)
	}

	t.Log("Checking that server was used")
	select {
	case <-respondedCh:
	default:
		t.Errorf("RPC server not used")
	}

	t.Log("Checking that there was only one RPC")
	timer := time.After(time.Millisecond)
	select {
	case <-server.callCh:
		t.Errorf("Unexpected second RPC")
	case <-timer:
	}

}

// === Tests

func TestClientGet(t *testing.T) {
	fmt.Printf("=== %s: %s\n", t.Name(), "Call client.Get with a reference RPC server")

	client, server := setupTestClient(t)
	defer teardownTestClient(client, server)

	doneCh := make(chan bool, 0)
	timeoutCh := time.After(clientTestTimeout)

	// Respond to the Get call when it comes.
	respondedCh := make(chan bool, 1)
	go func() {
		call := <-server.callCh
		if call.method != "Get" {
			t.Errorf("Unexpected call: %s", call.method)
		}
		args, ok := call.args.(kvcommon.GetArgs)
		if !ok || args.Key != "foo" {
			t.Errorf("Unexpected args: %+v", call.args)
		}

		respondedCh <- true

		call.replyCh <- kvcommon.GetReply{"bar", true}
	}()

	go func() {
		defer func() {
			doneCh <- true
		}()

		t.Log("Calling client.Get(\"foo\")")
		value, ok, err := client.Get("foo")
		if err != nil {
			t.Errorf("Get returned error: %s", err)
			return
		}
		if !ok {
			t.Errorf("Get returned ok=false but expected value \"bar\"")
			return
		}
		if value != "bar" {
			t.Errorf("Get returned incorrect value %q (expected \"bar\")", value)
		}

		t.Log("Checking that server was used")
		select {
		case <-respondedCh:
		default:
			t.Errorf("RPC server not used")
		}

		t.Log("Checking that there was only one RPC")
		timer := time.After(time.Millisecond)
		select {
		case <-server.callCh:
			t.Errorf("Unexpected second RPC")
		case <-timer:
		}
	}()

	select {
	case <-doneCh:
	case <-timeoutCh:
		t.Fatalf("Test timed out after %s", clientTestTimeout)
	}
}

func TestClientList(t *testing.T) {
	fmt.Printf("=== %s: %s\n", t.Name(), "Call client.List with a reference RPC server")

	client, server := setupTestClient(t)
	defer teardownTestClient(client, server)

	doneCh := make(chan bool, 0)
	timeoutCh := time.After(clientTestTimeout)

	// Setup planted values
	planted_map := make(map[string]string)
	planted_map["foo1"] = "one"
	planted_map["foo2"] = "two"

	// Respond to the List call when it comes.
	respondedCh := make(chan bool, 1)
	go func() {
		call := <-server.callCh
		if call.method != "List" {
			t.Errorf("Unexpected call: %s", call.method)
		}
		args, ok := call.args.(kvcommon.ListArgs)
		if !ok || args.Prefix != "foo" {
			t.Errorf("Unexpected args: %+v", call.args)
		}

		respondedCh <- true

		call.replyCh <- kvcommon.ListReply{planted_map}
	}()

	go func() {
		defer func() {
			doneCh <- true
		}()

		t.Log("Calling client.List(\"foo\")")
		value, err := client.List("foo")
		if err != nil {
			t.Errorf("List returned error: %s", err)
			return
		}
		if !reflect.DeepEqual(value, planted_map) {
			t.Errorf("List returned incorrect value %v (expected %v)", value, planted_map)
		}

		t.Log("Checking that server was used")
		select {
		case <-respondedCh:
		default:
			t.Errorf("RPC server not used")
		}

		t.Log("Checking that there was only one RPC")
		timer := time.After(time.Millisecond)
		select {
		case <-server.callCh:
			t.Errorf("Unexpected second RPC")
		case <-timer:
		}
	}()

	select {
	case <-doneCh:
	case <-timeoutCh:
		t.Fatalf("Test timed out after %s", clientTestTimeout)
	}
}

func TestClientPut(t *testing.T) {
	fmt.Printf("=== %s: %s\n", t.Name(), "Call client.Put with a reference RPC server")

	client, server := setupTestClient(t)
	defer teardownTestClient(client, server)

	doneCh := make(chan bool, 0)
	timeoutCh := time.After(clientTestTimeout)

	// Respond to the Put call when it comes.
	respondedCh := make(chan bool, 1)
	go func() {
		call := <-server.callCh
		if call.method != "Put" {
			t.Errorf("Unexpected call: %s", call.method)
		}
		args, ok := call.args.(kvcommon.PutArgs)
		if !ok || args.Key != "15440" || args.Value != "DistributedSystem" {
			t.Errorf("Unexpected args: %+v", call.args)
		}

		respondedCh <- true

		call.replyCh <- kvcommon.PutReply{}
	}()

	go func() {
		defer func() {
			doneCh <- true
		}()

		t.Log("Calling client.Put(\"15440\")")
		err := client.Put("15440", "DistributedSystem")
		if err != nil {
			t.Errorf("Put returned error: %s", err)
			return
		}

		t.Log("Checking that server was used")
		select {
		case <-respondedCh:
		default:
			t.Errorf("RPC server not used")
		}

		t.Log("Checking that there was only one RPC")
		timer := time.After(time.Millisecond)
		select {
		case <-server.callCh:
			t.Errorf("Unexpected second RPC")
		case <-timer:
		}
	}()

	select {
	case <-doneCh:
	case <-timeoutCh:
		t.Fatalf("Test timed out after %s", clientTestTimeout)
	}
}

func TestClientBalance(t *testing.T) {
	fmt.Printf("=== %s: %s\n", t.Name(), "Call client.Get with multiple reference RPC servers")

	serverNum := 3

	client, servers := setUpClientManyServers(t, serverNum)
	defer func() {
		client.Close()
		for _, server := range servers {
			server.ln.Close()
		}
	}()

	doneCh := make(chan bool, serverNum)
	timeoutCh := time.After(clientTestTimeout)

	for i := 0; i < serverNum; i++ {
		testSingleGet(t, client, servers[i], doneCh)
	}

	for i := 0; i < serverNum; i++ {
		select {
		case <-doneCh:
		case <-timeoutCh:
			t.Fatalf("Test timed out after %s", clientTestTimeout)
		}
	}
}

func TestClientSequential(t *testing.T) {
	fmt.Printf("=== %s: %s\n", t.Name(), "Call client.Get several times with a reference RPC server")

	client, server := setupTestClient(t)
	defer teardownTestClient(client, server)

	doneCh := make(chan bool, 0)
	timeoutCh := time.After(clientTestTimeout)

	numQ := 3

	// Respond to the Get call when it comes.
	respondedCh := make(chan bool, 1)
	go func() {
		for i := 0; i < numQ; i++ {
			call := <-server.callCh
			if call.method != "Get" {
				t.Errorf("Unexpected call: %s", call.method)
			}
			args, ok := call.args.(kvcommon.GetArgs)
			if !ok || args.Key != "foo"+strconv.Itoa(i) {
				t.Errorf("Unexpected args: %+v", call.args)
			}

			if i == numQ-1 {
				respondedCh <- true
			}

			call.replyCh <- kvcommon.GetReply{"bar" + strconv.Itoa(i), true}
		}
	}()

	go func() {
		defer func() {
			doneCh <- true
		}()
		for i := 0; i < numQ; i++ {
			t.Logf("Calling client.Get(%q)", "foo"+strconv.Itoa(i))
			value, ok, err := client.Get("foo" + strconv.Itoa(i))
			if err != nil {
				t.Errorf("Get returned error: %s", err)
				return
			}
			if !ok {
				t.Errorf("Get returned ok=false but expected value %q", "bar"+strconv.Itoa(i))
				return
			}
			if value != "bar"+strconv.Itoa(i) {
				t.Errorf("Get returned incorrect value %q (expected %q)", value, "bar"+strconv.Itoa(i))
			}
		}

		t.Log("Checking that server was used correct number of times")
		select {
		case <-respondedCh:
		default:
			t.Errorf("RPC server not used or used less than expected")
		}

		t.Log("Checking that there were no extra RPCs")
		timer := time.After(time.Millisecond)
		select {
		case <-server.callCh:
			t.Errorf("Unexpected extra RPC")
		case <-timer:
		}
	}()

	select {
	case <-doneCh:
	case <-timeoutCh:
		t.Fatalf("Test timed out after %s", clientTestTimeout)
	}
}

func TestClientConcurrent(t *testing.T) {
	fmt.Printf("=== %s: %s\n", t.Name(), "Call client.Get concurrently with a reference RPC server")

	client, server := setupTestClient(t)
	defer teardownTestClient(client, server)

	doneCh := make(chan bool, 0)
	timeoutCh := time.After(clientTestTimeout)

	delay := time.Duration(2) * time.Second
	delayCh := time.After(delay)

	msgTimeout := time.Duration(4) * time.Second
	msgTimeoutCh := time.After(msgTimeout)
	numQ := 3

	msgProcessCh := make(chan bool)

	// Respond to the Get call when it comes.
	respondedCh := make(chan bool, 1)
	go func() {
		<-delayCh
		for i := 0; i < numQ; i++ {
			call := <-server.callCh
			if call.method != "Get" {
				t.Errorf("Unexpected call: %s", call.method)
			}
			args, ok := call.args.(kvcommon.GetArgs)
			if !ok || args.Key != "foo" {
				t.Errorf("Unexpected args: %+v", call.args)
			}

			if i == numQ-1 {
				respondedCh <- true
			}

			call.replyCh <- kvcommon.GetReply{"bar", true}
		}
	}()

	go func() {
		defer func() {
			doneCh <- true
		}()
		for i := 0; i < numQ; i++ {
			go func() {
				t.Logf("Calling client.Get(%q)", "foo")
				value, ok, err := client.Get("foo")
				if err != nil {
					t.Errorf("Get returned error: %s", err)
					return
				}
				if !ok {
					t.Errorf("Get returned ok=false but expected value %q", "bar")
					return
				}
				if value != "bar" {
					t.Errorf("Get returned incorrect value %q (expected %q)", value, "bar")
				}
				msgProcessCh <- true
			}()

		}

		t.Log("Checking queries were handled concurrently")
		for i := 0; i < numQ; i++ {
			select {
			case <-msgTimeoutCh:
				t.Errorf("Client handles Get() too slowly")
			case <-msgProcessCh:
			}
		}

		t.Log("Checking that server was used correct number of times")
		select {
		case <-respondedCh:
		default:
			t.Errorf("RPC server not used or used less than expected")
		}

		t.Log("Checking that there were no extra RPCs")
		timer := time.After(time.Millisecond)
		select {
		case <-server.callCh:
			t.Errorf("Unexpected extra RPC")
		case <-timer:
		}
	}()

	select {
	case <-doneCh:
	case <-timeoutCh:
		t.Fatalf("Test timed out after %s", clientTestTimeout)
	}
}
