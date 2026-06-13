package vforward

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic" // 引入 atomic 包
	"time"

	"github.com/456vv/vconnpool/v3"
	"github.com/libp2p/go-reuseport"
)

// D2DSwap 数据交换处理器
type D2DSwap struct {
	aAddr       net.Addr
	acp         *vconnpool.Pool
	bAddr       net.Addr
	bcp         *vconnpool.Pool
	dd          *D2D
	ctx         context.Context
	conns       sync.Map // 代替 vmap，使用原生线程安全Map
	wg          sync.WaitGroup
	closed      atomic.Bool
	runed       atomic.Bool
	connCounter atomic.Int64 // 补充：用于统计活跃连接数
}

func (T *D2DSwap) ConnNum() int32 {
	return int32(T.connCounter.Load())
}

// Swap 开始交换数据
func (T *D2DSwap) Swap() error {
	if T.runed.Swap(true) {
		return errors.New("vforward: 交换服务已在运行")
	}
	var wg sync.WaitGroup
	var notClosed1, notClosed2 bool
	for !T.closed.Load() {
		wg.Go(func() {
			select {
			case <-T.ctx.Done():
			case notClosed1 = <-T.acp.WaitConnIdleGeq(T.aAddr, 1):
			}
		})
		wg.Go(func() {
			select {
			case <-T.ctx.Done():
			case notClosed2 = <-T.bcp.WaitConnIdleGeq(T.bAddr, 1):
			}
		})
		wg.Wait()
		if !notClosed1 || !notClosed2 {
			return errors.New("vforward: 交换服务的连接池已关闭")
		}

		select {
		case <-T.ctx.Done():
			return T.ctx.Err()
		default:
			if T.closed.Load() {
				return nil
			}
			// 强制从池中读取出来，避免阻塞
			ctx := context.WithValue(T.ctx, vconnpool.PriorityContextKey, false)
			conna, err := T.acp.DialContext(ctx, T.aAddr.Network(), T.aAddr.String())
			if err != nil {
				continue
			}

			connb, err := T.bcp.DialContext(ctx, T.bAddr.Network(), T.bAddr.String())
			if err != nil {
				conna.Close()
				continue
			}

			T.wg.Add(1)
			T.connCounter.Add(1)

			conna.Discard()
			connb.Discard()
			T.conns.Store(conna, connb)
			go T.dataCopy(conna, connb)
		}
	}
	return nil
}

func (T *D2DSwap) dataCopy(conna, connb net.Conn) {
	defer T.connCounter.Add(-1)
	defer conna.Close()
	defer connb.Close()
	defer T.conns.Delete(conna)

	if T.closed.Load() {
		T.wg.Done()
		return
	}

	T.wg.Done()

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

	// 关闭连接池，这将导致 Swap 循环退出
	T.acp.Close()
	T.bcp.Close()

	// 等待所有 dataCopy goroutine 完成
	T.wg.Wait()

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
	TryTime     time.Duration
	Timeout     time.Duration
	MaxIdleConn int
	MaxConn     int
	BufSize     int
}

// New 创建一个新的 D2DSwap 实例
func (T *D2D) New(ctx context.Context, a, b *Option) (*D2DSwap, error) {
	if T.MaxConn == 0 {
		return nil, errors.New("MaxConn cannot be zero")
	}
	d2ds := &D2DSwap{
		ctx:   ctx,
		dd:    T,
		aAddr: a.Remote,
		bAddr: b.Remote,
		acp:   T.newConnPool(a),
		bcp:   T.newConnPool(b),
	}
	// 启动后台连接维持 goroutine
	go T.keepAliveLoop(ctx, d2ds.acp, a)
	go T.keepAliveLoop(ctx, d2ds.bcp, b)
	return d2ds, nil
}

// newConnPool 根据 Option 创建连接池
func (T *D2D) newConnPool(a *Option) *vconnpool.Pool {
	cp := &vconnpool.Pool{
		Dialer: &net.Dialer{
			LocalAddr: a.Local,
			Control:   reuseport.Control, // 启用 SO_REUSEPORT
			Timeout:   T.Timeout,
		},
	}
	return cp
}

// keepAliveLoop 维护连接池中的连接数量
func (T *D2D) keepAliveLoop(ctx context.Context, cp *vconnpool.Pool, opt *Option) {
	var tickerC <-chan time.Time
	if T.TryTime > 0 {
		ticker := time.NewTicker(T.TryTime)
		defer ticker.Stop()
		tickerC = ticker.C
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-cp.WaitConnNumLeq(opt.Remote, T.MaxConn-1):
			if tickerC != nil {
				<-tickerC
			}
			// 检查当前连接数是否小于最大空闲连接数
			if T.MaxIdleConn == 0 {
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
				if ok := <-cp.WaitConnIdleLeq(opt.Remote, T.MaxIdleConn-1); !ok {
					// 池被关闭了
					return
				}
				go T.dialAndPut(ctx, cp, opt) // 在 goroutine 中执行
			}
		}
	}
}

// dialAndPut 拨号并放入连接池
func (T *D2D) dialAndPut(ctx context.Context, cp *vconnpool.Pool, opt *Option) {
	if T.Timeout > 0 {
		var cancel func()
		ctx, cancel = context.WithTimeout(ctx, T.Timeout)
		defer cancel()
	}

	ctx = context.WithValue(ctx, vconnpool.PriorityContextKey, true)
	conn, err := cp.DialContext(ctx, opt.Remote.Network(), opt.Remote.String())
	if err != nil {
		return
	}
	defer conn.Close()

	if opt.Verify != nil && !opt.Verify(conn) {
		conn.Discard()
	}
}
