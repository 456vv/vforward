package vforward

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	//"github.com/issue9/assert/v4"
)

// Helper: setupMockListeners 辅助函数，用于创建两个本地监听器
func setupMockListeners(t testing.TB) (net.Listener, net.Listener, net.Addr, net.Addr, func()) {
	// A Listener
	aListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to start A listener: %v", err)
	}
	aAddr := aListener.Addr()

	// B Listener (backend)
	bListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		aListener.Close()
		t.Fatalf("Failed to start B listener: %v", err)
	}
	bAddr := bListener.Addr()

	cleanup := func() {
		aListener.Close()
		bListener.Close()
	}
	go func() {
		for {
			conn, err := aListener.Accept()
			if err != nil {
				return
			}
			go func() {
				io.Copy(conn, conn)
				conn.Close()
			}()
		}
	}()
	go func() {
		for {
			conn, err := bListener.Accept()
			if err != nil {
				return
			}
			go func() {
				io.Copy(conn, conn)
				conn.Close()
			}()
		}
	}()
	return aListener, bListener, aAddr, bAddr, cleanup
}

func TestD2D_New(t *testing.T) {
	_, _, aAddr, bAddr, cleanup := setupMockListeners(t)
	defer cleanup()

	dd := &D2D{
		TryTime:     100 * time.Millisecond,
		Timeout:     time.Second,
		MaxIdleConn: 2,
		MaxConn:     5,
		BufSize:     DefaultReadBufSize,
	}

	optA := &Option{Remote: aAddr}
	optB := &Option{Remote: bAddr}

	d2ds, err := dd.New(t.Context(), optA, optB)
	if err != nil {
		t.Fatalf("D2D.New failed: %v", err)
	}

	<-d2ds.acp.WaitConnIdleGeq(aAddr, 2)
	<-d2ds.bcp.WaitConnIdleGeq(bAddr, 2)

	if n := d2ds.acp.NumIdle(aAddr); n != 2 {
		t.Fatalf("Expected 2 idle connections for aAddr, got %d", n)
	}
	if n := d2ds.bcp.NumIdle(bAddr); n != 2 {
		t.Fatalf("Expected 2 idle connections for bAddr, got %d", n)
	}

	d2ds.Close()

	if d2ds.acp.Num() != 0 || d2ds.bcp.Num() != 0 {
		t.Errorf("Expected all connections to be closed after D2D.Close, but found idle connections for aAddr and bAddr")
	}
}

// TestD2DSwap_ConnNum 测试 ConnNum 方法
func TestD2DSwap_ConnNum(t *testing.T) {
	_, _, aAddr, bAddr, cleanup := setupMockListeners(t)
	defer cleanup()

	dd := &D2D{
		TryTime:     10 * time.Millisecond,
		Timeout:     time.Second,
		MaxIdleConn: 0,
		MaxConn:     3, // Limit connections for easier testing
		BufSize:     DefaultReadBufSize,
	}

	d2ds, err := dd.New(t.Context(), &Option{Remote: aAddr}, &Option{Remote: bAddr})
	if err != nil {
		t.Fatalf("D2D.New failed: %v", err)
	}
	defer d2ds.Close()

	if d2ds.ConnNum() != 0 {
		t.Errorf("Expected initial ConnNum 0, got %d", d2ds.ConnNum())
	}

	var swapWG sync.WaitGroup
	swapWG.Go(func() {
		err := d2ds.Swap()
		if err != nil {
			t.Errorf("D2DSwap.Swap returned unexpected error: %v", err)
		}
	})

	time.Sleep(time.Second)

	if n := d2ds.ConnNum(); n != 3 {
		t.Fatalf("Expected ConnNum 3 after swap, got %d", n)
	}

	<-d2ds.acp.WaitConnIdleLeq(aAddr, 0)
	<-d2ds.acp.WaitConnIdleGeq(aAddr, 0)
	<-d2ds.acp.WaitConnNumLeq(aAddr, 3)
	<-d2ds.acp.WaitConnNumGeq(aAddr, 3)
	if n := d2ds.acp.NumConn(aAddr); n != 3 {
		t.Fatalf("Expected 3 connections for aAddr after swap, got %d", n)
	}
	if n := d2ds.acp.NumIdle(aAddr); n != 0 {
		t.Fatalf("Expected 3 idle connections for aAddr after swap, got %d", n)
	}

	<-d2ds.bcp.WaitConnIdleLeq(bAddr, 0)
	<-d2ds.bcp.WaitConnIdleGeq(bAddr, 0)
	<-d2ds.bcp.WaitConnNumLeq(bAddr, 3)
	<-d2ds.bcp.WaitConnNumGeq(bAddr, 3)
	if n := d2ds.bcp.NumConn(bAddr); n != 3 {
		t.Fatalf("Expected 3 connections for bAddr after swap, got %d", n)
	}
	if n := d2ds.bcp.NumIdle(bAddr); n != 0 {
		t.Fatalf("Expected 3 idle connections for bAddr after swap, got %d", n)
	}

	d2ds.Close()
	swapWG.Wait()
}

// TestD2DSwap_Swap_AlreadyRunning 测试重复调用 Swap
func TestD2DSwap_Swap_AlreadyRunning(t *testing.T) {
	_, _, aAddr, bAddr, cleanup := setupMockListeners(t)
	defer cleanup()

	dd := &D2D{MaxConn: 1}

	d2ds, err := dd.New(t.Context(), &Option{Remote: aAddr}, &Option{Remote: bAddr})
	if err != nil {
		t.Fatalf("D2D.New failed: %v", err)
	}
	defer d2ds.Close()

	var wg sync.WaitGroup

	var err1, err2 error
	wg.Go(func() {
		err1 = d2ds.Swap()
	})
	time.Sleep(50 * time.Millisecond)
	wg.Go(func() {
		err2 = d2ds.Swap()
	})

	go func() {
		time.Sleep(200 * time.Millisecond)
		d2ds.Close()
	}()

	wg.Wait()

	if err1 != nil {
		t.Errorf("First Swap call returned unexpected error: %v", err1)
	}

	if err2 == nil {
		t.Errorf("Second Swap call expected '交换服务已在运行' error, got %v", err2)
	}
}

// TestD2DSwap_Swap_ContextCancellation 测试 Swap 方法的上下文取消
func TestD2DSwap_Swap_ContextCancellation(t *testing.T) {
	_, _, aAddr, bAddr, cleanup := setupMockListeners(t)
	defer cleanup()

	dd := &D2D{MaxConn: 2}
	ctx, cancel := context.WithCancel(t.Context())

	d2ds, err := dd.New(ctx, &Option{Remote: aAddr}, &Option{Remote: bAddr})
	if err != nil {
		t.Fatalf("D2D.New failed: %v", err)
	}
	defer d2ds.Close()

	var swapErr error
	var wg sync.WaitGroup
	wg.Go(func() {
		swapErr = d2ds.Swap()
	})

	time.Sleep(100 * time.Millisecond)

	cancel()

	wg.Wait()

	if swapErr != context.Canceled {
		t.Errorf("Expected Swap to return context.Canceled, got %v", swapErr)
	}

	err = d2ds.Close()
	if err != nil {
		t.Errorf("Closing D2DSwap after context cancellation failed: %v", err)
	}
}

// TestD2DSwap_Close_Concurrent 测试并发关闭
func TestD2DSwap_Close_Concurrent(t *testing.T) {
	_, _, aAddr, bAddr, cleanup := setupMockListeners(t)
	defer cleanup()

	dd := &D2D{
		TryTime:     10 * time.Millisecond,
		Timeout:     time.Second,
		MaxIdleConn: 0,
		MaxConn:     1,
		BufSize:     1024,
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	d2ds, err := dd.New(ctx, &Option{Remote: aAddr}, &Option{Remote: bAddr})
	if err != nil {
		t.Fatalf("D2D.New failed: %v", err)
	}
	defer d2ds.Close()

	var swapWG sync.WaitGroup
	swapWG.Go(func() {
		err := d2ds.Swap()
		if err.Error() != "vforward: 交换服务的连接池已关闭" {
			t.Errorf("Swap goroutine returned unexpected error: %v", err)
		}
	})

	time.Sleep(50 * time.Millisecond)
	initialConnNum := d2ds.ConnNum()
	if initialConnNum == 0 {
		t.Errorf("Expected active connections before close, got %d", initialConnNum)
	}

	var closeWG sync.WaitGroup
	for range 5 {
		closeWG.Go(func() {
			err := d2ds.Close()
			if err != nil {
				t.Errorf("Concurrent Close call failed: %v", err)
			}
		})
	}
	closeWG.Wait()
	swapWG.Wait()

	time.Sleep(50 * time.Millisecond)

	// Verify ConnNum is 0
	if d2ds.ConnNum() != 0 {
		t.Errorf("Expected ConnNum 0 after concurrent Close, got %d", d2ds.ConnNum())
	}

	// Verify that the D2DSwap is actually closed
	if !d2ds.closed.Load() {
		t.Error("D2DSwap.closed flag is false after Close")
	}
}

// TestD2DSwap_Close_NoActiveConnections 测试无活跃连接时关闭
func TestD2DSwap_Close_NoActiveConnections(t *testing.T) {
	_, _, aAddr, bAddr, cleanup := setupMockListeners(t)
	defer cleanup()

	dd := &D2D{MaxConn: 1}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	d2ds, err := dd.New(ctx, &Option{Remote: aAddr}, &Option{Remote: bAddr})
	if err != nil {
		t.Fatalf("D2D.New failed: %v", err)
	}

	// No Swap started, no clients connected.
	// ConnNum should be 0.
	if d2ds.ConnNum() != 0 {
		t.Errorf("Expected initial ConnNum 0, got %d", d2ds.ConnNum())
	}

	err = d2ds.Close()
	if err != nil {
		t.Errorf("Closing D2DSwap with no active connections failed: %v", err)
	}

	// Verify ConnNum is still 0
	if d2ds.ConnNum() != 0 {
		t.Errorf("Expected ConnNum 0 after Close with no active connections, got %d", d2ds.ConnNum())
	}
	if !d2ds.closed.Load() {
		t.Error("D2DSwap.closed flag is false after Close (no active connections)")
	}
}

// TestD2D_New_WithVerifyOption 测试带 Verify 函数的连接池
func TestD2D_New_WithVerifyOption(t *testing.T) {
	_, _, aAddr, bAddr, cleanup := setupMockListeners(t)
	defer cleanup()

	var verifyCalled atomic.Bool
	verifyFunc := func(c net.Conn) bool {
		verifyCalled.Store(true)
		return false
	}

	dd := &D2D{
		TryTime:     10 * time.Millisecond,
		Timeout:     time.Second,
		MaxIdleConn: 1,
		MaxConn:     1,
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	d2ds, err := dd.New(ctx, &Option{Remote: aAddr}, &Option{Remote: bAddr, Verify: verifyFunc})
	if err != nil {
		t.Fatalf("D2D.New failed: %v", err)
	}
	defer d2ds.Close()

	time.Sleep(dd.TryTime + 50*time.Millisecond)

	if !verifyCalled.Load() {
		t.Error("Verify function was not called")
	}
}

// TestD2DSwap_DialErrors 测试 DialContext 错误处理
func TestD2DSwap_DialErrors(t *testing.T) {
	_, bListener, aAddr, bAddr, cleanup := setupMockListeners(t)
	defer cleanup()

	bListener.Close()

	dd := &D2D{
		TryTime:     10 * time.Millisecond,
		Timeout:     time.Millisecond * 50, // Short timeout for faster error detection
		MaxIdleConn: 0,
		MaxConn:     1,
		BufSize:     1024,
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	d2ds, err := dd.New(ctx, &Option{Remote: aAddr}, &Option{Remote: bAddr})
	if err != nil {
		t.Fatalf("D2D.New failed: %v", err)
	}
	defer d2ds.Close()

	var swapWG sync.WaitGroup
	swapWG.Go(func() {
		swapErr := d2ds.Swap()
		if swapErr.Error() != "vforward: 交换服务的连接池已关闭" {
			t.Errorf("Swap goroutine returned unexpected error: %v", swapErr)
		}
	})
	time.Sleep(200 * time.Millisecond)

	if d2ds.ConnNum() != 0 {
		t.Errorf("Expected ConnNum 0 after B-side dial failure, got %d", d2ds.ConnNum())
	}

	d2ds.Close()
	swapWG.Wait()
}
