package vforward

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic" // 引入 atomic 包
	"time"

	"github.com/456vv/vconnpool/v3"
)

// D2DSwap 数据交换处理器
type D2DSwap struct {
	aOpt      *Option
	acp       *vconnpool.Pool
	bOpt      *Option
	bcp       *vconnpool.Pool
	dd        D2D
	ctx       context.Context
	cancel    func()
	conns     sync.Map // 代替 vmap，使用原生线程安全Map
	closed    atomic.Bool
	running   atomic.Bool
	connCount atomic.Int64 // 补充：用于统计活跃连接数
}

func (T *D2DSwap) ConnNum() int32 {
	return int32(T.connCount.Load())
}

func (T *D2DSwap) keepAliveLoop(ctx context.Context, cp *vconnpool.Pool, opt *Option) {
	var tickerC <-chan time.Time
	if T.dd.TryTime > 0 {
		ticker := time.NewTicker(T.dd.TryTime)
		defer ticker.Stop()
		tickerC = ticker.C
	}

	ctx = context.WithValue(ctx, vconnpool.PriorityContextKey, true)
	for {
		select {
		case <-ctx.Done():
			return
		case ok := <-cp.WaitConnNumLeq(opt.Remote, T.dd.MaxConn-1):
			if !ok {
				return
			}
			if tickerC != nil {
				<-tickerC
			}
			// 检查当前连接数是否小于最大空闲连接数
			if T.dd.IdleConn == 0 {
				// MaxIdleConn为0，默认仅保留一条空闲连接。
				// 当池中所有连接都被使用或断开时，尝试拨号一个新连接。
				if ok := <-cp.WaitConnIdleLeq(opt.Remote, 0); !ok {
					// 池被关闭了
					return
				}
				T.dialAndPut(ctx, cp, opt)
			} else {
				// MaxIdleConn不为0，就保持指定数量的空闲连接。
				// 当空闲连接数低于 MaxIdleConn-1 时（即需要补充时），尝试拨号。
				if ok := <-cp.WaitConnIdleLeq(opt.Remote, T.dd.IdleConn-1); !ok {
					// 池被关闭了
					return
				}
				go T.dialAndPut(ctx, cp, opt) // 在 goroutine 中执行
			}
		}
	}
}

// dialAndPut 拨号并放入连接池
func (T *D2DSwap) dialAndPut(ctx context.Context, cp *vconnpool.Pool, opt *Option) {
	conn, err := cp.DialContext(ctx, opt.Remote.Network(), opt.Remote.String())
	if err != nil {
		return
	}
	defer conn.Close()

	if opt.Verify != nil && !opt.Verify(conn) {
		conn.Discard()
	}
}

func (T *D2DSwap) pairConnections() error {
	for {
		var wg sync.WaitGroup
		wg.Go(func() {
			select {
			case <-T.ctx.Done():
			case <-T.acp.WaitConnIdleGeq(T.aOpt.Remote, 1):
			}
		})
		wg.Go(func() {
			select {
			case <-T.ctx.Done():
			case <-T.bcp.WaitConnIdleGeq(T.bOpt.Remote, 1):
			}
		})
		wg.Wait()

		if T.ctx.Err() != nil {
			T.Close()
			return nil
		}

		// 强制从池中读取出来，避免阻塞
		ctx := context.WithValue(T.ctx, vconnpool.PriorityContextKey, false)
		conna, err := T.acp.DialContext(ctx, T.aOpt.Remote.Network(), T.aOpt.Remote.String())
		if err != nil {
			continue
		}

		connb, err := T.bcp.DialContext(ctx, T.bOpt.Remote.Network(), T.bOpt.Remote.String())
		if err != nil {
			conna.Close()
			continue
		}

		conna.Discard()
		connb.Discard()
		T.conns.Store(conna, connb)
		T.connCount.Add(1)
		go T.dataCopy(conna, connb)
	}
}

func (T *D2DSwap) Swap() error {
	if T.running.Swap(true) {
		return errors.New("vforward: 交换服务已在运行")
	}
	if err := T.ctx.Err(); err != nil {
		return err
	}

	// 启动后台连接维持 goroutine
	go T.keepAliveLoop(T.ctx, T.acp, T.aOpt)
	go T.keepAliveLoop(T.ctx, T.bcp, T.bOpt)

	return T.pairConnections()
}

func (T *D2DSwap) dataCopy(conna, connb net.Conn) {
	defer T.conns.Delete(conna)
	defer T.connCount.Add(-1)
	defer conna.Close()
	defer connb.Close()

	if T.ctx.Err() != nil {
		return
	}

	// 双向复制
	var cpWg sync.WaitGroup
	cpWg.Go(func() {
		// 方向 A -> B
		copyData(connb, conna, T.dd.BufSize)
	})
	cpWg.Go(func() {
		// 方向 B -> A
		copyData(conna, connb, T.dd.BufSize)
	})

	// 等待任意一端关闭
	cpWg.Wait()
}

// Close 停止数据交换并关闭所有连接
func (T *D2DSwap) Close() error {
	if T.closed.Swap(true) {
		return nil
	}

	T.cancel()

	// 关闭连接池，这将导致 Swap 循环退出
	T.acp.Close()
	T.bcp.Close()

	// 遍历并关闭所有仍在 map 中的连接，作为双重保险
	T.conns.Range(func(k, v any) bool {
		k.(net.Conn).Close()
		v.(net.Conn).Close()
		return true
	})

	return nil
}

// D2D 主结构体
type D2D struct {
	BufSize     int
	TryTime     time.Duration
	DialTimeout time.Duration
	MaxConn     int
	IdleConn    int
	IdleTimeout time.Duration
}

// New 创建一个新的 D2DSwap 实例
func (T D2D) New(ctx context.Context, a, b *Option) (*D2DSwap, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if a == nil || b == nil || a.Remote == nil || b.Remote == nil {
		return nil, errors.New("vforward: 选项或远程地址不能为零")
	}
	if T.MaxConn == 0 {
		return nil, errors.New("MaxConn cannot be zero")
	}
	d2ds := &D2DSwap{
		dd:   T,
		aOpt: a,
		bOpt: b,
		acp:  T.newConnPool(a),
		bcp:  T.newConnPool(b),
	}

	d2ds.ctx, d2ds.cancel = context.WithCancel(ctx)
	return d2ds, nil
}

// newConnPool 根据 Option 创建连接池
func (T D2D) newConnPool(a *Option) *vconnpool.Pool {
	if T.DialTimeout == 0 {
		T.DialTimeout = 30 * time.Second
	}
	cp := &vconnpool.Pool{
		IdleConn:    T.IdleConn,
		IdleTimeout: T.IdleTimeout,
		MaxConn:     T.MaxConn,
		Dialer: &net.Dialer{
			LocalAddr: a.Local,
			Timeout:   T.DialTimeout,
		},
	}
	return cp
}
