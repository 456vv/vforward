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
	fListen  = flag.String("Listen", "", "本地网卡监听地址 (format \"0.0.0.0:123\")")
	fAVerify = flag.String("AVerify", "", "监听端的验证字符串，收到客户端发来的验证数据头。")
)

var (
	fFromLocal = flag.String("FromLocal", "0.0.0.0", "转发请求的源地址")
	fToRemote  = flag.String("ToRemote", "", "转发请求的目地址 (format \"22.23.24.25:234\")")
	fBVerify   = flag.String("BVerify", "", "转发端的验证字符串，转发端发去出的验证数据头。")
)

var (
	fTimeout     = flag.String("Timeout", "5s", "请求远程连接超时。单位：ns, us, ms, s, m, h")
	fMaxConn     = flag.Int("MaxConn", 0, "限制连接最大的数量")
	fReadBufSize = flag.Int("ReadBufSize", 4096, "交换数据缓冲大小。单位：字节")
)

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
		listen = vforward.Addr{Network: *fNetwork}
		dial   = vforward.Addr{Network: *fNetwork, Local: &net.TCPAddr{IP: net.ParseIP(*fFromLocal), Port: 0}}
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

	ld := new(vforward.L2D)

	// 发起连接超时
	if ld.Timeout, err = time.ParseDuration(*fTimeout); err != nil {
		log.Println(err)
		return
	}
	ld.ReadBufSize = *fReadBufSize // 交换数据缓冲大小
	ld.MaxConn(*fMaxConn)

	oa := func(v string) [][]byte {
		vs := bytes.SplitN([]byte(v), []byte("|"), 2)
		if len(vs) != 2 {
			vs = append(vs, []byte(v))
		}
		return vs
	}
	ld.Verify(func(conn net.Conn) bool {
		v := *fAVerify
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
	}, func(conn net.Conn) bool {
		v := *fBVerify
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
	})

	defer ld.Close()
	lds, err := ld.Transport(&listen, &dial)
	if err != nil {
		log.Println(err)
		return
	}

	defer lds.Close()
	if err = lds.Swap(); err != nil {
		log.Printf("错误：%s\n", err)
	}
}
