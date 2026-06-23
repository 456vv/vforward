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

var (
	fNetwork = flag.String("Network", "tcp", "网络地址类型")
	fALocal  = flag.String("ALocal", "", "A本地监听网卡IP地址 (format \"12.13.14.15:123\")")
	fAVerify = flag.String("AVerify", "", "A的验证字符串，桥接后客户端发来的验证数据头。")
	fBLocal  = flag.String("BLocal", "", "B本地监听网卡IP地址 (format \"22.23.24.25:234\")")
	fBVerify = flag.String("BVerify", "", "B的验证字符串，桥接后客户端发来的验证数据头。")
)

var (
	fMaxConn     = flag.Int("MaxConn", 0, "限制连接最大的数量")
	fIdleConn    = flag.Int("IdleConn", 0, "保持一方连接数量，以备快速互相连接。")
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

//commandline:l2l-main.exe -ALocal 127.0.0.1:1201 -BLocal 127.0.0.1:1202 -Network tcp
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

	ll := &vforward.L2L{
		BufSize:  *fBufSize,
		MaxConn:  *fMaxConn,
		IdleConn: *fIdleConn,
	}
	// 空闲连接超时
	if ll.IdleTimeout, err = time.ParseDuration(*fIdleTimeout); err != nil {
		log.Println(err)
		return
	}

	var (
		addra = vforward.Option{
			Local: addr1,
			Verify: func(conn net.Conn) bool {
				return verify(conn, *fAVerify, ll.IdleTimeout)
			},
		}
		addrb = vforward.Option{
			Local: addr2,
			Verify: func(conn net.Conn) bool {
				return verify(conn, *fBVerify, ll.IdleTimeout)
			},
		}
	)

	lls, err := ll.New(nil, &addra, &addrb)
	if err != nil {
		log.Println(err)
		return
	}

	defer lls.Close()
	if err = lls.Swap(); err != nil {
		log.Printf("错误：%s\n", err)
	}
}
