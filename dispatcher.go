package zalobot

import (
	"context"
	"sync"
	"sync/atomic"
)

type dispatchConfig struct {
	workers     int
	quantum     int
	perChat     int
	maxBuffered int
}

type chatQueue struct {
	items  []Update
	active bool
}

type dispatcher struct {
	mu        sync.Mutex
	cond      *sync.Cond
	queues    map[string]*chatQueue
	ready     []string
	total     int
	stopped   bool
	cfg       dispatchConfig
	wg        sync.WaitGroup
	startOnce sync.Once
	handle    func(Update)

	updatesCh   chan Update
	updatesOn   *atomic.Bool
	onError     func(error)
	dropEpisode atomic.Bool
}

func newDispatcher(cfg dispatchConfig, updatesCh chan Update, updatesOn *atomic.Bool, onError func(error), handle func(Update)) *dispatcher {
	d := &dispatcher{
		queues:    make(map[string]*chatQueue),
		cfg:       cfg,
		updatesCh: updatesCh,
		updatesOn: updatesOn,
		onError:   onError,
		handle:    handle,
	}
	d.cond = sync.NewCond(&d.mu)
	return d
}

func chatKey(u Update) string {
	if u.Message != nil && u.Message.Chat != nil && u.Message.Chat.ID != "" {
		return u.Message.Chat.ID
	}
	return "event:" + string(u.EventName)
}

func (d *dispatcher) admitNonBlocking(u Update) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.admitLocked(u, nil)
}

func (d *dispatcher) admitBlocking(ctx context.Context, u Update) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	stop := context.AfterFunc(ctx, func() {
		d.mu.Lock()
		d.cond.Broadcast()
		d.mu.Unlock()
	})
	defer stop()
	return d.admitLocked(u, ctx)
}

func (d *dispatcher) admitLocked(u Update, ctx context.Context) error {
	key := chatKey(u)
	for {
		if d.stopped {
			return ErrStopped
		}
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		cq := d.queues[key]
		if cq == nil {
			cq = &chatQueue{}
			d.queues[key] = cq
		}
		if len(cq.items) < d.cfg.perChat && d.total < d.cfg.maxBuffered {
			cq.items = append(cq.items, u)
			d.total++
			if !cq.active {
				d.ready = append(d.ready, key)
				d.cond.Broadcast()
			}
			d.startOnce.Do(d.start)
			return nil
		}
		if ctx == nil {
			return ErrQueueFull
		}
		d.cond.Wait()
	}
}

func (d *dispatcher) start() {
	for i := 0; i < d.cfg.workers; i++ {
		d.wg.Add(1)
		go d.worker()
	}
}

func (d *dispatcher) worker() {
	defer d.wg.Done()
	for {
		d.mu.Lock()
		for len(d.ready) == 0 && !d.stopped {
			d.cond.Wait()
		}
		if len(d.ready) == 0 && d.stopped {
			d.mu.Unlock()
			return
		}
		key := d.ready[0]
		d.ready = d.ready[1:]
		cq := d.queues[key]
		if cq == nil || len(cq.items) == 0 {
			d.mu.Unlock()
			continue
		}
		cq.active = true
		n := d.cfg.quantum
		if n < 1 {
			n = 1
		}
		batch := make([]Update, 0, n)
		for len(cq.items) > 0 && len(batch) < n {
			batch = append(batch, cq.items[0])
			cq.items = cq.items[1:]
			d.total--
		}
		d.cond.Broadcast()
		d.mu.Unlock()

		for _, u := range batch {
			d.handle(u)
			d.fanout(u)
		}

		d.mu.Lock()
		if len(cq.items) > 0 {
			d.ready = append(d.ready, key)
			d.cond.Broadcast()
		} else {
			cq.active = false
			delete(d.queues, key)
		}
		d.mu.Unlock()
	}
}

func (d *dispatcher) fanout(u Update) {
	if d.updatesOn == nil || !d.updatesOn.Load() {
		return
	}
	select {
	case d.updatesCh <- u:
		d.dropEpisode.Store(false)
	default:
		if d.dropEpisode.CompareAndSwap(false, true) {
			if d.onError != nil {
				d.onError(ErrUpdatesDropped)
			}
		}
	}
}

func (d *dispatcher) stop() {
	d.mu.Lock()
	d.stopped = true
	d.cond.Broadcast()
	d.mu.Unlock()
}

func (d *dispatcher) wait() { d.wg.Wait() }
