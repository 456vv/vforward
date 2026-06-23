// l2l_test.go
package vforward

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

func testListener(t *testing.T) *net.TCPAddr {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr)
}

func TestL2L_New(t *testing.T) {
	l2l := L2L{
		BufSize: 32 * 1024,
		MaxConn: 10,
	}

	optA := &Option{Local: testListener(t)}
	optB := &Option{Local: testListener(t)}

	swap, err := l2l.New(context.Background(), optA, optB)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if swap == nil {
		t.Fatal("swap nil")
	}
	defer swap.Close()

	if swap.ConnNum() != 0 {
		t.Error("initial conn num should be 0")
	}
}

func TestL2LSwap_Swap_Close(t *testing.T) {
	l2l := L2L{BufSize: 4096}

	optA := &Option{Local: testListener(t)}
	optB := &Option{Local: testListener(t)}

	swap, err := l2l.New(context.Background(), optA, optB)
	if err != nil {
		t.Fatal(err)
	}
	defer swap.Close()

	go swap.Swap()
	time.Sleep(100 * time.Millisecond) // Wait for Swap to start
	// Test duplicate Swap
	err = swap.Swap()
	if err == nil {
		t.Error("expected duplicate swap error")
	}

	// Close
	if err := swap.Close(); err != nil {
		t.Error(err)
	}
	if swap.ConnNum() != 0 {
		t.Error("conn num not zero after close")
	}
}

func TestL2LSwap_DataForward(t *testing.T) {
	l2l := L2L{
		BufSize: 16 * 1024,
		MaxConn: 100,
	}
	optA := &Option{Local: testListener(t)}
	optB := &Option{Local: testListener(t)}

	swap, err := l2l.New(context.Background(), optA, optB)
	if err != nil {
		t.Fatal(err)
	}
	defer swap.Close()

	go swap.Swap()
	time.Sleep(time.Millisecond * 500)

	var wg sync.WaitGroup
	wg.Go(func() {
		connA, err := net.Dial("tcp", swap.al.Addr().String())
		if err != nil {
			t.Error(err)
			return
		}
		defer connA.Close()

		msg := "Hello from A!"
		_, err = connA.Write([]byte(msg))
		if err != nil {
			t.Error(err)
			return
		}

		// Read response
		resp := make([]byte, 1024)
		n, err := connA.Read(resp)
		if err != nil {
			t.Error(err)
			return
		}
		if string(resp[:n]) != "Echo from B: "+msg {
			t.Errorf("unexpected response: %s", resp[:n])
		}
	})

	// Client to B side (simulates the forward target)
	wg.Go(func() {
		connB, err := net.Dial("tcp", swap.bl.Addr().String())
		if err != nil {
			t.Error(err)
			return
		}
		defer connB.Close()

		// Read from forward
		p := make([]byte, 100)
		n, err := connB.Read(p)
		if err != nil {
			t.Error(err)
			return
		}

		// Echo back
		resp := "Echo from B: " + string(p[:n])
		_, err = connB.Write([]byte(resp))
		if err != nil {
			t.Error(err)
		}
	})

	wg.Wait()

	// Check conn count eventually goes to 0
	time.Sleep(100 * time.Millisecond)
	if swap.ConnNum() != 0 {
		t.Logf("Warning: conn num %d after test", swap.ConnNum())
	}
}
