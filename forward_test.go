package vforward

import (
    "testing"
    "net"
    "time"
    "fmt"
    "bytes"
    "log"
    "os"
    "io"
    
)

//如果测试出现错误提示，可能是你的电脑响应速度没跟上。
//由于关闭网络需要一段时间，所以我设置了延时。

var addra = &Addr{
        Network:"tcp",
        Local: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"),Port: 0,},
        Remote: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"),Port: 0,},
    }
var addrb = &Addr{
        Network:"tcp",
        Local: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"),Port: 0,},
        Remote: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"),Port: 0,},
    }
	


func fatal(t *testing.T, err error){
    if err != nil {
    	t.Fatal(err)
    }
}
func server(t *testing.T, addr net.Addr, c chan net.Listener){
    bl, err := net.Listen(addr.Network(), addr.String())
    fatal(t, err)
    defer bl.Close()
    c <- bl
    for {
	    conn, err := bl.Accept()
	    if err != nil {
	    	return
	    }
		go func(conn net.Conn){
			defer conn.Close()
            io.Copy(conn, conn)
		}(conn)
    }
}
func runServer(t *testing.T, addr net.Addr) net.Listener {
	run := make(chan net.Listener, 1)
	go server(t, addr, run)
	l := <- run
    *addr.(*net.TCPAddr) = *l.Addr().(*net.TCPAddr)
	return l
}

func Test_D2D_0(t *testing.T){
    al := runServer(t, addra.Remote)
    defer al.Close()
    bl := runServer(t, addrb.Remote)
    defer bl.Close()
	
    //客户端
    dd := &D2D{
        TryConnTime: time.Millisecond,
        Timeout: time.Second,
        MaxConn:500,
        KeptIdeConn:4,
        ReadBufSize:1024,
        ErrorLog:log.New(os.Stdout, "*", log.LstdFlags|log.Lshortfile),
    }
    defer dd.Close()
    defer dd.Close()
    
    dds, err := dd.Transport(addra, addrb)
    if err != nil {
        t.Fatalf("发起请求失败：%s", err)
    }
    defer dds.Close()
    defer dds.Close()

    go func(t *testing.T){
        time.Sleep(time.Second*2)
        if dds.ConnNum() != dd.MaxConn {
            t.Fatalf("连接数未达预计数量。返回为：%d，预计为：%d", dds.ConnNum(), dd.MaxConn)
        }
        dds.Close()
        dd.Close()
    }(t)
    dds.Swap()
    time.Sleep(time.Second)
    if dds.ConnNum() != 0 {
        t.Fatalf("还有连接没有被关闭。返回为：%d，预计为：0", dds.ConnNum())
    }
}

func Test_L2D_0(t *testing.T){
    var exit bool

    bl := runServer(t, addrb.Remote)
    defer bl.Close()

    //客户端
    ld := &L2D{
        MaxConn:10,
        ReadBufSize:1024,
        ErrorLog:log.New(os.Stdout, "*", log.LstdFlags|log.Lshortfile),
    }
	
    lds, err := ld.Transport(addra, addrb)
    if err != nil {
        t.Fatalf("发起请求失败：%s", err)
    }
    defer ld.Close()
    defer ld.Close()

    _, err = ld.Transport(addra, addrb)
    if err == nil {
        t.Fatalf("不应该可以重复调用")
    }
	
	//创建连接并发送数据
    go func(t *testing.T){
        addr, ok := ld.listen.(interface{Addr() net.Addr})
        if !ok {
        	t.Fatal("error")
        }
        b := []byte("123")
        p := make([]byte, 10)
		for {
            if exit {
                return
            }
			time.Sleep(time.Millisecond)
			conn, err := net.Dial(addr.Addr().Network(), addr.Addr().String())
            if err != nil {
    			continue
            }
            conn.Write(b)
            n, err := conn.Read(p)
            if err != nil {
    			continue
            }
           	defer conn.Close()
            if !bytes.Equal(p[:n], b) {
            	t.Fatal("两个数据不相等")
            }
        }
    }(t)
	
	//判断连接数量
    go func(t *testing.T){
        time.Sleep(time.Second)
        if lds.ConnNum() != ld.MaxConn {
            t.Fatalf("连接数计数量不准。返回为：%d，预计为：%d", lds.ConnNum(), ld.MaxConn)
        }
        
        exit = true
        lds.Close()
    }(t)

    defer lds.Close()
    defer lds.Close()
    lds.Swap()
    time.Sleep(time.Second)
    if lds.ConnNum() != 0 {
        t.Fatalf("还有连接没有被关闭。返回为：%d，预计为：0",  lds.ConnNum())
    }
}


func Test_L2D_1(t *testing.T){
 	log.SetFlags(log.Lshortfile|log.LstdFlags)
	var exit bool
	
    dial := &Addr{
        Network:"udp",
        Local: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"),Port: 0,},
        Remote: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"),Port: 0,},
    }
    listen := &Addr{
        Network:"udp",
        Local: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"),Port: 0,},
    }

    //服务器
    server, err := net.ListenPacket(dial.Network, dial.Remote.String())
    if err != nil {t.Fatal(err)}
    defer server.Close()
    dial.Remote = server.LocalAddr()
    
    done := make(chan bool)
    go func(t *testing.T){
        done<- true
        for {
            if exit {
                return
            }
            b := make([]byte, 1024)
        	n, laddr, err := server.ReadFrom(b)
            if err != nil {
            	return
            }
        	go server.WriteTo(b[:n], laddr)
        }
    }(t)
    <-done

    //客户端
    ld := &L2D{
        MaxConn:10,
        ReadBufSize:1024,
        Timeout:time.Second*2,
        ErrorLog:log.New(os.Stdout, "*", log.LstdFlags),
    }

    lds, err := ld.Transport(listen, dial)
    if err != nil {
        t.Fatalf("发起请求失败：%s", err)
    }
    _, err = ld.Transport(listen, dial)
    if err == nil {
        t.Fatalf("不应该可以重复调用")
    }
    defer ld.Close()
    defer ld.Close()
    
	//创建连接并发送数据
    go func(t *testing.T){
        addr, ok := ld.listen.(interface{LocalAddr() net.Addr})
        if !ok {
        	t.Fatal("error")
        }
        
        b := []byte("123")
        p := make([]byte, 10)
        for i:=0;i<ld.MaxConn;i++ {
            client, err := net.DialUDP(listen.Network, nil, addr.LocalAddr().(*net.UDPAddr))
            if err != nil {
                lds.Close()
                t.Fatalf("可能是服务器还没启动：%s", err)
            }
            
            client.Write(b)
            client.SetReadDeadline(time.Now().Add(time.Second))
            n, err := client.Read(p)
            if err == nil && !bytes.Equal(b, p[:n]) {
                fmt.Printf("UDP不正常，发送和收到数据不一致。发送的是：%s，返回的是：%s\n", b, p[:n])
            }
            client.Close()
        }
		
		if lds.ConnNum() != ld.MaxConn {
            t.Fatalf("连接数未达预计数量。返回为：%d，预计为：%d", lds.ConnNum(), ld.MaxConn)
        }
        
   		exit = true
        lds.Close()
    }(t)

    defer lds.Close()
    defer lds.Close()
    lds.Swap()
	time.Sleep(time.Second)
	if lds.ConnNum() != 0 {
	    t.Fatalf("还有连接没有被关闭。返回为：%d，预计为：0", lds.ConnNum())
	}

}

func Test_L2L_0(t *testing.T){
    ll := &L2L{
        MaxConn:5,
        KeptIdeConn:2,
        ErrorLog:log.New(os.Stdout, "*", log.LstdFlags),
    }
    lls, err := ll.Transport(addra, addrb)
    if err != nil {
        t.Fatalf("发起请求失败：%s", err)
    }
    defer ll.Close()
    
    go func(t *testing.T){
       	//监听端口改变
        addra.Local = ll.alisten.Addr()
        addrb.Local = ll.blisten.Addr()
        
        
        for i:=0;i<ll.MaxConn*2;i++ {
        	conna, err := net.Dial(addra.Network, addra.Local.String())
            if err != nil {
                t.Fatalf("客户端连接失败：%s", err)
            }
            defer conna.Close()

        	connb, err := net.Dial(addrb.Network, addrb.Local.String())
            if err != nil {
                t.Fatalf("客户端连接失败：%s", err)
            }
            defer connb.Close()
            time.Sleep(time.Millisecond*100)
        }
        
        time.Sleep(time.Second)
        if lls.ConnNum() != ll.MaxConn {
            t.Fatalf("连接数未达预计数量。返回为：%d，预计为：%d", lls.ConnNum(), ll.MaxConn)
        }
        lls.Close()
        lls.Close()
    }(t)

    defer lls.Close()
    lls.Swap()

    time.Sleep(time.Second)
    if lls.ConnNum() != 0 {
        t.Fatalf("还有连接没有被关闭。返回为：%d，预计为：0", lls.ConnNum())
    }

}
