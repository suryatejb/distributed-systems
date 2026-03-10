// MODIFICATIONS IGNORED ON GRADESCOPE!

package actor

import (
	"bytes"
	"encoding/gob"
)

// Need to register to-level structs with gob.Register before using
// (typically in an init() function).

// Marshals message into []byte using encoding/gob.
//
// message must be encodable by encoding/gob as any (i.e., as
// an interface). In particular, you need to register top-level structs
// with gob.Register (typically in an init() function) before marshalling them.
func marshal(message any) ([]byte, error) {
	var buffer bytes.Buffer
	enc := gob.NewEncoder(&buffer)
	err := enc.Encode(&message)
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

// Unmarshals a message marshalled by marshal().
func unmarshal(b []byte) (any, error) {
	dec := gob.NewDecoder(bytes.NewBuffer(b))
	var message any
	err := dec.Decode(&message)
	if err != nil {
		return nil, err
	}
	return message, nil
}
