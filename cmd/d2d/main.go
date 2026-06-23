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
	fALocal  = flag.String("ALocal", "0.0.0.0", "A端本地发起连接地址")
	fARemote = flag.String("ARemote", "", "A端远程请求连接地址 (format \"12.13.14.15:123\")")
	fAVerify = flag.String("AVerify", "", "A的验证字符串，桥接后的发出的第一条验证数据头。")
)

var (
	fBLocal  = flag.String("BLocal", "0.0.0.0", "B端本地发起连接地址")
	fBRemote = flag.String("BRemote", "", "B端远程请求连接地址 (format \"22.23.24.25:234\")")
	fBVerify = flag.String("BVerify", "", "B的验证字符串，桥接后的发出的第一条验证数据头。")
)

var (
	fTryTime     = flag.String("TryTime", "500ms", "尝试或发起连接时间，可能一方不在线，会间隔尝试连接对方。单位：ns, us, ms, s, m, h")
	fTimeout     = flag.String("Timeout", "5s", "请求远程连接超时。单位：ns, us, ms, s, m, h")
	fMaxConn     = flag.Int("MaxConn", 0, "限制连接最大的数量")
	fKIdleConn   = flag.Int("IdleConn", 0, "保持一方连接数量，以备快速互相连接。")
	fIdleTimeout = flag.String("IdleTimeout", "10s", "空闲连接超时。单位：ns, us, ms, s, m, h")
	fBufSize     = flag.Int("BufSize", 4096, "交换数据缓冲大小。单位：字节")
)

func oa(v string) [][]byte {
	vs := bytes.SplitN([]byte(v), []byte("|"), 2)
	if len(vs) != 2 {
		vs = append(vs, nil)
	}
	return vs
}

func verify(conn net.Conn, v string, timeout time.Duration) bool {
	if v != "" {
		vs := oa(v)
		if timeout > 0 {
			conn.SetDeadline(time.Now().Add(timeout))
		}
		if _, err := conn.Write(vs[0]); err != nil {
			conn.Close()
			return false
		}

		if vs[1] != nil {
			p := make([]byte, len(vs[1]))
			if n, err := conn.Read(p); err != nil || !bytes.Equal(p[:n], vs[1]) {
				conn.Close()
				fmt.Printf("verify error, %s != %s \n", v, p[:n])
				return false
			}
		}
	}
	return true
}

//commandline:d2d-main.exe -ARemote 127.0.0.1:1201 -BRemote 127.0.0.1:1202 -Network udp
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
	if *fARemote == "" || *fBRemote == "" {
		log.Printf("地址未填，A端远程地址 %q, B端远程地址 %q", *fARemote, *fBRemote)
		return
	}

	dd := vforward.D2D{
		BufSize:  *fBufSize,
		MaxConn:  *fMaxConn,
		IdleConn: *fKIdleConn,
	}

	// 尝试或发起连接时间，可能一方不在线，会间隔尝试连接对方。
	if dd.TryTime, err = time.ParseDuration(*fTryTime); err != nil {
		log.Println(err)
		return
	}

	// 发起连接超时
	if dd.DialTimeout, err = time.ParseDuration(*fTimeout); err != nil {
		log.Println(err)
		return
	}

	// 空闲连接超时
	if dd.IdleTimeout, err = time.ParseDuration(*fIdleTimeout); err != nil {
		log.Println(err)
		return
	}

	var (
		addra = vforward.Option{
			Local: &net.TCPAddr{IP: net.ParseIP(*fALocal), Port: 0},
			Verify: func(conn net.Conn) bool {
				return verify(conn, *fAVerify, dd.IdleTimeout)
			},
		}
		addrb = vforward.Option{
			Local: &net.TCPAddr{IP: net.ParseIP(*fBLocal), Port: 0},
			Verify: func(conn net.Conn) bool {
				return verify(conn, *fBVerify, dd.IdleTimeout)
			},
		}
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

	dds, err := dd.New(nil, &addra, &addrb)
	if err != nil {
		log.Println(err)
		return
	}
	defer dds.Close()
	if err = dds.Swap(); err != nil {
		log.Printf("错误：%s\n", err)
	}
}
