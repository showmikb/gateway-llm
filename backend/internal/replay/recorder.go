package replay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gateway-llm/gateway-llm/internal/db"
	"github.com/gateway-llm/gateway-llm/internal/ir"
	"github.com/gateway-llm/gateway-llm/internal/models"
	"go.uber.org/zap"
)

// Recorder captures request/response pairs for later replay and online eval.
// It is deliberately async: the hot path calls Enqueue() and returns; a
// background worker writes blobs and rows. Losing a few recordings under
// load is preferable to blocking real requests.
type Recorder struct {
	store   BlobStore
	db      *db.DB
	logger  *zap.Logger
	queue   chan *PendingRecording
	workers int
	dropped atomic.Uint64
	wg      sync.WaitGroup
	stopped atomic.Bool
}

// PendingRecording is the bundle the hot path hands off. Response can be
// nil/empty when a request failed before a response was produced — we still
// want to record the attempt.
type PendingRecording struct {
	Recording *models.Recording
	RequestIR *ir.ChatRequest
	ResponseIR *ir.ChatResponse
}

func NewRecorder(store BlobStore, database *db.DB, logger *zap.Logger) *Recorder {
	return NewRecorderWithWorkers(store, database, logger, 4, 4096)
}

func NewRecorderWithWorkers(store BlobStore, database *db.DB, logger *zap.Logger, workers, queueSize int) *Recorder {
	if logger == nil {
		logger = zap.NewNop()
	}
	r := &Recorder{
		store:   store,
		db:      database,
		logger:  logger,
		queue:   make(chan *PendingRecording, queueSize),
		workers: workers,
	}
	for i := 0; i < workers; i++ {
		r.wg.Add(1)
		go r.run()
	}
	return r
}

// Enqueue drops the recording into the queue, returning immediately.
// If the queue is full we drop rather than block — the hot path must not
// wait on recording I/O. The dropped counter is exposed via metrics so an
// operator can see when capacity needs to grow.
func (r *Recorder) Enqueue(p *PendingRecording) {
	if r == nil || r.stopped.Load() {
		return
	}
	select {
	case r.queue <- p:
	default:
		r.dropped.Add(1)
	}
}

// Stop closes the queue and drains outstanding work. Safe to call at
// shutdown from the server's graceful-shutdown path.
func (r *Recorder) Stop(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if !r.stopped.CompareAndSwap(false, true) {
		return nil
	}
	close(r.queue)
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Dropped returns the count of recordings that have been dropped due to
// queue pressure. Exposed as a metric.
func (r *Recorder) Dropped() uint64 {
	if r == nil {
		return 0
	}
	return r.dropped.Load()
}

func (r *Recorder) run() {
	defer r.wg.Done()
	for p := range r.queue {
		r.write(p)
	}
}

func (r *Recorder) write(p *PendingRecording) {
	if p == nil || p.Recording == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Serialize request + response in canonical form.
	reqBytes, _ := json.Marshal(p.RequestIR)
	p.Recording.RequestHash = sha256Hex(reqBytes)
	if blob, err := r.store.Put(ctx, p.Recording.RequestHash, reqBytes); err == nil {
		p.Recording.RequestBlob = blob
	} else {
		r.logger.Warn("record request blob", zap.Error(err))
	}

	if p.ResponseIR != nil {
		respBytes, _ := json.Marshal(p.ResponseIR)
		p.Recording.ResponseHash = sha256Hex(respBytes)
		if blob, err := r.store.Put(ctx, p.Recording.ResponseHash, respBytes); err == nil {
			p.Recording.ResponseBlob = blob
		} else {
			r.logger.Warn("record response blob", zap.Error(err))
		}
	}

	if p.Recording.ID == uuid.Nil {
		p.Recording.ID = uuid.New()
	}
	if p.Recording.CreatedAt.IsZero() {
		p.Recording.CreatedAt = time.Now().UTC()
	}

	if r.db != nil {
		if err := r.db.InsertRecording(ctx, p.Recording); err != nil {
			r.logger.Warn("record insert", zap.Error(err))
		}
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
