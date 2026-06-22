package vforward

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/issue9/assert/v4"
)

var testPort atomic.Int32

func getTestAddr(network string) net.Addr {
	port := int(testPort.Add(1) + 20000)
	switch network {
	case "udp", "udp4", "udp6":
		return &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port}
	default:
		return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: port}
	}
}

func startEchoServer(t *testing.T, addr net.Addr) net.Listener {
	ln, err := net.Listen(addr.Network(), addr.String())
	assert.New(t, true).NotError(err)
	t.Cleanup(func() {
		ln.Close()
	})

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				c.SetDeadline(time.Now().Add(time.Second))
				io.Copy(c, c) // simple echo
			}(conn)
		}
	}()
	return ln
}

func startUDPEcho(t *testing.T, addr net.Addr) net.PacketConn {
	pc, err := net.ListenPacket(addr.Network(), addr.String())
	assert.New(t, true).NotError(err)
	t.Cleanup(func() { pc.Close() })

	go func() {
		buf := make([]byte, 4096)
		for {
			n, clientAddr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			pc.WriteTo(buf[:n], clientAddr)
		}
	}()
	return pc
}

func TestL2D_New(t *testing.T) {
	as := assert.New(t, true)
	tests := []struct {
		name    string
		ld      L2D
		a, b    *Option
		wantErr bool
	}{
		{"basic TCP", L2D{BufSize: 4096}, &Option{Local: getTestAddr("tcp")}, &Option{Remote: getTestAddr("tcp")}, false},
		{"UDP", L2D{}, &Option{Local: getTestAddr("udp")}, &Option{Remote: getTestAddr("udp")}, false},
		{"defaults", L2D{}, &Option{Local: getTestAddr("tcp")}, &Option{Remote: getTestAddr("tcp")}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			swap, err := tt.ld.New(context.Background(), tt.a, tt.b)
			if tt.wantErr {
				as.Error(err)
				return
			}
			as.NotError(err)
			as.Equal(int32(0), swap.ConnNum())
		})
	}
}

func TestL2DSwap_Swap_TCP(t *testing.T) {
	as := assert.New(t, true)
	ld := L2D{BufSize: DefaultReadBufSize, DialTimeout: 2 * time.Second}
	localAddr := getTestAddr("tcp")
	remoteAddr := getTestAddr("tcp")
	startEchoServer(t, remoteAddr)

	aOpt := &Option{Local: localAddr}
	bOpt := &Option{Remote: remoteAddr}

	swap, err := ld.New(context.Background(), aOpt, bOpt)
	as.NotError(err)

	go swap.Swap()
	defer swap.Close()
	time.Sleep(time.Millisecond * 500)

	conn, err := net.Dial("tcp", localAddr.String())
	as.NotError(err)
	defer conn.Close()

	msg := []byte("hello vforward tcp")
	_, err = conn.Write(msg)
	as.NotError(err)

	resp := make([]byte, len(msg))
	_, err = io.ReadFull(conn, resp)
	as.NotError(err)
	as.Equal(msg, resp)
}

func TestL2DSwap_Swap_UDP(t *testing.T) {
	as := assert.New(t, true)
	ld := L2D{BufSize: DefaultReadBufSize}
	localAddr := getTestAddr("udp")
	remoteAddr := getTestAddr("udp")
	startUDPEcho(t, remoteAddr)

	aOpt := &Option{Local: localAddr}
	bOpt := &Option{Remote: remoteAddr}

	swap, err := ld.New(context.Background(), aOpt, bOpt)
	as.NotError(err)

	go swap.Swap()
	time.Sleep(time.Millisecond * 500)
	t.Cleanup(func() { swap.Close() })

	conn, err := net.DialUDP("udp", nil, localAddr.(*net.UDPAddr))
	as.NotError(err)
	defer conn.Close()

	msg := []byte("udp forward test")
	_, err = conn.Write(msg)
	as.NotError(err)

	resp := make([]byte, len(msg))
	conn.SetReadDeadline(time.Now().Add(time.Second))
	_, err = conn.Read(resp)
	as.NotError(err)
	as.Equal(msg, resp)
}

func TestL2DSwap_Close_And_Reopen(t *testing.T) {
	as := assert.New(t, true)
	ld := L2D{}
	a := &Option{Local: getTestAddr("tcp")}
	b := &Option{Remote: getTestAddr("tcp")}
	swap, err := ld.New(context.Background(), a, b)
	as.NotError(err)

	go swap.Swap()
	time.Sleep(time.Millisecond * 500)
	defer swap.Close()

	swap2, err := ld.New(context.Background(), a, b)
	as.NotError(err)
	go func() {
		err := swap2.Swap()
		as.NotError(err)
	}()
	time.Sleep(time.Millisecond * 500)
	swap2.Close()
}

func TestVerifyFunc(t *testing.T) {
	as := assert.New(t, true)
	ld := L2D{}
	called := false
	verify := func(c net.Conn) bool {
		called = true
		return false // reject
	}
	aOpt := &Option{Local: getTestAddr("tcp"), Verify: verify}
	bOpt := &Option{Remote: getTestAddr("tcp")}

	swap, _ := ld.New(context.Background(), aOpt, bOpt)
	go swap.Swap()
	time.Sleep(time.Millisecond * 500)
	defer swap.Close()

	// Connection should be rejected
	_, err := net.DialTimeout("tcp", aOpt.Local.String(), 500*time.Millisecond)
	as.NotError(err)
	time.Sleep(time.Millisecond * 500)
	as.True(called)
}

func TestMaxConnAndKeepAvailable(t *testing.T) {
	as := assert.New(t, true)
	ld := L2D{MaxConn: 3, IdleConn: 1, TryTime: 100 * time.Millisecond, BufSize: 1024}
	local := getTestAddr("tcp")
	remote := getTestAddr("tcp")
	startEchoServer(t, remote)

	swap, _ := ld.New(context.Background(), &Option{Local: local}, &Option{Remote: remote})
	go swap.Swap()
	time.Sleep(time.Millisecond * 500)
	defer swap.Close()

	var conns []net.Conn
	for range 10 {
		if c, err := net.Dial("tcp", local.String()); err == nil {
			conns = append(conns, c)
		}
	}

	time.Sleep(300 * time.Millisecond) // allow keepAvailable

	as.Equal(swap.ConnNum(), int32(3))

	for _, c := range conns {
		c.Close()
	}
}

func TestConcurrentSwapsAndCrossCalls(t *testing.T) {
	t.Parallel()
	const workers = 8
	var wg sync.WaitGroup
	for i := range workers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ld := L2D{
				BufSize: 512 + (id%4)*1024,
				MaxConn: 5,
			}
			local := getTestAddr("tcp")
			remote := getTestAddr("tcp")
			startEchoServer(t, remote) // per-worker echo

			swap, err := ld.New(context.Background(), &Option{Local: local}, &Option{Remote: remote})
			if err != nil {
				return
			}

			go swap.Swap()
			time.Sleep(time.Millisecond * 500)
			defer swap.Close()

			// Cross-call data + ConnNum
			for j := 0; j < 3; j++ {
				if conn, err := net.DialTimeout("tcp", local.String(), 500*time.Millisecond); err == nil {
					conn.Write([]byte("cross-thread-ping"))
					conn.Close()
				}
			}
		}(i)
	}
	wg.Wait()
}
