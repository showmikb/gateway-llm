package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/gateway-llm/gateway-llm/internal/models"
	"github.com/gateway-llm/gateway-llm/internal/smartroute"
)

// InsertFeedback persists a quality signal attached to a prior inference.
// Each row is immutable; the API never offers UPDATE/DELETE.
func (db *DB) InsertFeedback(ctx context.Context, ev *models.FeedbackEvent) error {
	return db.pool.QueryRow(ctx, `
		INSERT INTO feedback_events (trace_id, metric, value_float, value_bool, value_text, model_alias, variant_id, api_key_id, org_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, created_at`,
		ev.TraceID, ev.Metric, ev.ValueFloat, ev.ValueBool, ev.ValueText,
		ev.ModelAlias, ev.VariantID, ev.APIKeyID, ev.OrgID,
	).Scan(&ev.ID, &ev.CreatedAt)
}

// ModelMetric is a row of the aggregated feedback view, grouped by
// (model_alias, variant_id, metric) over the last N days.
type ModelMetric struct {
	ModelAlias string  `json:"model_alias"`
	VariantID  string  `json:"variant_id"`
	Metric     string  `json:"metric"`
	Count      int     `json:"count"`
	AvgFloat   float64 `json:"avg_float,omitempty"`
	TrueRate   float64 `json:"true_rate,omitempty"`
}

// AggregateFeedback returns the mean float value and "true" rate for
// each (model_alias, variant_id, metric) tuple over the past `days`.
func (db *DB) AggregateFeedback(ctx context.Context, orgID *uuid.UUID, days int) ([]ModelMetric, error) {
	if days <= 0 {
		days = 7
	}
	args := []any{days}
	where := "created_at > now() - ($1::int || ' days')::interval"
	if orgID != nil {
		args = append(args, *orgID)
		where += fmt.Sprintf(" AND org_id = $%d", len(args))
	}
	rows, err := db.pool.Query(ctx, fmt.Sprintf(`
		SELECT model_alias, COALESCE(variant_id, ''), metric,
		       COUNT(*)::int,
		       COALESCE(AVG(value_float), 0),
		       COALESCE(AVG(CASE WHEN value_bool THEN 1.0 WHEN value_bool IS NOT NULL THEN 0.0 END), 0)
		FROM feedback_events
		WHERE %s
		GROUP BY model_alias, variant_id, metric
		ORDER BY model_alias, variant_id, metric`, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ModelMetric
	for rows.Next() {
		var m ModelMetric
		if err := rows.Scan(&m.ModelAlias, &m.VariantID, &m.Metric, &m.Count, &m.AvgFloat, &m.TrueRate); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// InsertAuditEvent appends to the immutable audit log.
func (db *DB) InsertAuditEvent(ctx context.Context, ev *models.AuditEvent) error {
	meta, err := json.Marshal(ev.Metadata)
	if err != nil {
		meta = []byte(`{}`)
	}
	var ip any
	if ev.IP != "" {
		ip = ev.IP
	}
	return db.pool.QueryRow(ctx, `
		INSERT INTO audit_logs (actor_type, actor_id, action, resource, org_id, team_id, api_key_id, ip, user_agent, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, occurred_at`,
		ev.ActorType, nullableStr(ev.ActorID), ev.Action, nullableStr(ev.Resource),
		ev.OrgID, ev.TeamID, ev.APIKeyID, ip, nullableStr(ev.UserAgent), string(meta),
	).Scan(&ev.ID, &ev.OccurredAt)
}

// ListAuditEvents returns the most recent audit entries, optionally
// scoped to an org.
func (db *DB) ListAuditEvents(ctx context.Context, orgID *uuid.UUID, limit int) ([]models.AuditEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	var (
		rows pgx.Rows
		err  error
	)
	if orgID != nil {
		rows, err = db.pool.Query(ctx, `
			SELECT id, occurred_at, actor_type, COALESCE(actor_id, ''), action,
			       COALESCE(resource, ''), org_id, team_id, api_key_id,
			       COALESCE(host(ip), ''), COALESCE(user_agent, ''), metadata
			FROM audit_logs WHERE org_id = $1
			ORDER BY occurred_at DESC LIMIT $2`, *orgID, limit)
	} else {
		rows, err = db.pool.Query(ctx, `
			SELECT id, occurred_at, actor_type, COALESCE(actor_id, ''), action,
			       COALESCE(resource, ''), org_id, team_id, api_key_id,
			       COALESCE(host(ip), ''), COALESCE(user_agent, ''), metadata
			FROM audit_logs ORDER BY occurred_at DESC LIMIT $1`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.AuditEvent
	for rows.Next() {
		var ev models.AuditEvent
		var metaRaw []byte
		if err := rows.Scan(&ev.ID, &ev.OccurredAt, &ev.ActorType, &ev.ActorID,
			&ev.Action, &ev.Resource, &ev.OrgID, &ev.TeamID, &ev.APIKeyID,
			&ev.IP, &ev.UserAgent, &metaRaw); err != nil {
			return nil, err
		}
		if len(metaRaw) > 0 {
			_ = json.Unmarshal(metaRaw, &ev.Metadata)
		}
		out = append(out, ev)
	}
	return out, nil
}

func nullableStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// InsertRequestFeatures snapshots the feature vector used for a single
// inference so it can later be joined with feedback for training. The
// call is safe to run inside a goroutine; conflicts on trace_id are
// swallowed so a retry cannot produce duplicates.
func (db *DB) InsertRequestFeatures(ctx context.Context, traceID, modelAlias string, orgID, apiKeyID *uuid.UUID, features smartroute.Features) error {
	data, err := json.Marshal(features)
	if err != nil {
		return err
	}
	_, err = db.pool.Exec(ctx, `
		INSERT INTO request_features (trace_id, model_alias, org_id, api_key_id, features)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (trace_id) DO NOTHING`,
		traceID, modelAlias, orgID, apiKeyID, string(data),
	)
	return err
}

// LoadTrainingSamples joins request_features with feedback_events by
// trace_id, returning (features, label) pairs for the ML trainer. The
// label is derived from the feedback metric: numeric feedback is used
// as-is (clamped to [0,1]); boolean feedback becomes 1.0 / 0.0; text
// feedback is ignored because it has no obvious mapping.
func (db *DB) LoadTrainingSamples(ctx context.Context, metric string, days int) ([]smartroute.TrainingSample, error) {
	if days <= 0 {
		days = 30
	}
	rows, err := db.pool.Query(ctx, `
		SELECT rf.features, fe.value_float, fe.value_bool
		FROM request_features rf
		JOIN feedback_events fe ON fe.trace_id = rf.trace_id
		WHERE fe.metric = $1
		  AND fe.created_at >= now() - ($2 || ' days')::interval
		  AND (fe.value_float IS NOT NULL OR fe.value_bool IS NOT NULL)`,
		metric, fmt.Sprintf("%d", days),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []smartroute.TrainingSample
	for rows.Next() {
		var raw []byte
		var vf *float64
		var vb *bool
		if err := rows.Scan(&raw, &vf, &vb); err != nil {
			return nil, err
		}
		var f smartroute.Features
		if err := json.Unmarshal(raw, &f); err != nil {
			continue
		}
		var label float64
		switch {
		case vf != nil:
			label = *vf
			if label < 0 {
				label = 0
			} else if label > 1 {
				label = 1
			}
		case vb != nil:
			if *vb {
				label = 1
			}
		default:
			continue
		}
		out = append(out, smartroute.TrainingSample{Features: f, Label: label})
	}
	return out, nil
}
