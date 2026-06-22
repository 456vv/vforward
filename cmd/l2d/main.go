package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/456vv/vforward/v2"
)

var fNetwork = flag.String("Network", "tcp", "网络地址类型")

var (
	fListen  = flag.String("Listen", "", "本地网卡监听地址 (format \"0.0.0.0:123\")")
	fAVerify = flag.String("AVerify", "", "监听端的验证字符串，收到客户端发来的验证数据头。")
)

var (
	fFromLocal = flag.String("FromLocal", "0.0.0.0", "转发请求的源地址")
	fToRemote  = flag.String("ToRemote", "", "转发请求的目地址 (format \"22.23.24.25:234\")")
	fBVerify   = flag.String("BVerify", "", "转发端的验证字符串，转发端发去出的验证数据头。")
)

var (
	fTryTime     = flag.String("TryTime", "500ms", "尝试或发起连接时间，可能一方不在线，会间隔尝试连接对方。单位：ns, us, ms, s, m, h")
	fTimeout     = flag.String("Timeout", "5s", "请求远程连接超时。单位：ns, us, ms, s, m, h")
	fMaxConn     = flag.Int("MaxConn", 0, "限制连接最大的数量")
	fBufSize     = flag.Int("BufSize", 4096, "交换数据缓冲大小。单位：字节")
	fKIdleConn   = flag.Int("IdleConn", 0, "保持一方连接数量，以备快速互相连接。")
	fIdleTimeout = flag.String("IdleTimeout", "10s", "空闲连接超时。单位：ns, us, ms, s, m, h")
)

func oa(v string) [][]byte {
	vs := bytes.SplitN([]byte(v), []byte("|"), 2)
	if len(vs) != 2 {
		vs = append(vs, []byte(v))
	}
	return vs
}

func verify(conn net.Conn, v string) bool {
	if v != "" {
		vs := oa(v)

		if _, err := conn.Write(vs[0]); err != nil {
			conn.Close()
			return false
		}

		p := make([]byte, len(vs[1]))
		if n, err := conn.Read(p); err != nil || !bytes.Equal(p[:n], vs[1]) {
			conn.Close()
			fmt.Printf("verify error, %s != %s \n", v, p[:n])
			return false
		}
	}
	return true
}

//commandline:l2d-main.exe -Listen 127.0.0.1:1201 -ToRemote 127.0.0.1:1202 -Network tcp
func main() {
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
		listen = vforward.Option{
			Verify: func(conn net.Conn) bool {
				return verify(conn, *fAVerify)
			},
		}
		dial = vforward.Option{
			Local: &net.TCPAddr{IP: net.ParseIP(*fFromLocal), Port: 0},
			Verify: func(conn net.Conn) bool {
				return verify(conn, *fBVerify)
			},
		}
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
		BufSize:  *fBufSize,
		MaxConn:  *fMaxConn,
		IdleConn: *fKIdleConn,
	}

	// 尝试或发起连接时间，可能一方不在线，会间隔尝试连接对方。
	if ld.TryTime, err = time.ParseDuration(*fTryTime); err != nil {
		log.Println(err)
		return
	}

	// 发起连接超时
	if ld.DialTimeout, err = time.ParseDuration(*fTimeout); err != nil {
		log.Println(err)
		return
	}

	// 空闲连接超时
	if ld.IdleTimeout, err = time.ParseDuration(*fIdleTimeout); err != nil {
		log.Println(err)
		return
	}

	lds, err := ld.New(nil, &listen, &dial)
	if err != nil {
		log.Println(err)
		return
	}

	defer lds.Close()
	if err = lds.Swap(); err != nil {
		log.Printf("错误：%s\n", err)
	}
}
