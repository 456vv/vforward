package main

import (
    "github.com/456vv/vforward"
    "flag"
    "net"
    "log"
    "fmt"
)
var fBackstage		= flag.Bool("Backstage", false, "后台启动进程")

var fNetwork 		= flag.String("Network", "tcp", "网络地址类型")
var fALocal 		= flag.String("ALocal", "", "A本地监听网卡IP地址 (format \"12.13.14.15:123\")")
var fBLocal 		= flag.String("BLocal", "", "B本地监听网卡IP地址 (format \"22.23.24.25:234\")")

var fMaxConn 		= flag.Int("MaxConn", 500, "限制连接最大的数量")
var fKeptIdeConn	 = flag.Int("KeptIdeConn", 2, "保持一方连接数量，以备快速互相连接。")
var fReadBufSize 	= flag.Int("ReadBufSize", 4096, "交换数据缓冲大小。单位：字节")


//commandline:l2l-main.exe -ALocal 127.0.0.1:1201 -BLocal 127.0.0.1:1202 -Network tcp
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
    if *fALocal == "" || *fBLocal == "" {
        log.Printf("地址未填，A监听地址 %q, B监听地址 %q", *fALocal, *fBLocal)
        return
    }
    switch *fNetwork {
    	case "tcp", "tcp4", "tcp6":
        default:
            log.Printf("网络地址类型  %q 是未知的，日前仅支持：tcp/tcp4/tcp6", *fNetwork)
            return
    }

    addr1, err := net.ResolveTCPAddr(*fNetwork, *fALocal)
    if err != nil {
        log.Println(err)
        return
    }
    addr2, err := net.ResolveTCPAddr(*fNetwork, *fBLocal)
    if err != nil {
        log.Println(err)
        return
    }
    addra := &vforward.Addr{
        Network:*fNetwork,
        Local: addr1,
    }
    addrb := &vforward.Addr{
        Network:*fNetwork,
        Local: addr2,
    }
    ll := &vforward.L2L{
        MaxConn: *fMaxConn,                     // 限制连接最大的数量
        KeptIdeConn: *fKeptIdeConn,             // 保持一方连接数量，以备快速互相连接。
        ReadBufSize: *fReadBufSize,             // 交换数据缓冲大小
    }
	defer ll.Close()
    lls, err := ll.Transport(addra, addrb)
    if err != nil {
        log.Println(err)
        return
    }
    
    if !*fBackstage {
	    go func(){
	        defer lls.Close()
	        log.Println("L2L启动了")

	        var in0 string
	        for err == nil  {
	            log.Println("输入任何字符，并回车可以退出L2L!")
	            fmt.Scan(&in0)
	            if in0 != "" {
    				log.Println("L2L退出了")
	                return
	            }
	        }
	    }()
	}
	defer lls.Close()
    err = lls.Swap()
    if err != nil {
        log.Println("错误：%s", err)
    }
}