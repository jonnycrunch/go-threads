package db

import (
	"context"
	"sync"
	"time"

	format "github.com/ipfs/go-ipld-format"
	"github.com/libp2p/go-libp2p-core/peer"
	threadcbor "github.com/textileio/go-threads/cbor"
	"github.com/textileio/go-threads/core/net"
	"github.com/textileio/go-threads/core/thread"
)

const (
	addRecordTimeout  = time.Second * 10
	fetchEventTimeout = time.Second * 15
)

// SingleThreadAdapter connects a DB with a Service
type singleThreadAdapter struct {
	net         net.Net
	db          *DB
	threadCreds thread.Credentials
	ownLogID    peer.ID
	closeChan   chan struct{}
	goRoutines  sync.WaitGroup

	lock    sync.Mutex
	started bool
	closed  bool
}

// NewSingleThreadAdapter returns a new Adapter which maps
// a DB with a single Thread
func newSingleThreadAdapter(db *DB, n net.Net, creds thread.Credentials) *singleThreadAdapter {
	return &singleThreadAdapter{
		net:         n,
		threadCreds: creds,
		db:          db,
		closeChan:   make(chan struct{}),
	}
}

// Close closes the db thread and stops listening both directions
// of thread<->db
func (a *singleThreadAdapter) Close() {
	a.lock.Lock()
	defer a.lock.Unlock()
	if a.closed {
		return
	}
	a.closed = true
	close(a.closeChan)
	a.goRoutines.Wait()
}

// Start starts connection from DB to Service, and viceversa
func (a *singleThreadAdapter) Start() {
	a.lock.Lock()
	defer a.lock.Unlock()
	if a.started {
		return
	}
	a.started = true
	li, err := a.net.GetThread(context.Background(), a.threadCreds)
	if err != nil {
		log.Fatalf("error getting thread %s: %v", a.threadCreds.ThreadID(), err)
	}
	if ownLog := li.GetOwnLog(); ownLog != nil {
		a.ownLogID = ownLog.ID
	} else {
		log.Fatalf("error getting own log for thread %s: %v", a.threadCreds.ThreadID(), err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go a.threadToDB(&wg)
	go a.dbToThread(&wg)
	wg.Wait()
	a.goRoutines.Add(2)
}

func (a *singleThreadAdapter) threadToDB(wg *sync.WaitGroup) {
	defer a.goRoutines.Done()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub, err := a.net.Subscribe(ctx, net.WithCredentials(a.threadCreds))
	if err != nil {
		log.Fatalf("error getting thread subscription: %v", err)
	}
	wg.Done()
	for {
		select {
		case <-a.closeChan:
			log.Debugf("closing thread-to-db flow on thread %s", a.threadCreds.ThreadID())
			return
		case rec, ok := <-sub:
			if !ok {
				log.Debug("notification channel closed, not listening to external changes anymore")
				return
			}
			if rec.LogID() == a.ownLogID {
				continue // Ignore our own events since DB already dispatches to DB reducers
			}
			ctx, cancel := context.WithTimeout(context.Background(), fetchEventTimeout)
			event, err := threadcbor.EventFromRecord(ctx, a.net, rec.Value())
			if err != nil {
				block, err := a.getBlockWithRetry(ctx, rec.Value(), 3, time.Millisecond*500)
				if err != nil { // @todo: Buffer them and retry...
					log.Fatalf("error when getting block from record: %v", err)
				}
				event, err = threadcbor.EventFromNode(block)
				if err != nil {
					log.Fatalf("error when decoding block to event: %v", err)
				}
			}
			info, err := a.net.GetThread(ctx, a.threadCreds)
			if err != nil {
				log.Fatalf("error when getting info for thread %s: %v", a.threadCreds.ThreadID(), err)
			}
			if !info.Key.CanRead() {
				log.Fatalf("read key not found for thread %s/%s", a.threadCreds.ThreadID(), rec.LogID())
			}
			node, err := event.GetBody(ctx, a.net, info.Key.Read())
			if err != nil {
				log.Fatalf("error when getting body of event on thread %s/%s: %v", a.threadCreds.ThreadID(), rec.LogID(), err)
			}
			dbEvents, err := a.db.eventsFromBytes(node.RawData())
			if err != nil {
				log.Fatalf("error when unmarshaling event from bytes: %v", err)
			}
			log.Debugf("dispatching to db external new record: %s/%s", rec.ThreadID(), rec.LogID())
			if err := a.db.dispatch(dbEvents); err != nil {
				log.Fatal(err)
			}
			cancel()
		}
	}
}

func (a *singleThreadAdapter) dbToThread(wg *sync.WaitGroup) {
	defer a.goRoutines.Done()
	l := a.db.localEventListen()
	defer l.Discard()
	wg.Done()

	for {
		select {
		case <-a.closeChan:
			log.Debugf("closing db-to-thread flow on thread %s", a.threadCreds.ThreadID())
			return
		case node, ok := <-l.Channel():
			if !ok {
				log.Errorf("ending sending db local event to own thread since channel was closed for thread %s", a.threadCreds.ThreadID())
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), addRecordTimeout)
			if _, err := a.net.CreateRecord(ctx, a.threadCreds, node); err != nil {
				log.Fatalf("error writing record: %v", err)
			}
			cancel()
		}
	}
}

func (a *singleThreadAdapter) getBlockWithRetry(ctx context.Context, rec net.Record, cantRetries int, backoffTime time.Duration) (format.Node, error) {
	var err error
	for i := 1; i <= cantRetries; i++ {
		n, err := rec.GetBlock(ctx, a.net)
		if err == nil {
			return n, nil
		}
		log.Warnf("error when fetching block %s in retry %d", rec.Cid(), i)
		time.Sleep(backoffTime)
		backoffTime *= 2
	}
	return nil, err
}
