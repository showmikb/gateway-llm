package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gateway-llm/gateway-llm/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ---- operator_price_catalog ---------------------------------------------

// ListActiveOperatorCatalog returns every catalog row whose
// effective_to is NULL (i.e. still in force). Caller verifies
// signatures.
func (db *DB) ListActiveOperatorCatalog(ctx context.Context) ([]models.OperatorPriceCatalog, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT id, provider, model, mode,
		       input_cost_per_token, output_cost_per_token, cache_read_cost_per_token,
		       input_cost_per_image, input_cost_per_character, input_cost_per_second,
		       max_input_tokens, max_output_tokens, size_pricing,
		       source, effective_from, effective_to,
		       signed_at, signature, signed_by_key_id, created_at
		  FROM operator_price_catalog
		 WHERE effective_to IS NULL
		 ORDER BY provider, model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOperatorCatalog(rows)
}

// ListAllOperatorCatalog returns every active catalog row plus a
// configurable historical window (defaults to 0 = active only). Used
// by the admin UI to show past list-price snapshots.
func (db *DB) ListAllOperatorCatalog(ctx context.Context, includeHistory bool) ([]models.OperatorPriceCatalog, error) {
	q := `SELECT id, provider, model, mode,
	             input_cost_per_token, output_cost_per_token, cache_read_cost_per_token,
	             input_cost_per_image, input_cost_per_character, input_cost_per_second,
	             max_input_tokens, max_output_tokens, size_pricing,
	             source, effective_from, effective_to,
	             signed_at, signature, signed_by_key_id, created_at
	        FROM operator_price_catalog`
	if !includeHistory {
		q += ` WHERE effective_to IS NULL`
	}
	q += ` ORDER BY provider, model, effective_from DESC`
	rows, err := db.pool.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanOperatorCatalog(rows)
}

func scanOperatorCatalog(rows pgx.Rows) ([]models.OperatorPriceCatalog, error) {
	var out []models.OperatorPriceCatalog
	for rows.Next() {
		var r models.OperatorPriceCatalog
		var sizeRaw []byte
		var mode *string
		if err := rows.Scan(
			&r.ID, &r.Provider, &r.Model, &mode,
			&r.InputCostPerToken, &r.OutputCostPerToken, &r.CacheReadCostPerToken,
			&r.InputCostPerImage, &r.InputCostPerCharacter, &r.InputCostPerSecond,
			&r.MaxInputTokens, &r.MaxOutputTokens, &sizeRaw,
			&r.Source, &r.EffectiveFrom, &r.EffectiveTo,
			&r.SignedAt, &r.Signature, &r.SignedByKeyID, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		if mode != nil {
			r.Mode = *mode
		}
		if len(sizeRaw) > 0 {
			_ = json.Unmarshal(sizeRaw, &r.SizePricing)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpsertOperatorCatalogRow inserts a new active row and supersedes the
// prior active row for the same (provider, model). The supersede +
// insert is wrapped in a single statement using a CTE so the unique
// "active row per (provider, model)" invariant is never violated mid
// transaction.
func (db *DB) UpsertOperatorCatalogRow(ctx context.Context, r *models.OperatorPriceCatalog) error {
	if r == nil {
		return errors.New("nil catalog row")
	}
	if r.EffectiveFrom.IsZero() {
		r.EffectiveFrom = time.Now().UTC()
	}
	var sizeRaw []byte
	if len(r.SizePricing) > 0 {
		b, err := json.Marshal(r.SizePricing)
		if err != nil {
			return err
		}
		sizeRaw = b
	}
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		UPDATE operator_price_catalog
		   SET effective_to = NOW()
		 WHERE provider = $1 AND model = $2 AND effective_to IS NULL`,
		r.Provider, r.Model)
	if err != nil {
		return err
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO operator_price_catalog
		    (provider, model, mode,
		     input_cost_per_token, output_cost_per_token, cache_read_cost_per_token,
		     input_cost_per_image, input_cost_per_character, input_cost_per_second,
		     max_input_tokens, max_output_tokens, size_pricing,
		     source, effective_from, signed_at, signature, signed_by_key_id)
		VALUES ($1,$2,NULLIF($3,''),
		        $4,$5,$6,
		        $7,$8,$9,
		        $10,$11,$12,
		        $13,$14,$15,$16,$17)
		RETURNING id, created_at`,
		r.Provider, r.Model, r.Mode,
		r.InputCostPerToken, r.OutputCostPerToken, r.CacheReadCostPerToken,
		r.InputCostPerImage, r.InputCostPerCharacter, r.InputCostPerSecond,
		r.MaxInputTokens, r.MaxOutputTokens, sizeRaw,
		r.Source, r.EffectiveFrom, r.SignedAt, r.Signature, r.SignedByKeyID,
	).Scan(&r.ID, &r.CreatedAt)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CountOperatorCatalog returns the number of active rows. Used by the
// auto-seed path in cost.Engine.
func (db *DB) CountOperatorCatalog(ctx context.Context) (int, error) {
	var n int
	err := db.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM operator_price_catalog WHERE effective_to IS NULL`).Scan(&n)
	return n, err
}

// ---- org_provider_discounts ---------------------------------------------

func (db *DB) ListActiveOrgDiscounts(ctx context.Context) ([]models.OrgProviderDiscount, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT id, org_id, provider, discount_pct, evidence_url, evidence_note,
		       declared_by, declared_at, attested_by, attested_at, operator_signature,
		       effective_from, effective_to, created_at
		  FROM org_provider_discounts
		 WHERE effective_to IS NULL
		 ORDER BY org_id, provider`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDiscounts(rows)
}

func (db *DB) ListOrgDiscountsForOrg(ctx context.Context, orgID *uuid.UUID) ([]models.OrgProviderDiscount, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT id, org_id, provider, discount_pct, evidence_url, evidence_note,
		       declared_by, declared_at, attested_by, attested_at, operator_signature,
		       effective_from, effective_to, created_at
		  FROM org_provider_discounts
		 WHERE (org_id IS NOT DISTINCT FROM $1)
		 ORDER BY effective_to NULLS FIRST, declared_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDiscounts(rows)
}

func scanDiscounts(rows pgx.Rows) ([]models.OrgProviderDiscount, error) {
	var out []models.OrgProviderDiscount
	for rows.Next() {
		var d models.OrgProviderDiscount
		if err := rows.Scan(
			&d.ID, &d.OrgID, &d.Provider, &d.DiscountPct,
			&d.EvidenceURL, &d.EvidenceNote,
			&d.DeclaredBy, &d.DeclaredAt,
			&d.AttestedBy, &d.AttestedAt, &d.OperatorSignature,
			&d.EffectiveFrom, &d.EffectiveTo, &d.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DeclareOrgDiscount supersedes any prior active row for the same (org,
// provider) and inserts a new declared (un-countersigned) entry.
func (db *DB) DeclareOrgDiscount(ctx context.Context, d *models.OrgProviderDiscount) error {
	if d == nil {
		return errors.New("nil discount")
	}
	if d.EffectiveFrom.IsZero() {
		d.EffectiveFrom = time.Now().UTC()
	}
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		UPDATE org_provider_discounts
		   SET effective_to = NOW()
		 WHERE org_id IS NOT DISTINCT FROM $1
		   AND provider = $2
		   AND effective_to IS NULL`,
		d.OrgID, d.Provider)
	if err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO org_provider_discounts
		    (org_id, provider, discount_pct, evidence_url, evidence_note,
		     declared_by, effective_from)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, declared_at, created_at`,
		d.OrgID, d.Provider, d.DiscountPct, d.EvidenceURL, d.EvidenceNote,
		d.DeclaredBy, d.EffectiveFrom,
	).Scan(&d.ID, &d.DeclaredAt, &d.CreatedAt)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CountersignDiscount fills attested_by, attested_at, operator_signature.
func (db *DB) CountersignDiscount(ctx context.Context, id uuid.UUID, attestedBy uuid.UUID, signature string) error {
	tag, err := db.pool.Exec(ctx, `
		UPDATE org_provider_discounts
		   SET attested_by = $2,
		       attested_at = NOW(),
		       operator_signature = $3
		 WHERE id = $1 AND attested_at IS NULL AND effective_to IS NULL`,
		id, attestedBy, signature)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("discount not found or already attested/expired")
	}
	return nil
}

// RevokeDiscount sets effective_to so the row stops counting toward
// EffectiveRate without losing the audit trail.
func (db *DB) RevokeDiscount(ctx context.Context, id uuid.UUID) error {
	_, err := db.pool.Exec(ctx,
		`UPDATE org_provider_discounts SET effective_to = NOW() WHERE id = $1 AND effective_to IS NULL`, id)
	return err
}

// ---- routing_policy ------------------------------------------------------

func (db *DB) GetRoutingPolicy(ctx context.Context, alias string, orgID *uuid.UUID) (*models.RoutingPolicy, error) {
	// Per-org wins over global default.
	row := db.pool.QueryRow(ctx, `
		SELECT id, model_alias, org_id, strategy, quality_threshold,
		       baseline_provider_model, judge_alias, retry_when_streaming,
		       sample_pct, min_samples_before_routing, created_at, updated_at
		  FROM routing_policy
		 WHERE model_alias = $1
		   AND org_id IS NOT DISTINCT FROM $2`,
		alias, orgID)
	var p models.RoutingPolicy
	err := row.Scan(&p.ID, &p.ModelAlias, &p.OrgID, &p.Strategy, &p.QualityThreshold,
		&p.BaselineProviderModel, &p.JudgeAlias, &p.RetryWhenStreaming,
		&p.SamplePct, &p.MinSamplesBeforeRouting, &p.CreatedAt, &p.UpdatedAt)
	if err == nil {
		return &p, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if orgID == nil {
		return nil, nil
	}
	// Fallback to global default.
	return db.GetRoutingPolicy(ctx, alias, nil)
}

func (db *DB) ListRoutingPolicies(ctx context.Context, orgID *uuid.UUID) ([]models.RoutingPolicy, error) {
	q := `SELECT id, model_alias, org_id, strategy, quality_threshold,
	             baseline_provider_model, judge_alias, retry_when_streaming,
	             sample_pct, min_samples_before_routing, created_at, updated_at
	        FROM routing_policy`
	args := []any{}
	if orgID != nil {
		q += ` WHERE org_id IS NOT DISTINCT FROM $1 OR org_id IS NULL`
		args = append(args, orgID)
	}
	q += ` ORDER BY model_alias, org_id NULLS FIRST`
	rows, err := db.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.RoutingPolicy
	for rows.Next() {
		var p models.RoutingPolicy
		if err := rows.Scan(&p.ID, &p.ModelAlias, &p.OrgID, &p.Strategy, &p.QualityThreshold,
			&p.BaselineProviderModel, &p.JudgeAlias, &p.RetryWhenStreaming,
			&p.SamplePct, &p.MinSamplesBeforeRouting, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (db *DB) UpsertRoutingPolicy(ctx context.Context, p *models.RoutingPolicy) error {
	if p == nil {
		return errors.New("nil policy")
	}
	row := db.pool.QueryRow(ctx, `
		INSERT INTO routing_policy
		    (model_alias, org_id, strategy, quality_threshold,
		     baseline_provider_model, judge_alias, retry_when_streaming,
		     sample_pct, min_samples_before_routing)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (model_alias, COALESCE(org_id::text, ''))
		DO UPDATE SET strategy = EXCLUDED.strategy,
		              quality_threshold = EXCLUDED.quality_threshold,
		              baseline_provider_model = EXCLUDED.baseline_provider_model,
		              judge_alias = EXCLUDED.judge_alias,
		              retry_when_streaming = EXCLUDED.retry_when_streaming,
		              sample_pct = EXCLUDED.sample_pct,
		              min_samples_before_routing = EXCLUDED.min_samples_before_routing,
		              updated_at = NOW()
		RETURNING id, created_at, updated_at`,
		p.ModelAlias, p.OrgID, p.Strategy, p.QualityThreshold,
		p.BaselineProviderModel, p.JudgeAlias, p.RetryWhenStreaming,
		p.SamplePct, p.MinSamplesBeforeRouting)
	return row.Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
}

func (db *DB) DeleteRoutingPolicy(ctx context.Context, id uuid.UUID) error {
	_, err := db.pool.Exec(ctx, `DELETE FROM routing_policy WHERE id = $1`, id)
	return err
}

// ---- routing_savings -----------------------------------------------------

func (db *DB) InsertRoutingSavings(ctx context.Context, s *models.RoutingSavings) error {
	if s == nil {
		return errors.New("nil savings row")
	}
	row := db.pool.QueryRow(ctx, `
		INSERT INTO routing_savings
		    (recording_id, trace_id, org_id, api_key_id,
		     requested_alias, served_alias, served_provider, served_model,
		     baseline_provider, baseline_model,
		     baseline_input_tokens, baseline_output_tokens,
		     served_input_tokens, served_output_tokens,
		     baseline_cost_usd, actual_cost_usd, discount_pct_applied, savings_usd,
		     quality_score, quality_pass, quality_scorer,
		     retried, strategy, complexity_score, complexity_bucket, overridden,
		     prev_hash, signature, signed_by_key_id, signed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,
		        $11,$12,$13,$14,$15,$16,$17,$18,
		        $19,$20,$21,$22,$23,$24,$25,$26,
		        $27,$28,$29,$30)
		RETURNING id, created_at`,
		s.RecordingID, s.TraceID, s.OrgID, s.APIKeyID,
		s.RequestedAlias, s.ServedAlias, s.ServedProvider, s.ServedModel,
		s.BaselineProvider, s.BaselineModel,
		s.BaselineInputTokens, s.BaselineOutputTokens,
		s.ServedInputTokens, s.ServedOutputTokens,
		s.BaselineCostUSD, s.ActualCostUSD, s.DiscountPctApplied, s.SavingsUSD,
		s.QualityScore, s.QualityPass, s.QualityScorer,
		s.Retried, s.Strategy, s.ComplexityScore, s.ComplexityBucket, s.Overridden,
		s.PrevHash, s.Signature, s.SignedByKeyID, s.SignedAt,
	)
	return row.Scan(&s.ID, &s.CreatedAt)
}

// ListRoutingSavings is the dashboard list query. orgID nil means
// cross-tenant (super admin / master). Filters are AND-ed.
type SavingsFilter struct {
	OrgID    *uuid.UUID
	Alias    string
	Since    *time.Time
	Until    *time.Time
	APIKeyID *uuid.UUID
	Limit    int
	Offset   int
}

func (db *DB) ListRoutingSavings(ctx context.Context, f SavingsFilter) ([]models.RoutingSavings, error) {
	q := `SELECT id, recording_id, trace_id, org_id, api_key_id,
	             requested_alias, served_alias, served_provider, served_model,
	             baseline_provider, baseline_model,
	             baseline_input_tokens, baseline_output_tokens,
	             served_input_tokens, served_output_tokens,
	             baseline_cost_usd, actual_cost_usd, discount_pct_applied, savings_usd,
	             quality_score, quality_pass, quality_scorer,
	             retried, strategy, complexity_score, complexity_bucket, overridden,
	             prev_hash, signature, signed_by_key_id, signed_at, created_at
	        FROM routing_savings WHERE 1=1`
	args := []any{}
	add := func(clause string, val any) {
		args = append(args, val)
		q += " AND " + clause + " $" + itoa(len(args))
	}
	if f.OrgID != nil {
		add("org_id =", *f.OrgID)
	}
	if f.Alias != "" {
		add("requested_alias =", f.Alias)
	}
	if f.APIKeyID != nil {
		add("api_key_id =", *f.APIKeyID)
	}
	if f.Since != nil {
		add("created_at >=", *f.Since)
	}
	if f.Until != nil {
		add("created_at <=", *f.Until)
	}
	q += " ORDER BY created_at DESC"
	if f.Limit > 0 {
		args = append(args, f.Limit)
		q += " LIMIT $" + itoa(len(args))
	}
	if f.Offset > 0 {
		args = append(args, f.Offset)
		q += " OFFSET $" + itoa(len(args))
	}
	rows, err := db.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSavings(rows)
}

func (db *DB) GetRoutingSavingsByID(ctx context.Context, id uuid.UUID) (*models.RoutingSavings, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT id, recording_id, trace_id, org_id, api_key_id,
		       requested_alias, served_alias, served_provider, served_model,
		       baseline_provider, baseline_model,
		       baseline_input_tokens, baseline_output_tokens,
		       served_input_tokens, served_output_tokens,
		       baseline_cost_usd, actual_cost_usd, discount_pct_applied, savings_usd,
		       quality_score, quality_pass, quality_scorer,
		       retried, strategy, complexity_score, complexity_bucket, overridden,
		       prev_hash, signature, signed_by_key_id, signed_at, created_at
		  FROM routing_savings WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out, err := scanSavings(rows)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return &out[0], nil
}

// GetRoutingSavingsByTrace looks up a ledger row by trace_id (used by
// the /usage drill-down to jump to the savings detail).
func (db *DB) GetRoutingSavingsByTrace(ctx context.Context, traceID string) (*models.RoutingSavings, error) {
	if traceID == "" {
		return nil, nil
	}
	rows, err := db.pool.Query(ctx, `
		SELECT id, recording_id, trace_id, org_id, api_key_id,
		       requested_alias, served_alias, served_provider, served_model,
		       baseline_provider, baseline_model,
		       baseline_input_tokens, baseline_output_tokens,
		       served_input_tokens, served_output_tokens,
		       baseline_cost_usd, actual_cost_usd, discount_pct_applied, savings_usd,
		       quality_score, quality_pass, quality_scorer,
		       retried, strategy, complexity_score, complexity_bucket, overridden,
		       prev_hash, signature, signed_by_key_id, signed_at, created_at
		  FROM routing_savings WHERE trace_id = $1
		  ORDER BY created_at DESC LIMIT 1`, traceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out, err := scanSavings(rows)
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	return &out[0], nil
}

func scanSavings(rows pgx.Rows) ([]models.RoutingSavings, error) {
	var out []models.RoutingSavings
	for rows.Next() {
		var s models.RoutingSavings
		if err := rows.Scan(
			&s.ID, &s.RecordingID, &s.TraceID, &s.OrgID, &s.APIKeyID,
			&s.RequestedAlias, &s.ServedAlias, &s.ServedProvider, &s.ServedModel,
			&s.BaselineProvider, &s.BaselineModel,
			&s.BaselineInputTokens, &s.BaselineOutputTokens,
			&s.ServedInputTokens, &s.ServedOutputTokens,
			&s.BaselineCostUSD, &s.ActualCostUSD, &s.DiscountPctApplied, &s.SavingsUSD,
			&s.QualityScore, &s.QualityPass, &s.QualityScorer,
			&s.Retried, &s.Strategy, &s.ComplexityScore, &s.ComplexityBucket, &s.Overridden,
			&s.PrevHash, &s.Signature, &s.SignedByKeyID, &s.SignedAt, &s.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// PrevSavingsHashForOrg returns the hash of the most-recent ledger row
// for org so the writer can chain.
func (db *DB) PrevSavingsHashForOrg(ctx context.Context, orgID *uuid.UUID) (string, error) {
	var sig *string
	err := db.pool.QueryRow(ctx, `
		SELECT signature FROM routing_savings
		 WHERE org_id IS NOT DISTINCT FROM $1
		 ORDER BY created_at DESC LIMIT 1`, orgID).Scan(&sig)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if sig == nil {
		return "", nil
	}
	return *sig, nil
}

// ---- daily_savings -------------------------------------------------------

// UpsertDailySavingsForDate aggregates routing_savings for (date, org)
// and writes/updates the matching daily_savings row. Called by the
// rollup worker.
func (db *DB) UpsertDailySavingsForDate(ctx context.Context, date time.Time, orgID *uuid.UUID, ourCutPct float64) (*models.DailySavings, error) {
	day := date.UTC().Truncate(24 * time.Hour)
	row := db.pool.QueryRow(ctx, `
		WITH agg AS (
		    SELECT COUNT(*)                                                 AS total_requests,
		           COUNT(*) FILTER (WHERE overridden)                       AS routed_requests,
		           COUNT(*) FILTER (WHERE retried)                          AS retry_count,
		           COALESCE(SUM(baseline_cost_usd), 0)                      AS total_baseline,
		           COALESCE(SUM(actual_cost_usd), 0)                        AS total_actual,
		           COALESCE(SUM(savings_usd), 0)                            AS total_savings,
		           AVG(quality_score)                                       AS avg_quality,
		           AVG(CASE WHEN quality_pass IS TRUE THEN 1.0
		                    WHEN quality_pass IS FALSE THEN 0.0 END)        AS pass_pct
		      FROM routing_savings
		     WHERE org_id IS NOT DISTINCT FROM $2
		       AND created_at >= $1
		       AND created_at <  $1 + INTERVAL '1 day'
		)
		INSERT INTO daily_savings
		    (date, org_id, total_requests, routed_requests, retry_count,
		     total_baseline_cost_usd, total_actual_cost_usd, total_savings_usd,
		     our_cut_usd, quality_pass_pct, avg_quality_score)
		SELECT $1, $2, total_requests, routed_requests, retry_count,
		       total_baseline, total_actual, total_savings,
		       total_savings * $3, pass_pct, avg_quality
		  FROM agg
		ON CONFLICT (date, COALESCE(org_id::text, ''))
		DO UPDATE SET total_requests = EXCLUDED.total_requests,
		              routed_requests = EXCLUDED.routed_requests,
		              retry_count = EXCLUDED.retry_count,
		              total_baseline_cost_usd = EXCLUDED.total_baseline_cost_usd,
		              total_actual_cost_usd = EXCLUDED.total_actual_cost_usd,
		              total_savings_usd = EXCLUDED.total_savings_usd,
		              our_cut_usd = EXCLUDED.our_cut_usd,
		              quality_pass_pct = EXCLUDED.quality_pass_pct,
		              avg_quality_score = EXCLUDED.avg_quality_score,
		              computed_at = NOW()
		RETURNING id, date, org_id, total_requests, routed_requests, retry_count,
		          total_baseline_cost_usd, total_actual_cost_usd, total_savings_usd,
		          our_cut_usd, quality_pass_pct, avg_quality_score, computed_at`,
		day, orgID, ourCutPct)
	var d models.DailySavings
	if err := row.Scan(&d.ID, &d.Date, &d.OrgID, &d.TotalRequests, &d.RoutedRequests, &d.RetryCount,
		&d.TotalBaselineCostUSD, &d.TotalActualCostUSD, &d.TotalSavingsUSD,
		&d.OurCutUSD, &d.QualityPassPct, &d.AvgQualityScore, &d.ComputedAt); err != nil {
		return nil, err
	}
	return &d, nil
}

// DistinctSavingsOrgsForDate returns every org_id (incl. NULL) that has
// at least one routing_savings row in the window. Used by the rollup
// worker to know which (date, org) pairs to upsert.
func (db *DB) DistinctSavingsOrgsForDate(ctx context.Context, date time.Time) ([]*uuid.UUID, error) {
	day := date.UTC().Truncate(24 * time.Hour)
	rows, err := db.pool.Query(ctx, `
		SELECT DISTINCT org_id FROM routing_savings
		 WHERE created_at >= $1 AND created_at < $1 + INTERVAL '1 day'`,
		day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*uuid.UUID
	for rows.Next() {
		var oid *uuid.UUID
		if err := rows.Scan(&oid); err != nil {
			return nil, err
		}
		out = append(out, oid)
	}
	return out, rows.Err()
}

// ListDailySavings is the time-series query backing the dashboard chart.
func (db *DB) ListDailySavings(ctx context.Context, orgID *uuid.UUID, since, until time.Time) ([]models.DailySavings, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT id, date, org_id, total_requests, routed_requests, retry_count,
		       total_baseline_cost_usd, total_actual_cost_usd, total_savings_usd,
		       our_cut_usd, quality_pass_pct, avg_quality_score, computed_at
		  FROM daily_savings
		 WHERE org_id IS NOT DISTINCT FROM $1
		   AND date >= $2 AND date <= $3
		 ORDER BY date ASC`,
		orgID, since.UTC().Truncate(24*time.Hour), until.UTC().Truncate(24*time.Hour))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.DailySavings
	for rows.Next() {
		var d models.DailySavings
		if err := rows.Scan(&d.ID, &d.Date, &d.OrgID, &d.TotalRequests, &d.RoutedRequests, &d.RetryCount,
			&d.TotalBaselineCostUSD, &d.TotalActualCostUSD, &d.TotalSavingsUSD,
			&d.OurCutUSD, &d.QualityPassPct, &d.AvgQualityScore, &d.ComputedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// AliasSavingsRow is the per-alias breakdown the /savings page renders.
type AliasSavingsRow struct {
	Alias           string   `json:"requested_alias"`
	ServedAlias     string   `json:"served_alias"`
	Requests        int      `json:"requests"`
	Routed          int      `json:"routed"`
	BaselineCostUSD float64  `json:"baseline_usd"`
	ActualCostUSD   float64  `json:"actual_usd"`
	SavingsUSD      float64  `json:"savings_usd"`
	QualityPassPct  *float64 `json:"quality_pass_pct,omitempty"`
	AvgQualityScore *float64 `json:"avg_quality_score,omitempty"`
}

func (db *DB) AggregateSavingsByAlias(ctx context.Context, orgID *uuid.UUID, since, until time.Time) ([]AliasSavingsRow, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT requested_alias, served_alias,
		       COUNT(*)                                              AS requests,
		       COUNT(*) FILTER (WHERE overridden)                    AS routed,
		       COALESCE(SUM(baseline_cost_usd), 0)                   AS baseline,
		       COALESCE(SUM(actual_cost_usd), 0)                     AS actual,
		       COALESCE(SUM(savings_usd), 0)                         AS savings,
		       AVG(CASE WHEN quality_pass IS TRUE THEN 1.0
		                WHEN quality_pass IS FALSE THEN 0.0 END)     AS pass_pct,
		       AVG(quality_score)                                    AS avg_quality
		  FROM routing_savings
		 WHERE org_id IS NOT DISTINCT FROM $1
		   AND created_at >= $2 AND created_at <= $3
		 GROUP BY requested_alias, served_alias
		 ORDER BY savings DESC`,
		orgID, since, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AliasSavingsRow
	for rows.Next() {
		var r AliasSavingsRow
		if err := rows.Scan(&r.Alias, &r.ServedAlias, &r.Requests, &r.Routed,
			&r.BaselineCostUSD, &r.ActualCostUSD, &r.SavingsUSD,
			&r.QualityPassPct, &r.AvgQualityScore); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// (placeholder builder uses the package-local itoa already defined in
// recordings.go.)
