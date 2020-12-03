package main

import (
    "github.com/456vv/vforward"
    "flag"
    "net"
    "time"
    "log"
    "fmt"
)

var fNetwork 		= flag.String("Network", "tcp", "网络地址类型")

var fListen 		= flag.String("Listen", "", "本地网卡监听地址 (format \"0.0.0.0:123\")")

var fFromLocal 		= flag.String("FromLocal", "0.0.0.0", "转发请求的源地址")
var fToRemote 		= flag.String("ToRemote", "", "转发请求的目地址 (format \"22.23.24.25:234\")")

var fTimeout 		= flag.Duration("Timeout", time.Second*5, "转发连接时候，请求远程连接超时。单位：ns, us, ms, s, m, h")
var fMaxConn 		= flag.Int("MaxConn", 0, "限制连接最大的数量")
var fReadBufSize 	= flag.Int("ReadBufSize", 4096, "交换数据缓冲大小。单位：字节")

//commandline:l2d-main.exe -Listen 127.0.0.1:1201 -ToRemote 127.0.0.1:1202 -Network tcp
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
    if *fListen == "" || *fToRemote == "" {
        log.Printf("地址未填，本地监听地址 %q, 转发到远程地址 %q", *fListen, *fToRemote)
        return
    }

    var (
     	listen = vforward.Addr{Network:*fNetwork}
     	dial = vforward.Addr{Network:*fNetwork, Local: &net.TCPAddr{IP: net.ParseIP(*fFromLocal),Port: 0,}}
     ) 
    switch *fNetwork {
    	case "tcp", "tcp4", "tcp6":
            listen.Local, err = net.ResolveTCPAddr(*fNetwork, *fListen)
            if err != nil {
                log.Println(err)
                return
            }
            dial.Remote, err = net.ResolveTCPAddr(*fNetwork, *fToRemote)
            if err != nil {
                log.Println(err)
                return
            }
    	case "udp", "udp4", "udp6":
            listen.Local, err = net.ResolveUDPAddr(*fNetwork, *fListen)
            if err != nil {
                log.Println(err)
                return
            }
            dial.Remote, err = net.ResolveUDPAddr(*fNetwork, *fToRemote)
            if err != nil {
                log.Println(err)
                return
            }
        default:
            log.Printf("网络地址类型  %q 是未知的，日前仅支持：tcp/tcp4/tcp6, upd/udp4/udp6", *fNetwork)
            return
    }

    ld := &vforward.L2D{
        MaxConn: *fMaxConn,            // 限制连接最大的数量
        ReadBufSize: *fReadBufSize,    // 交换数据缓冲大小
        Timeout: *fTimeout,            // 发起连接超时
    }
	
	defer ld.Close()
    lds, err := ld.Transport(&dial, &listen)
    if err != nil {
        log.Println(err)
        return
    }

	defer lds.Close()
    err = lds.Swap()
    if err != nil {
        log.Printf("错误：%s\n", err)
    }
}