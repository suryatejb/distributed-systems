// Tests for SquarerImpl.
package p0partB

import (
	"fmt"
	"testing"
	"time"
)

const (
	timeoutMillis    = 5000
	operationTimeout = 1500
	closeTimeoutSecs = 3
)

func TestBasicCorrectness(t *testing.T) {
	fmt.Println("Running TestBasicCorrectness.")
	input := make(chan int)
	sq := SquarerImpl{}
	squares := sq.Initialize(input)
	go func() {
		input <- 2
	}()
	timeoutChan := time.After(time.Duration(timeoutMillis) * time.Millisecond)
	select {
	case <-timeoutChan:
		t.Error("Test timed out.")
	case result := <-squares:
		if result != 4 {
			t.Error("Error, got result", result, ", expected 4 (=2^2).")
		}
	}
}

func TestSequentialProcessing(t *testing.T) {
	input := make(chan int)
	sq := SquarerImpl{}
	output := sq.Initialize(input)

	// Send sequential values to test ordering
	go func() {
		for i := 1; i <= 6; i++ {
			input <- i
		}
	}()

	// Verify outputs arrive in correct order
	for i := 1; i <= 6; i++ {
		select {
		case x := <-output:
			if x != i*i {
				t.Fatalf("Expected %d, received %d", i*i, x)
			}
		case <-time.After(operationTimeout * time.Millisecond):
			t.Fatal("Test timed out waiting for squared value")
		}
	}
	sq.Close()
}

func TestSendBlocksAfterClose(t *testing.T) {
	input := make(chan int)
	sq := SquarerImpl{}
	output := sq.Initialize(input)
	_ = output // simulate unread output channel

	sq.Close()

	// Verify input channel behavior after cleanup
	select {
	case input <- 13:
		t.Error("Input accepted data after squarer termination")
	case <-time.After(closeTimeoutSecs * time.Second):
		// Expected behavior - input should not accept new data
	}
}
