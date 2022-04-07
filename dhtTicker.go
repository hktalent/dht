package dht

import (
	_ "embed"
	"time"
)

// 定义函数类型
type Fn func()

// 定时器中的成员
type MyTicker struct {
	MyTick *time.Ticker
	Runner Fn
	stop   chan struct{}
}

func NewMyTick(interval int, f Fn) *MyTicker {
	return &MyTicker{
		MyTick: time.NewTicker(time.Duration(interval) * time.Second),
		Runner: f,
		stop:   make(chan struct{}),
	}
}

func (t *MyTicker) Stop() {
	close(t.stop)
}

// 启动定时器需要执行的任务
func (t *MyTicker) Start() {
	for {
		select {
		case <-t.stop:
			break
		case <-t.MyTick.C:
			t.Runner()
		}
	}
}
