package main

import (
    "github.com/456vv/vforward"
    "net"
    "log"
    "time"
    "flag"
    "fmt"
)
var fNetwork 		= flag.String("Network", "tcp", "网络地址类型")

var fALocal 		= flag.String("ALocal", "0.0.0.0", "A端本地发起连接地址")
var fARemote 		= flag.String("ARemote", "", "A端远程请求连接地址 (format \"12.13.14.15:123\")")
var fAVerify		= flag.String("AVerify", "", "A的验证字符串，桥接后的发出的第一条验证数据头。")

var fBLocal 		= flag.String("BLocal", "0.0.0.0", "B端本地发起连接地址")
var fBRemote 		= flag.String("BRemote", "", "B端远程请求连接地址 (format \"22.23.24.25:234\")")
var fBVerify		= flag.String("BVerify", "", "B的验证字符串，桥接后的发出的第一条验证数据头。")

var fTryConnTime 	= flag.Duration("TryConnTime", time.Millisecond*500, "尝试或发起连接时间，可能一方不在线，会一直尝试连接对方。单位：ns, us, ms, s, m, h")
var fTimeout 		= flag.Duration("Timeout", time.Second*5, "转发连接时候，请求远程连接超时。单位：ns, us, ms, s, m, h")
var fMaxConn 		= flag.Int("MaxConn", 0, "限制连接最大的数量")
var fKeptIdeConn 	= flag.Int("KeptIdeConn", 2, "保持一方连接数量，以备快速互相连接。")
var fIdeTimeout		= flag.Duration("IdeTimeout", 0, "空闲连接超时。单位：ns, us, ms, s, m, h")
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
    
    log.SetFlags(log.Lshortfile)
    
    var err error
    if *fARemote == "" || *fBRemote == "" {
        log.Printf("地址未填，A端远程地址 %q, B端远程地址 %q", *fARemote, *fBRemote)
        return
    }

    var (
     	addra = vforward.Addr{Network:*fNetwork, Local: &net.TCPAddr{IP: net.ParseIP(*fALocal),Port: 0,}}
     	addrb = vforward.Addr{Network:*fNetwork, Local: &net.TCPAddr{IP: net.ParseIP(*fBLocal),Port: 0,}}
     ) 
    switch *fNetwork {
    	case "tcp", "tcp4", "tcp6":
            addra.Remote, err = net.ResolveTCPAddr(*fNetwork, *fARemote)
            if err != nil {
                log.Println(err)
                return
            }
            addrb.Remote, err = net.ResolveTCPAddr(*fNetwork, *fBRemote)
            if err != nil {
                log.Println(err)
                return
            }
    	case "udp", "udp4", "udp6":
            addra.Remote, err = net.ResolveUDPAddr(*fNetwork, *fARemote)
            if err != nil {
                log.Println(err)
                return
            }
            addrb.Remote, err = net.ResolveUDPAddr(*fNetwork, *fBRemote)
            if err != nil {
                log.Println(err)
                return
            }
        default:
            log.Printf("网络地址类型  %q 是未知的，日前仅支持：tcp/tcp4/tcp6 或 upd/udp4/udp6", *fNetwork)
            return
    }

	dd := &vforward.D2D{
        TryConnTime: *fTryConnTime,             // 尝试或发起连接时间，可能一方不在线，会一直尝试连接对方。
        MaxConn: *fMaxConn,                     // 限制连接最大的数量
        KeptIdeConn: *fKeptIdeConn,             // 保持一方连接数量，以备快速互相连接。
        IdeTimeout: *fIdeTimeout,				// 空闲连接超时
        Timeout: *fTimeout,                     // 发起连接超时
        ReadBufSize: *fReadBufSize,             // 交换数据缓冲大小
    }
    dds, err := dd.Transport(&addra, &addrb)
    if err != nil {
        log.Println(err)
        return
    }
    defer dd.Close()
	dds.Verify = func(a, b net.Conn) (net.Conn, net.Conn, error){
		if *fAVerify != ""  {
			_, ne := a.Write([]byte(*fAVerify))
			if ne != nil {
				a.Close()
				b.Close()
				return nil, nil, ne
			}
		}
		if *fBVerify != ""  {
			_, ie := b.Write([]byte(*fBVerify))
			if ie != nil {
				a.Close()
				b.Close()
				return nil, nil, ie
			}
		}
		return a, b, nil
	}
	defer dds.Close()
    err = dds.Swap()
    if err != nil {
        log.Printf("错误：%s\n", err)
    }
}
