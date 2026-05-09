// Package audit wraps the DB audit_logs table in a small fire-and-forget
// API for recording security-relevant events. All writes are async so the
// request path never blocks on audit I/O. On error we log and drop the
// event rather than failing the user's request.
package audit

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gateway-llm/gateway-llm/internal/db"
	"github.com/gateway-llm/gateway-llm/internal/models"
	"go.uber.org/zap"
)

type Logger struct {
	db     *db.DB
	logger *zap.Logger
	ch     chan models.AuditEvent
}

// New starts a background writer that consumes events from an internal
// channel. A buffer of 1024 is typically enough for bursty traffic; if
// it fills we drop the oldest event and log a warning.
func New(database *db.DB, logger *zap.Logger) *Logger {
	l := &Logger{db: database, logger: logger, ch: make(chan models.AuditEvent, 1024)}
	if database != nil {
		go l.run()
	}
	return l
}

func (l *Logger) run() {
	for ev := range l.ch {
		ev := ev
		if err := l.db.InsertAuditEvent(context.Background(), &ev); err != nil && l.logger != nil {
			l.logger.Debug("audit insert failed", zap.Error(err), zap.String("action", ev.Action))
		}
	}
}

// Record enqueues an audit event. Non-blocking: drops the event if the
// buffer is full, which is preferable to stalling the request path.
func (l *Logger) Record(ev models.AuditEvent) {
	if l == nil || l.db == nil {
		return
	}
	select {
	case l.ch <- ev:
	default:
		if l.logger != nil {
			l.logger.Warn("audit buffer full, dropping event", zap.String("action", ev.Action))
		}
	}
}

// From builds an AuditEvent skeleton from an HTTP request. Callers fill
// in Action/Resource/Metadata and then pass to Record.
func From(r *http.Request, actorType string) models.AuditEvent {
	ev := models.AuditEvent{
		ActorType: actorType,
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
	}
	return ev
}

func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if i := strings.Index(v, ","); i >= 0 {
			return strings.TrimSpace(v[:i])
		}
		return strings.TrimSpace(v)
	}
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	if r.RemoteAddr != "" {
		if i := strings.LastIndex(r.RemoteAddr, ":"); i > 0 {
			return r.RemoteAddr[:i]
		}
		return r.RemoteAddr
	}
	return ""
}

// Close stops the background writer.
func (l *Logger) Close() {
	if l == nil || l.ch == nil {
		return
	}
	close(l.ch)
}

// Helpers for UUID ptr conversion at call sites.
func UUIDPtr(id uuid.UUID) *uuid.UUID { return &id }
