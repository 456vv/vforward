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

	"github.com/issue9/assert/v2"
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

	as := assert.New(t, true)

	// 客户端
	dd := new(D2D)
	dd.TryConnTime = time.Millisecond
	dd.MaxConn(500)
	dd.KeptIdeConn(4)
	defer dd.Close()

	birdge, err := dd.Transport(addra, addrb)
	as.NotError(err)
	defer birdge.Close()

	go func() {
		defer birdge.Close()
		time.Sleep(time.Second * 2)
		// 连接数未达预计数量
		as.Equal(birdge.ConnNum(), dd.acp.MaxConn)
	}()

	birdge.Swap()
	time.Sleep(time.Second * 5)
	// 还有连接没有被关闭
	as.Equal(birdge.ConnNum(), 0)
}

// 判断创建连接是否达到最大
// 判断转发的内容是否正确
func Test_L2D_TCP(t *testing.T) {
	defer runServerTCP(t, addrb.Remote).Close()

	as := assert.New(t, true)
	// 客户端
	ld := new(L2D)
	ld.MaxConn(100)
	defer ld.Close()

	bridge, err := ld.Transport(addra, addrb)
	as.NotError(err)
	defer bridge.Close()

	// 创建连接并发送数据
	go func() {
		defer bridge.Close()

		addr := ld.listen.(interface{ Addr() net.Addr }).Addr()

		var wg sync.WaitGroup
		var skipErr int
		for i := 0; i < ld.maxConn*2; i++ {
			wg.Add(1)
			conn, err := net.Dial(addr.Network(), addr.String())
			as.NotError(err)
			defer conn.Close()

			go func(conn net.Conn) {
				defer wg.Done()

				b := make([]byte, 1024)
				rand.Reader.Read(b)
				conn.Write(b)

				p := make([]byte, 1024)
				n, err := conn.Read(p)
				// 连接达到最大，ld.maxConn+100
				if err == io.EOF {
					skipErr++
					return
				}
				var ne *net.OpError
				if ok := errors.As(err, &ne); ok {
					switch e := ne.Err.Error(); e {
					case "wsarecv: An existing connection was forcibly closed by the remote host.":
						skipErr++
						return
					case "wsarecv: An established connection was aborted by the software in your host machine.":
						skipErr++
						return
					default:
						t.Log(e)
					}
				}
				as.NotError(err)
				// 两个数据不相等
				as.Equal(p[:n], b)
			}(conn)
		}
		wg.Wait()
		if skipErr != ld.maxConn {
			as.TB().Fatal("超出连接数量被关闭的数量不正确")
		}
	}()

	bridge.Swap()
	time.Sleep(time.Second)
	// 还有连接没有被关闭
	as.Equal(bridge.ConnNum(), 0)
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

	as := assert.New(t, true)

	ld := new(L2D)
	ld.MaxConn(1000)
	defer ld.Close()

	bridge, err := ld.Transport(listen, dial)
	as.NotError(err)
	defer bridge.Close()

	// 创建连接并发送数据
	go func() {
		defer bridge.Close()

		addr := ld.listen.(interface{ LocalAddr() net.Addr }).LocalAddr()

		for i := 0; i < ld.maxConn+1; i++ {
			// 这里没有并行测试，由于UDP并行发送会丢包
			conn, err := net.Dial(addr.Network(), addr.String())
			as.NotError(err)
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
			as.NotError(err)
			as.Equal(p[:n], b)
		}
	}()

	bridge.Swap()
	time.Sleep(time.Second)
	// 还有连接没有被关闭
	as.Equal(bridge.ConnNum(), 0)
}

// 判断创建连接是否达到最大
func Test_L2L_0(t *testing.T) {
	as := assert.New(t, true)

	ll := new(L2L)
	ll.MaxConn(50)
	ll.KeptIdeConn(50)
	defer ll.Close()

	birdge, err := ll.Transport(addra, addrb)
	as.NotError(err)
	defer birdge.Close()

	matchData := [][]byte{}
	addra.Local = ll.alisten.Addr()
	addrb.Local = ll.blisten.Addr()

	go func() {
		defer birdge.Close()

		var wg sync.WaitGroup
		var skipErr int
		for i := 0; i < ll.acp.MaxConn*2; i++ {
			wg.Add(1)

			conna, err := net.Dial(addra.Network, addra.Local.String())
			as.NotError(err)
			defer conna.Close()
			// 发送
			b := make([]byte, 10)
			rand.Reader.Read(b)
			matchData = append(matchData, b)
			conna.Write(b)

			connb, err := net.Dial(addrb.Network, addrb.Local.String())
			as.NotError(err)
			defer connb.Close()

			go func(connb net.Conn) {
				defer wg.Done()
				// 接收
				connb.SetReadDeadline(time.Now().Add(time.Second))
				p := make([]byte, 10)
				n, err := connb.Read(p)
				if err == io.EOF {
					// 连接数量达到最大，连接被关闭了
					skipErr++
					return
				}
				as.NotError(err)

				// 对比发送与接收
				var ok bool
				for _, v := range matchData {
					ok = bytes.Equal(v[:n], p[:n])
					if ok {
						break
					}
				}
				as.True(ok)
			}(connb)
		}

		time.Sleep(time.Second)
		as.Equal(birdge.ConnNum(), ll.acp.MaxConn)
		wg.Wait()

		if skipErr != ll.acp.MaxConn {
			as.TB().Fatal("超出连接数量被关闭的数量不正确")
		}
	}()

	birdge.Swap()
	time.Sleep(time.Second)
	as.Equal(birdge.ConnNum(), 0)
}

func Test_L2L_1(t *testing.T) {
	as := assert.New(t, true)

	ll := new(L2L)
	ll.MaxConn(0)
	ll.KeptIdeConn(1)
	defer ll.Close()

	birdge, err := ll.Transport(addra, addrb)
	as.NotError(err)
	defer birdge.Close()

	addra.Local = ll.alisten.Addr()
	addrb.Local = ll.blisten.Addr()

	go func() {
		defer birdge.Close()

		conna, err := net.Dial(addra.Network, addra.Local.String())
		as.NotError(err)
		defer conna.Close()

		connb, err := net.Dial(addrb.Network, addrb.Local.String())
		as.NotError(err)
		defer connb.Close()

		// 发送
		b := make([]byte, 10)
		rand.Reader.Read(b)
		conna.Write(b)

		connb.SetReadDeadline(time.Now().Add(time.Second))
		p := make([]byte, 10)
		n, err := connb.Read(p)
		as.NotError(err).Equal(b[:n], p[:n])
	}()

	birdge.Swap()
	time.Sleep(time.Second)
	as.Equal(birdge.ConnNum(), 0)
}
