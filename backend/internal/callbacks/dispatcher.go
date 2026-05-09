package callbacks

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

type Dispatcher struct {
	callbacks []Callback
	logger    *zap.Logger
	timeout   time.Duration
}

func NewDispatcher(cbs []Callback, logger *zap.Logger) *Dispatcher {
	return &Dispatcher{
		callbacks: cbs,
		logger:    logger,
		timeout:   10 * time.Second,
	}
}

func (d *Dispatcher) Dispatch(event RequestEvent) {
	if len(d.callbacks) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), d.timeout)
		defer cancel()

		var wg sync.WaitGroup
		for _, cb := range d.callbacks {
			wg.Add(1)
			go func(c Callback) {
				defer wg.Done()
				if err := c.Send(ctx, event); err != nil {
					d.logger.Warn("callback send failed",
						zap.String("callback", c.Name()),
						zap.Error(err))
				}
			}(cb)
		}
		wg.Wait()
	}()
}

func (d *Dispatcher) TestAll(ctx context.Context) map[string]string {
	results := make(map[string]string)
	testEvent := RequestEvent{
		TraceID:          "test-trace-id",
		SpanID:           "test-span-id",
		Timestamp:        time.Now(),
		DurationMS:       100,
		Method:           "POST",
		Endpoint:         "test",
		ModelAlias:       "test-model",
		Provider:         "test-provider",
		ProviderModel:    "test-provider-model",
		Status:           200,
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
		CostUSD:          0.001,
	}
	for _, cb := range d.callbacks {
		if err := cb.Send(ctx, testEvent); err != nil {
			results[cb.Name()] = "error: " + err.Error()
		} else {
			results[cb.Name()] = "ok"
		}
	}
	return results
}

func (d *Dispatcher) ListCallbacks() []map[string]string {
	var out []map[string]string
	for _, cb := range d.callbacks {
		out = append(out, map[string]string{
			"name": cb.Name(),
			"type": cb.Name(),
		})
	}
	return out
}

func (d *Dispatcher) Close() {
	for _, cb := range d.callbacks {
		if err := cb.Close(); err != nil {
			d.logger.Warn("callback close failed", zap.String("callback", cb.Name()), zap.Error(err))
		}
	}
}
