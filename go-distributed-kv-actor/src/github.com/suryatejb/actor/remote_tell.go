package actor

import (
	"net/rpc"
)

// RPC args for RemoteTell
type RemoteTellArgs struct {
	Ref  *ActorRef
	Mars []byte
}

// RPC reply for RemoteTell (empty, but needed for RPC interface)
type RemoteTellReply struct {
}

// RPC handler for remote tells
type remoteTellHandler struct {
	system *ActorSystem
}

// RemoteTell RPC method that forwards the message to the local ActorSystem
func (handler *remoteTellHandler) RemoteTell(args RemoteTellArgs, reply *RemoteTellReply) error {
	handler.system.tellFromRemote(args.Ref, args.Mars)
	return nil
}

// Calls system.tellFromRemote(ref, mars) on the remote ActorSystem listening
// on ref.Address.
//
// This function should NOT wait for a reply from the remote system before
// returning, to allow sending multiple messages in a row more quickly.
// It should ensure that messages are delivered in-order to the remote system.
// (You may assume that remoteTell is not called multiple times
// concurrently with the same ref.Address).
func remoteTell(client *rpc.Client, ref *ActorRef, mars []byte) {
	args := RemoteTellArgs{
		Ref:  ref,
		Mars: mars,
	}
	var reply RemoteTellReply

	// Use Go() for async RPC call to not wait for reply
	client.Go("RemoteTellHandler.RemoteTell", args, &reply, nil)
}

// Registers an RPC handler on server for remoteTell calls to system.
//
// You do not need to start the server's listening on the network;
// just register a handler struct that handles remoteTell RPCs by calling
// system.tellFromRemote(ref, mars).
func registerRemoteTells(system *ActorSystem, server *rpc.Server) error {
	handler := &remoteTellHandler{
		system: system,
	}
	return server.RegisterName("RemoteTellHandler", handler)
}
