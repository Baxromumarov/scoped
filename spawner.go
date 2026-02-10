package scoped

import (
	"context"
	"sync/atomic"
	"time"
)

// Spawner allows spawning concurrent tasks into a scope.
type Spawner interface {
	// Go starts a new concurrent task with the given name.
	// The task function receives a child Spawner allowing it to create sub-tasks.
	Go(name string, fn TaskFunc)
}

// spawner implements the Spawner interface and manages the lifecycle of tasks.
type spawner struct {
	s    *scope
	open atomic.Bool
}

// Go implements Spawner.Go.

func (sp *spawner) Go(name string, fn TaskFunc) {
	sp.s.wg.Add(1)

	if !sp.open.Load() {
		sp.s.wg.Done() // undo the Add for this task
		panic("scoped: Go called after scope shutdown")
	}

	info := TaskInfo{Name: name}

	go func() {
		defer sp.s.wg.Done()

		// semaphore
		if sp.s.sem != nil {
			select {
			case sp.s.sem <- struct{}{}:
				defer func() { <-sp.s.sem }()
			case <-sp.s.ctx.Done():
				sp.s.recordError(info, sp.s.ctx.Err())
				return
			}
		}

		if sp.s.cfg.onStart != nil {
			sp.s.cfg.onStart(info)
		}

		if sp.s.ctx.Err() != nil {
			panic("scoped: Go called after scope canceled")
		}

		//child spawner is valid only for the lifetime of the task;
		// spawning after the task function returns will panic.
		child := &spawner{
			s:    sp.s,
			open: atomic.Bool{},
		}
		child.open.Store(true)

		start := time.Now()
		err := sp.s.exec(func(ctx context.Context) error {
			return fn(ctx, child)
		})
		elapsed := time.Since(start)

		child.close()

		if sp.s.cfg.onDone != nil {
			sp.s.cfg.onDone(info, err, elapsed)
		}

		if err != nil {
			sp.s.recordError(info, err)
		}
	}()
}

// close marks the spawner as closed, preventing further Go calls.
func (sp *spawner) close() {
	sp.open.Store(false)
}
