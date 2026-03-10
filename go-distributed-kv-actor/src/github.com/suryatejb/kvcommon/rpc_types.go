// Package kvcommon includes shared internals for kvclient and kvserver.
package kvcommon

// Args for Get RPC.
type GetArgs struct {
	Key string
}

// Reply for Get RPC.
type GetReply struct {
	Value string
	Ok    bool
}

// Args for List RPC.
type ListArgs struct {
	Prefix string
}

// Reply for List RPC.
type ListReply struct {
	Entries map[string]string
}

// Args for Put RPC.
type PutArgs struct {
	Key   string
	Value string
}

// Reply for Put RPC.
type PutReply struct {
}

// Interface for kvclient-kvserver RPC calls.
type QueryReceiver interface {
	// Returns the value associated with args.Key, if present.
	// If not present, reply.Ok is false.
	Get(args GetArgs, reply *GetReply) error
	// Returns all (key, value) pairs whose key starts with prefix, similar
	// to recursively listing all files in a folder.
	List(args ListArgs, reply *ListReply) error
	// Sets the value associated with key.
	Put(args PutArgs, reply *PutReply) error
}
