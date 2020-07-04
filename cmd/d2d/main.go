package main

import (
    "github.com/456vv/vforward"
    "net"
    "log"
    "time"
    "flag"
    "fmt"
)
var fBackstage		= flag.Bool("Backstage", false, "后台启动进程")

var fNetwork 		= flag.String("Network", "tcp", "网络地址类型")

var fALocal 		= flag.String("ALocal", "0.0.0.0", "A端本地发起连接地址")
var fARemote 		= flag.String("ARemote", "", "A端远程请求连接地址 (format \"12.13.14.15:123\")")

var fBLocal 		= flag.String("BLocal", "0.0.0.0", "B端本地发起连接地址")
var fBRemote 		= flag.String("BRemote", "", "B端远程请求连接地址 (format \"22.23.24.25:234\")")

var fTryConnTime 	= flag.Duration("TryConnTime", time.Millisecond*500, "尝试或发起连接时间，可能一方不在线，会一直尝试连接对方。单位：ns, us, ms, s, m, h")
var fTimeout 		= flag.Duration("Timeout", time.Second*5, "转发连接时候，请求远程连接超时。单位：ns, us, ms, s, m, h")
var fMaxConn 		= flag.Int("MaxConn", 500, "限制连接最大的数量")
var fKeptIdeConn 	= flag.Int("KeptIdeConn", 2, "保持一方连接数量，以备快速互相连接。")
var fReadBufSize 	= flag.Int("ReadBufSize", 4096, "交换数据缓冲大小。单位：字节")


//commandline:d2d-main.exe -ARemote 127.0.0.1:1201 -BRemote 127.0.0.1:1202 -Network udp
func main(){
    flag.Parse()
    if flag.NFlag() == 0 {
    	if flag.NArg() != 0 {
    		fmt.Println(flag.Args())
    	}
        flag.PrintDefaults()
        return
    }
    var err error
    if *fARemote == "" || *fBRemote == "" {
        log.Printf("地址未填，A端远程地址 %q, B端远程地址 %q", *fARemote, *fBRemote)
        return
    }

    var addra *vforward.Addr
    var addrb *vforward.Addr
    switch *fNetwork {
    	case "tcp", "tcp4", "tcp6":
            rtcpaddr1, err := net.ResolveTCPAddr(*fNetwork, *fARemote)
            if err != nil {
                log.Println(err)
                return
            }
            rtcpaddr2, err := net.ResolveTCPAddr(*fNetwork, *fBRemote)
            if err != nil {
                log.Println(err)
                return
            }
            addra = &vforward.Addr{Network:*fNetwork,Local: &net.TCPAddr{IP: net.ParseIP(*fALocal),Port: 0,},Remote: rtcpaddr1,}
            addrb = &vforward.Addr{Network:*fNetwork,Local: &net.TCPAddr{IP: net.ParseIP(*fBLocal),Port: 0,},Remote: rtcpaddr2,}
    	case "udp", "udp4", "udp6":
            rudpaddr1, err := net.ResolveUDPAddr(*fNetwork, *fARemote)
            if err != nil {
                log.Println(err)
                return
            }
            rudpaddr2, err := net.ResolveUDPAddr(*fNetwork, *fBRemote)
            if err != nil {
                log.Println(err)
                return
            }
            addra = &vforward.Addr{Network:*fNetwork,Local: &net.UDPAddr{IP: net.ParseIP(*fALocal),Port: 0,},Remote: rudpaddr1,}
            addrb = &vforward.Addr{Network:*fNetwork,Local: &net.UDPAddr{IP: net.ParseIP(*fBLocal),Port: 0,},Remote: rudpaddr2,}
        default:
            log.Printf("网络地址类型  %q 是未知的，日前仅支持：tcp/tcp4/tcp6 或 upd/udp4/udp6", *fNetwork)
            return
    }

	dd := &vforward.D2D{
        TryConnTime: *fTryConnTime,             // 尝试或发起连接时间，可能一方不在线，会一直尝试连接对方。
        MaxConn: *fMaxConn,                     // 限制连接最大的数量
        KeptIdeConn: *fKeptIdeConn,             // 保持一方连接数量，以备快速互相连接。
        Timeout: *fTimeout,                     // 发起连接超时
        ReadBufSize: *fReadBufSize,             // 交换数据缓冲大小
    }
    defer dd.Close()
    dds, err := dd.Transport(addra, addrb)
    if err != nil {
        log.Println(err)
        return
    }
    
    if !*fBackstage {
	    go func(){
	        defer dd.Close()
	        log.Println("D2D启动了")

	        var in0 string
	        for err == nil  {
	            log.Println("输入任何字符，并回车可以退出D2D!")
	            fmt.Scan(&in0)
	            if in0 != "" {
    				log.Println("D2D退出了")
	                return
	            }
	        }
	    }()
	}
	defer dds.Close()
    err = dds.Swap()
    if err != nil {
        log.Println("错误：%s", err)
    }
}
