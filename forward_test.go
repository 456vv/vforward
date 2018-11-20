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


func Test_D2D_0(t *testing.T){
    var exit bool

    addra := &Addr{
        Network:"tcp",
        Local: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"),Port: 0,},
        Remote: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"),Port: 1201,},
    }
    addrb := &Addr{
        Network:"tcp",
        Local: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"),Port: 0,},
        Remote: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"),Port: 1202,},
    }

    //服务器
    al, err := net.Listen(addra.Remote.Network(), addra.Remote.String())
    if err != nil {
        t.Fatal(err)
    }
    bl, err := net.Listen(addrb.Remote.Network(), addrb.Remote.String())
    if err != nil {
        al.Close()
        t.Fatal(err)
    }
    defer al.Close()
    defer bl.Close()
    done := make(chan bool)
    go func(t *testing.T){
        done<-true
        for  {
            if exit {
                return
            }
            conn, err := al.Accept()
            if err != nil {
            	return
            	t.Fatal("正常错误：", err)
            }
            go io.Copy(conn, conn)
        }
    }(t)
    <-done
    	
    go func(t *testing.T){
        done<-true
        for  {
            if exit {
                return
            }
            conn, err := bl.Accept()
            if err != nil {
            	return
            	t.Fatal("正常错误：", err)
            }
            go io.Copy(conn, conn)
        }
    }(t)
    <-done

    //客户端
    dd := &D2D{
        TryConnTime: time.Millisecond,
        Timeout: time.Second,
        MaxConn:500,
        KeptIdeConn:4,
        ReadBufSize:1024,
        ErrorLog:log.New(os.Stdout, "*", log.LstdFlags),
    }
    defer dd.Close()
    defer dd.Close()
    dds, err := dd.Transport(addra, addrb)
    if err != nil {
        t.Fatalf("发起请求失败：%s", err)
    }

    go func(t *testing.T){
        defer func(){
            dd.Close()
            done<-true
        }()
        time.Sleep(time.Second*2)
        if dds.ConnNum() != dd.MaxConn {
            t.Fatalf("连接数未达预计数量。返回为：%d，预计为：%d", dds.ConnNum(), dd.MaxConn)
        }
        dds.Close()
        dd.Close()
        time.Sleep(time.Second*2)
        //由于是异步关闭连接，所以需要等一会
        if dds.ConnNum() != 0 {
            t.Fatalf("还有连接没有被关闭。返回为：%d，预计为：0", dds.ConnNum())
        }
    }(t)
    defer dds.Close()
    defer dds.Close()
    dds.Swap()
    exit = <-done
}

func Test_D2D_1(t *testing.T){
    var exit bool

    addra := &Addr{
        Network:"tcp",
        Local: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"),Port: 0,},
        Remote: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"),Port: 1201,},
    }
    addrb := &Addr{
        Network:"tcp",
        Local: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"),Port: 0,},
        Remote: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"),Port: 1202,},
    }

    //服务器
    al, err := net.Listen(addra.Remote.Network(), addra.Remote.String())
    if err != nil {
        t.Fatal(err)
    }
    defer al.Close()
    done := make(chan bool)
    go func(t *testing.T){
        done<-true
        for  {
            if exit {
                return
            }
            conn, err := al.Accept()
            if err != nil {
            	return
            	t.Fatal("正常错误：", err)
            }
            go io.Copy(conn, conn)
        }
    }(t)
    <-done
    	
    //客户端
    dd := &D2D{
        TryConnTime: time.Millisecond,
        Timeout: time.Second,
        MaxConn:50,
        KeptIdeConn:4,
        ReadBufSize:1024,
        ErrorLog:log.New(os.Stdout, "*", log.LstdFlags),
    }
    defer dd.Close()
    defer dd.Close()
    dds, err := dd.Transport(addra, addrb)
    if err != nil {
        t.Fatalf("发起请求失败：%s", err)
    }

    go func(t *testing.T){
        defer func(){
            dd.Close()
            done<-true
        }()
        time.Sleep(time.Second*2)
        if ideconn := dd.acp.ConnNumIde(addra.Remote.Network(), addra.Remote.String()); ideconn != dd.KeptIdeConn {
            t.Fatalf("空闲连接数未达预计数量。返回为：%d，预计为：%d", ideconn, dd.KeptIdeConn)
        }
        dds.Close()
        dd.Close()
        time.Sleep(time.Second*3)
        //由于是异步关闭连接，所以需要等一会
        if ideconn := dd.acp.ConnNumIde(addra.Remote.Network(), addra.Remote.String()); ideconn  != 0 {
            t.Fatalf("还有连接没有被关闭。返回为：%d，预计为：0", ideconn)
        }
    }(t)
    defer dds.Close()
    defer dds.Close()
    dds.Swap()
    exit = <-done
}

func Test_L2D_0(t *testing.T){
	log.SetFlags(log.Lshortfile|log.LstdFlags)

    var exit bool
    dial := &Addr{
        Network:"tcp",
        Local: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"),Port: 0,},
        Remote: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"),Port: 1234,},
    }
    listen := &Addr{
        Network:"tcp",
        Local: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"),Port: 2345,},
    }

    //服务器
    al, err := net.Listen(dial.Network, dial.Remote.String())
    if err != nil {t.Fatal(err)}
    defer al.Close()
    done := make(chan bool)
    go func(t *testing.T){
        done<- true
        for {
            if exit {
                return
            }
            conn, err := al.Accept()
            if err != nil {
             	return
           		t.Fatal("正常错误：", err)
            }
            go io.Copy(conn, conn)
        }
    }(t)
    <-done

    go func(t *testing.T){
    	<-done
        for {
            if exit {
                return
            }
            time.Sleep(time.Millisecond)
        	conn, err := net.Dial(listen.Network, listen.Local.String())
            if err != nil {
            	//log.Println(err)
    			continue
            }
            b := []byte("123")
            conn.Write(b)
            p := make([]byte, 10)
            n, err := conn.Read(p)
            if err != nil {
            	//远程关闭连接
            	//log.Println(err)
    			continue
            }
           	defer conn.Close()
            if !bytes.Equal(p[:n], b) {
            	t.Fatal("两个数据不相等")
            }
        }
    }(t)

    //客户端
    ld := &L2D{
        MaxConn:10,
        ReadBufSize:1024,
        ErrorLog:log.New(os.Stdout, "*", log.LstdFlags),
    }
    defer ld.Close()
    defer ld.Close()

    lds, err := ld.Transport(dial, listen)
    if err != nil {
        t.Fatalf("发起请求失败：%s", err)
    }
    _, err = ld.Transport(dial, listen)
    if err == nil {
        t.Fatalf("不应该可以重复调用")
    }
    
    exitld := make(chan bool)
    go func(t *testing.T){
        defer func(){
            lds.Close()
            exit 	= true
            exitld	<-true
        }()
        time.Sleep(time.Second*2)
        
        if lds.ConnNum() != ld.MaxConn {
            t.Logf("连接数计数量不准。返回为：%d，预计为：%d", lds.ConnNum(), ld.MaxConn)
        }
        lds.Close()
        //异步关闭需要等一会
        time.Sleep(time.Second*4)
        if lds.ConnNum() != 0 {
            t.Logf("还有连接没有被关闭。返回为：%d，预计为：0",  lds.ConnNum())
        }
    }(t)

    defer lds.Close()
    defer lds.Close()
    
    done<-true
    lds.Swap()
    <-exitld
}


func Test_L2D_1(t *testing.T){
 	log.SetFlags(log.Lshortfile|log.LstdFlags)
	var exit bool
    dial := &Addr{
        Network:"udp",
        Local: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"),Port: 0,},
        Remote: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"),Port: 1205,},
    }
    listen := &Addr{
        Network:"udp",
        Local: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"),Port: 1206,},
    }

    //服务器
    server, err := net.ListenPacket(dial.Network, dial.Remote.String())
    if err != nil {t.Fatal(err)}
    defer server.Close()
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
            	t.Fatal("正常错误：", err)
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
    defer ld.Close()
    defer ld.Close()

    lds, err := ld.Transport(dial, listen)
    if err != nil {
        t.Fatalf("发起请求失败：%s", err)
    }
    _, err = ld.Transport(dial, listen)
    if err == nil {
        t.Fatalf("不应该可以重复调用")
    }
    
    go func(t *testing.T){
        defer func (){
   			exit = true
    		done<-true
        }()

        p := []byte("1234")
        for i:=0;i<ld.MaxConn;i++ {
            client, err := net.DialUDP(listen.Network, nil, listen.Local.(*net.UDPAddr))
            if err != nil {
                lds.Close()
                t.Fatalf("可能是服务器还没启动：%s", err)
            }
            client.Write(p)
            b := make([]byte, 1024)
            client.SetReadDeadline(time.Now().Add(time.Second))
            n, err := client.Read(b)

            if err == nil && !bytes.Equal(p, b[:n]) {
                fmt.Printf("UDP不正常，发送和收到数据不一致。发送的是：%s，返回的是：%s\n", p, b[:n])
            }
            client.Close()
        }
       if lds.ConnNum() != ld.MaxConn {
            t.Logf("连接数未达预计数量。返回为：%d，预计为：%d", lds.ConnNum(), ld.MaxConn)
        }
        ld.Close()
        lds.Close()
        time.Sleep(time.Second)
       if lds.ConnNum() != 0 {
            t.Logf("还有连接没有被关闭。返回为：%d，预计为：0", lds.ConnNum())
        }
    }(t)

    defer lds.Close()
    lds.Swap()
    <-done
}

func Test_L2L_0(t *testing.T){
    addra := &Addr{
        Network: "tcp",
        Local: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"),Port: 1207,},
    }
    addrb := &Addr{
        Network: "tcp",
        Local: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"),Port: 1208,},
    }
    ll := &L2L{
        MaxConn:5,
        KeptIdeConn:2,
        ErrorLog:log.New(os.Stdout, "*", log.LstdFlags),
    }
    defer ll.Close()
    lls, err := ll.Transport(addra, addrb)
    if err != nil {
        t.Fatalf("发起请求失败：%s", err)
    }

    exitgo := make(chan bool,1)
    go func(t *testing.T){
        defer func(){
            lls.Close()
            exitgo<-true
        }()
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
            t.Logf("连接数未达预计数量。返回为：%d，预计为：%d", lls.ConnNum(), ll.MaxConn)
        }
        lls.Close()
        lls.Close()
    }(t)
    defer lls.Close()

    lls.Swap()
    <-exitgo

    time.Sleep(time.Second)
    if lls.ConnNum() != 0 {
        t.Logf("还有连接没有被关闭。返回为：%d，预计为：0", lls.ConnNum())
    }

    go func(){
        ll.Close()
        lls.Close()
        exitgo<- true
    }()
    lls.Swap()
    <-exitgo
}
