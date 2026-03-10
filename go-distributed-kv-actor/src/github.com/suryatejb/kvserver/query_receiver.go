package kvserver

import (
	"github.com/suryatejb/actor"
	"github.com/suryatejb/kvcommon"
)

// RPC handler implementing the kvcommon.QueryReceiver interface.
// There is one queryReceiver per queryActor, each running on its own port,
// created and registered for RPCs in NewServer.
//
// A queryReceiver MUST answer RPCs by sending a message to its query
// actor and getting a response message from that query actor (via
// ActorSystem's NewChannelRef). It must NOT attempt to answer queries
// using its own state, and it must NOT directly coordinate with other
// queryReceivers - all coordination is done within the actor system
// by its query actor.
type queryReceiver struct {
	system        *actor.ActorSystem
	queryActorRef *actor.ActorRef
}

// Get implements kvcommon.QueryReceiver.Get.
func (rcvr *queryReceiver) Get(args kvcommon.GetArgs, reply *kvcommon.GetReply) error {
	// Create a channel ref to receive response
	channelRef, responseChan := rcvr.system.NewChannelRef()

	// Send Get message to query actor
	msg := MGet{
		Key:    args.Key,
		Sender: channelRef,
	}
	rcvr.system.Tell(rcvr.queryActorRef, msg)

	// Wait for response
	response := <-responseChan
	getReply := response.(MGetReply)

	reply.Value = getReply.Value
	reply.Ok = getReply.Ok

	return nil
}

// List implements kvcommon.QueryReceiver.List.
func (rcvr *queryReceiver) List(args kvcommon.ListArgs, reply *kvcommon.ListReply) error {
	// Create a channel ref to receive response
	channelRef, responseChan := rcvr.system.NewChannelRef()

	// Send List message to query actor
	msg := MList{
		Prefix: args.Prefix,
		Sender: channelRef,
	}
	rcvr.system.Tell(rcvr.queryActorRef, msg)

	// Wait for response
	response := <-responseChan
	listReply := response.(MListReply)

	reply.Entries = listReply.Entries

	return nil
}

// Put implements kvcommon.QueryReceiver.Put.
func (rcvr *queryReceiver) Put(args kvcommon.PutArgs, reply *kvcommon.PutReply) error {
	// Create a channel ref to receive response
	channelRef, responseChan := rcvr.system.NewChannelRef()

	// Send Put message to query actor
	msg := MPut{
		Key:    args.Key,
		Value:  args.Value,
		Sender: channelRef,
	}
	rcvr.system.Tell(rcvr.queryActorRef, msg)

	// Wait for response
	<-responseChan

	return nil
}
