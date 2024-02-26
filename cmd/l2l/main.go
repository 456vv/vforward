package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/456vv/vforward"
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
	fKeptIdeConn = flag.Int("KeptIdeConn", 2, "保持一方连接数量，以备快速互相连接。")
	fIdeTimeout  = flag.String("IdeTimeout", "0s", "空闲连接超时。单位：ns, us, ms, s, m, h")
	fReadBufSize = flag.Int("ReadBufSize", 4096, "交换数据缓冲大小。单位：字节")
)

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
	addra := &vforward.Addr{
		Network: *fNetwork,
		Local:   addr1,
	}
	addrb := &vforward.Addr{
		Network: *fNetwork,
		Local:   addr2,
	}

	ll := new(vforward.L2L)
	// 空闲连接超时
	d, err := time.ParseDuration(*fIdeTimeout)
	if err != nil {
		log.Println(err)
		return
	}
	ll.IdeTimeout(d)
	ll.MaxConn(*fMaxConn)
	ll.KeptIdeConn(*fKeptIdeConn)
	ll.ReadBufSize = *fReadBufSize // 交换数据缓冲大小

	oa := func(v string) [][]byte {
		vs := bytes.SplitN([]byte(v), []byte("|"), 2)
		if len(vs) != 2 {
			vs = append(vs, []byte(v))
		}
		return vs
	}
	verify := func(conn net.Conn, v string) bool {
		if v != "" {
			vs := oa(v)

			p := make([]byte, len(vs[0]))
			if n, err := conn.Read(p); err != nil || !bytes.Equal(p[:n], vs[0]) {
				conn.Close()
				fmt.Printf("verify error, %s != %s \n", v, p[:n])
				return false
			}

			if _, err := conn.Write(vs[1]); err != nil {
				conn.Close()
				return false
			}

		}
		return true
	}
	ll.Verify(func(a net.Conn) bool {
		return verify(a, *fAVerify)
	}, func(b net.Conn) bool {
		return verify(b, *fBVerify)
	})

	defer ll.Close()
	lls, err := ll.Transport(addra, addrb)
	if err != nil {
		log.Println(err)
		return
	}

	defer lls.Close()
	if err = lls.Swap(); err != nil {
		log.Printf("错误：%s\n", err)
	}
}
