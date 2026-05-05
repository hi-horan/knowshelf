package idgen

import (
	"sync/atomic"
	"time"
)

type Generator struct {
	lastMicro atomic.Int64
	now       func() time.Time
}

func New() *Generator {
	return &Generator{now: time.Now}
}

func (g *Generator) Next() int64 {
	for {
		last := g.lastMicro.Load()
		micro := g.now().UnixMicro()
		if micro <= last {
			micro = last + 1
		}
		if g.lastMicro.CompareAndSwap(last, micro) {
			return micro
		}
	}
}
