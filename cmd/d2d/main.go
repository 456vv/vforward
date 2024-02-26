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
	fTryConnTime = flag.String("TryConnTime", "500ms", "尝试或发起连接时间，可能一方不在线，会间隔尝试连接对方。单位：ns, us, ms, s, m, h")
	fTimeout     = flag.String("Timeout", "5s", "请求远程连接超时。单位：ns, us, ms, s, m, h")
	fMaxConn     = flag.Int("MaxConn", 500, "限制连接最大的数量")
	fKeptIdeConn = flag.Int("KeptIdeConn", 2, "保持一方连接数量，以备快速互相连接。")
	fIdeTimeout  = flag.String("IdeTimeout", "0s", "空闲连接超时。单位：ns, us, ms, s, m, h")
	fReadBufSize = flag.Int("ReadBufSize", 4096, "交换数据缓冲大小。单位：字节")
)

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

	var (
		addra = vforward.Addr{Network: *fNetwork, Local: &net.TCPAddr{IP: net.ParseIP(*fALocal), Port: 0}}
		addrb = vforward.Addr{Network: *fNetwork, Local: &net.TCPAddr{IP: net.ParseIP(*fBLocal), Port: 0}}
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

	dd := new(vforward.D2D)

	// 尝试或发起连接时间，可能一方不在线，会间隔尝试连接对方。
	if dd.TryConnTime, err = time.ParseDuration(*fTryConnTime); err != nil {
		log.Println(err)
		return
	}

	// 发起连接超时
	if dd.Timeout, err = time.ParseDuration(*fTimeout); err != nil {
		log.Println(err)
		return
	}

	// 发起连接超时
	d, err := time.ParseDuration(*fIdeTimeout)
	if err != nil {
		log.Println(err)
		return
	}
	dd.IdeTimeout(d)
	dd.MaxConn(*fMaxConn)
	dd.KeptIdeConn(*fKeptIdeConn)
	dd.ReadBufSize = *fReadBufSize // 交换数据缓冲大小

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

			if _, err := conn.Write(vs[0]); err != nil {
				conn.Close()
				return false
			}

			p := make([]byte, len(vs[1]))
			if n, err := conn.Read(p); err != nil || !bytes.Equal(p, vs[1]) {
				conn.Close()
				fmt.Printf("verify error, %s != %s \n", v, p[:n])
				return false
			}
		}
		return true
	}
	dd.Verify(func(conn net.Conn) bool {
		return verify(conn, *fAVerify)
	}, func(conn net.Conn) bool {
		return verify(conn, *fBVerify)
	})

	defer dd.Close()
	dds, err := dd.Transport(&addra, &addrb)
	if err != nil {
		log.Println(err)
		return
	}
	defer dds.Close()
	if err = dds.Swap(); err != nil {
		log.Printf("错误：%s\n", err)
	}
}
