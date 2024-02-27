package vforward

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// 如果测试出现错误提示，可能是你的电脑响应速度没跟上。
// 由于关闭网络需要一段时间，所以我设置了延时。
var addra = &Addr{
	Network: "tcp",
	Local:   &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0},
	Remote:  &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0},
}

var addrb = &Addr{
	Network: "tcp",
	Local:   &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0},
	Remote:  &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0},
}

func fatal(t *testing.T, err ...interface{}) {
	if len(err) > 0 && err[0] != nil {
		t.Fatal(err...)
	}
}

func serverTCP(t *testing.T, addr net.Addr, c chan net.Listener) {
	bl, err := net.Listen(addr.Network(), addr.String())
	fatal(t, err)
	defer bl.Close()
	c <- bl
	for {
		conn, err := bl.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			io.Copy(conn, conn)
		}()
	}
}

func runServerTCP(t *testing.T, addr net.Addr) net.Listener {
	run := make(chan net.Listener, 1)
	go serverTCP(t, addr, run)
	l := <-run
	*addr.(*net.TCPAddr) = *l.Addr().(*net.TCPAddr)
	return l
}

func serverUDP(t *testing.T, addr net.Addr, c chan net.PacketConn) {
	bl, err := net.ListenPacket(addr.Network(), addr.String())
	fatal(t, err)
	defer bl.Close()
	c <- bl
	// var i int
	for {
		p := make([]byte, 1024) // 设置过小，读取到不完成的数据，laddr为nil。
		n, laddr, err := bl.ReadFrom(p)
		// t.Log(i, err)
		// i++
		if err != nil {
			break
		}
		bl.WriteTo(p[:n], laddr)
	}
}

func runServerUDP(t *testing.T, addr net.Addr) net.PacketConn {
	run := make(chan net.PacketConn, 1)
	go serverUDP(t, addr, run)
	l := <-run
	*addr.(*net.UDPAddr) = *l.LocalAddr().(*net.UDPAddr)
	return l
}

// 判断创建连接是否达到最大
func Test_D2D_0(t *testing.T) {
	defer runServerTCP(t, addra.Remote).Close()
	defer runServerTCP(t, addrb.Remote).Close()

	// 客户端
	dd := new(D2D)
	dd.TryConnTime = time.Millisecond
	dd.MaxConn(500)
	dd.KeptIdeConn(4)
	defer dd.Close()

	birdge, err := dd.Transport(addra, addrb)
	fatal(t, err)
	defer birdge.Close()

	go func() {
		defer birdge.Close()
		time.Sleep(time.Second * 2)
		if birdge.ConnNum() != dd.acp.MaxConn {
			t.Logf("连接数未达预计数量。返回为：%d，预计为：%d", birdge.ConnNum(), dd.acp.MaxConn)
			t.Fail()
		}
	}()

	birdge.Swap()
	time.Sleep(time.Second)
	if birdge.ConnNum() != 0 {
		t.Fatalf("还有连接没有被关闭。返回为：%d，预计为：0", birdge.ConnNum())
	}
}

// 判断创建连接是否达到最大
// 判断转发的内容是否正确
func Test_L2D_TCP(t *testing.T) {
	defer runServerTCP(t, addrb.Remote).Close()

	// 客户端
	ld := new(L2D)
	ld.MaxConn(1000)
	defer ld.Close()

	bridge, err := ld.Transport(addra, addrb)
	fatal(t, err)
	defer bridge.Close()

	// 创建连接并发送数据
	go func() {
		defer bridge.Close()

		addr := ld.listen.(interface{ Addr() net.Addr }).Addr()

		var wg sync.WaitGroup
		for i := 0; i < ld.maxConn+100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				conn, err := net.Dial(addr.Network(), addr.String())
				fatal(t, err)
				defer conn.Close()

				b := make([]byte, 1024)
				rand.Reader.Read(b)
				conn.Write(b)

				p := make([]byte, 1024)
				n, err := conn.Read(p)
				// 连接达到最大，ld.maxConn+100
				if err == io.EOF {
					return
				}
				var ne *net.OpError
				if ok := errors.As(err, &ne); ok {
					switch e := ne.Err.Error(); e {
					case "wsarecv: An existing connection was forcibly closed by the remote host.":
						return
					case "wsarecv: An established connection was aborted by the software in your host machine.":
						return
					default:
						t.Log(e)
					}
				}
				fatal(t, err)

				if !bytes.Equal(p[:n], b) {
					fatal(t, "两个数据不相等")
				}
			}()
		}
		wg.Wait()
	}()

	bridge.Swap()
	time.Sleep(time.Second)
	if bridge.ConnNum() != 0 {
		t.Fatalf("还有连接没有被关闭。返回为：%d，预计为：0", bridge.ConnNum())
	}
}

// 判断创建连接是否达到最大
// 测试UDP发送与接收
func Test_L2D_UDP(t *testing.T) {
	listen := &Addr{
		Network: "udp",
		Local:   &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0},
	}
	dial := &Addr{
		Network: "udp",
		Local:   &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0},
		Remote:  &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0},
	}

	defer runServerUDP(t, dial.Remote).Close()

	ld := new(L2D)
	ld.MaxConn(1000)

	defer ld.Close()

	bridge, err := ld.Transport(listen, dial)
	fatal(t, err)
	defer bridge.Close()

	// 创建连接并发送数据
	go func() {
		defer bridge.Close()

		addr := ld.listen.(interface{ LocalAddr() net.Addr }).LocalAddr()

		for i := 0; i < ld.maxConn+1; i++ {
			// 这里没有并行测试，由于UDP并行发送会丢包
			conn, err := net.Dial(addr.Network(), addr.String())
			fatal(t, err)
			defer conn.Close()

			b := make([]byte, 1024)
			rand.Reader.Read(b)
			conn.Write(b)

			conn.SetReadDeadline(time.Now().Add(time.Second))
			p := make([]byte, 1024)
			n, err := conn.Read(p)
			// 连接达到最大，ld.maxConn+1，服务器没有返回。过期读取超时
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			fatal(t, err)

			if !bytes.Equal(p[:n], b) {
				fatal(t, "两个数据不相等")
			}
		}
	}()

	bridge.Swap()
	time.Sleep(time.Second)
	if bridge.ConnNum() != 0 {
		t.Fatalf("还有连接没有被关闭。返回为：%d，预计为：0", bridge.ConnNum())
	}
}

// 判断创建连接是否达到最大
func Test_L2L_0(t *testing.T) {
	ll := new(L2L)
	ll.MaxConn(500)
	ll.KeptIdeConn(2)
	defer ll.Close()

	birdge, err := ll.Transport(addra, addrb)
	fatal(t, err)
	defer birdge.Close()

	go func() {
		defer birdge.Close()

		// 监听端口改变
		addra.Local = ll.alisten.Addr()
		addrb.Local = ll.blisten.Addr()

		for i := 0; i < ll.acp.MaxConn*2; i++ {
			conna, err := net.Dial(addra.Network, addra.Local.String())
			fatal(t, err)
			defer conna.Close()

			connb, err := net.Dial(addrb.Network, addrb.Local.String())
			fatal(t, err)
			defer connb.Close()
			time.Sleep(time.Millisecond)
		}

		time.Sleep(time.Second)
		if birdge.ConnNum() != ll.acp.MaxConn {
			t.Logf("连接数未达预计数量。返回为：%d，预计为：%d", birdge.ConnNum(), ll.acp.MaxConn)
			t.Fail()
		}
	}()

	birdge.Swap()
	time.Sleep(time.Second)
	if birdge.ConnNum() != 0 {
		t.Fatalf("还有连接没有被关闭。返回为：%d，预计为：0", birdge.ConnNum())
	}
}
