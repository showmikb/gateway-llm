package db

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/gateway-llm/gateway-llm/internal/models"
	"go.uber.org/zap"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type DB struct {
	pool   *pgxpool.Pool
	logger *zap.Logger
}

func New(ctx context.Context, databaseURL string, maxConns int, logger *zap.Logger) (*DB, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing database URL: %w", err)
	}
	config.MaxConns = int32(maxConns)

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("creating connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	return &DB{pool: pool, logger: logger}, nil
}

func (db *DB) RunMigrations(ctx context.Context) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("reading migrations dir: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		sqlBytes, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", entry.Name(), err)
		}

		raw := string(sqlBytes)

		upStart := strings.Index(raw, "-- +migrate Up")
		downStart := strings.Index(raw, "-- +migrate Down")
		if upStart >= 0 && downStart > upStart {
			raw = raw[upStart+len("-- +migrate Up") : downStart]
		} else if upStart >= 0 {
			raw = raw[upStart+len("-- +migrate Up"):]
		}

		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		_, err = db.pool.Exec(ctx, raw)
		if err != nil {
			if strings.Contains(err.Error(), "already exists") {
				db.logger.Info("migration already applied", zap.String("file", entry.Name()))
				continue
			}
			return fmt.Errorf("running migration %s: %w", entry.Name(), err)
		}
		db.logger.Info("migration applied", zap.String("file", entry.Name()))
	}
	return nil
}

func (db *DB) Close() {
	db.pool.Close()
}

func (db *DB) Ping(ctx context.Context) error {
	return db.pool.Ping(ctx)
}

// --- API Key operations ---

func (db *DB) GetAPIKeyByHash(ctx context.Context, tokenHash string) (*models.APIKey, error) {
	key := &models.APIKey{}
	err := db.pool.QueryRow(ctx,
		`SELECT id, token_hash, name, team_id, user_id, models, rpm_limit, tpm_limit,
		        max_budget, total_spend, is_active, expires_at, created_at, updated_at
		 FROM api_keys WHERE token_hash = $1`, tokenHash,
	).Scan(&key.ID, &key.TokenHash, &key.Name, &key.TeamID, &key.UserID, &key.Models,
		&key.RPMLimit, &key.TPMLimit, &key.MaxBudget, &key.TotalSpend,
		&key.IsActive, &key.ExpiresAt, &key.CreatedAt, &key.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return key, nil
}

func (db *DB) CreateAPIKey(ctx context.Context, key *models.APIKey) error {
	return db.pool.QueryRow(ctx,
		`INSERT INTO api_keys (token_hash, name, team_id, user_id, models, rpm_limit, tpm_limit, max_budget)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id, created_at, updated_at`,
		key.TokenHash, key.Name, key.TeamID, key.UserID, key.Models,
		key.RPMLimit, key.TPMLimit, key.MaxBudget,
	).Scan(&key.ID, &key.CreatedAt, &key.UpdatedAt)
}

func (db *DB) ListAPIKeys(ctx context.Context) ([]models.APIKey, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT id, name, team_id, user_id, models, rpm_limit, tpm_limit,
		        max_budget, total_spend, is_active, expires_at, created_at, updated_at
		 FROM api_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []models.APIKey
	for rows.Next() {
		var k models.APIKey
		if err := rows.Scan(&k.ID, &k.Name, &k.TeamID, &k.UserID, &k.Models,
			&k.RPMLimit, &k.TPMLimit, &k.MaxBudget, &k.TotalSpend,
			&k.IsActive, &k.ExpiresAt, &k.CreatedAt, &k.UpdatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, nil
}

func (db *DB) DeleteAPIKey(ctx context.Context, id uuid.UUID) error {
	_, err := db.pool.Exec(ctx, `DELETE FROM api_keys WHERE id = $1`, id)
	return err
}

// --- Team operations ---

func (db *DB) CreateTeam(ctx context.Context, team *models.Team) error {
	return db.pool.QueryRow(ctx,
		`INSERT INTO teams (name, org_id, models, rpm_limit, tpm_limit, max_budget)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at, updated_at`,
		team.Name, team.OrgID, team.Models, team.RPMLimit, team.TPMLimit, team.MaxBudget,
	).Scan(&team.ID, &team.CreatedAt, &team.UpdatedAt)
}

func (db *DB) ListTeams(ctx context.Context) ([]models.Team, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT id, name, org_id, models, rpm_limit, tpm_limit, max_budget, total_spend, created_at, updated_at
		 FROM teams ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var teams []models.Team
	for rows.Next() {
		var t models.Team
		if err := rows.Scan(&t.ID, &t.Name, &t.OrgID, &t.Models, &t.RPMLimit, &t.TPMLimit,
			&t.MaxBudget, &t.TotalSpend, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		teams = append(teams, t)
	}
	return teams, nil
}

func (db *DB) GetTeam(ctx context.Context, id uuid.UUID) (*models.Team, error) {
	t := &models.Team{}
	err := db.pool.QueryRow(ctx,
		`SELECT id, name, org_id, models, rpm_limit, tpm_limit, max_budget, total_spend, created_at, updated_at
		 FROM teams WHERE id = $1`, id,
	).Scan(&t.ID, &t.Name, &t.OrgID, &t.Models, &t.RPMLimit, &t.TPMLimit,
		&t.MaxBudget, &t.TotalSpend, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (db *DB) DeleteTeam(ctx context.Context, id uuid.UUID) error {
	_, err := db.pool.Exec(ctx, `DELETE FROM teams WHERE id = $1`, id)
	return err
}

// --- Spend log operations ---

func (db *DB) InsertSpendLog(ctx context.Context, log *models.SpendLog) error {
	var traceID *string
	if log.TraceID != "" {
		t := log.TraceID
		traceID = &t
	}
	_, err := db.pool.Exec(ctx,
		`INSERT INTO spend_logs (api_key_id, deployment_id, model_alias, provider, endpoint,
		 prompt_tokens, completion_tokens, total_tokens, cost_usd, latency_ms, status_code, trace_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		log.APIKeyID, log.DeploymentID, log.ModelAlias, log.Provider, log.Endpoint,
		log.PromptTokens, log.CompletionTokens, log.TotalTokens, log.CostUSD,
		log.LatencyMS, log.StatusCode, traceID)
	return err
}

func (db *DB) UpdateKeySpend(ctx context.Context, keyID uuid.UUID, cost float64) error {
	_, err := db.pool.Exec(ctx,
		`UPDATE api_keys SET total_spend = total_spend + $1, updated_at = now() WHERE id = $2`,
		cost, keyID)
	return err
}

func (db *DB) GetSpendLogs(ctx context.Context, limit, offset int) ([]models.SpendLog, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT id, api_key_id, deployment_id, model_alias, provider, endpoint,
		        prompt_tokens, completion_tokens, total_tokens, cost_usd, latency_ms,
		        status_code, COALESCE(trace_id, ''), created_at
		 FROM spend_logs ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []models.SpendLog
	for rows.Next() {
		var l models.SpendLog
		if err := rows.Scan(&l.ID, &l.APIKeyID, &l.DeploymentID, &l.ModelAlias, &l.Provider,
			&l.Endpoint, &l.PromptTokens, &l.CompletionTokens, &l.TotalTokens,
			&l.CostUSD, &l.LatencyMS, &l.StatusCode, &l.TraceID, &l.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, nil
}

func (db *DB) GetDailySpend(ctx context.Context, days int) ([]models.DailySpend, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT api_key_id, team_id, date, total_tokens, total_cost_usd, request_count
		 FROM daily_spend WHERE date >= now() - $1::interval ORDER BY date DESC`,
		fmt.Sprintf("%d days", days))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var spends []models.DailySpend
	for rows.Next() {
		var s models.DailySpend
		if err := rows.Scan(&s.APIKeyID, &s.TeamID, &s.Date, &s.TotalTokens,
			&s.TotalCostUSD, &s.RequestCount); err != nil {
			return nil, err
		}
		spends = append(spends, s)
	}
	return spends, nil
}

// GetSpendLogsForKey returns recent spend log rows scoped to a single
// API key, newest first. Drives the per-key drill-down in the UI.
func (db *DB) GetSpendLogsForKey(ctx context.Context, keyID uuid.UUID, limit, offset int) ([]models.SpendLog, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT id, api_key_id, deployment_id, model_alias, provider, endpoint,
		        prompt_tokens, completion_tokens, total_tokens, cost_usd, latency_ms,
		        status_code, COALESCE(trace_id, ''), created_at
		 FROM spend_logs
		 WHERE api_key_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2 OFFSET $3`, keyID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	logs := make([]models.SpendLog, 0)
	for rows.Next() {
		var l models.SpendLog
		if err := rows.Scan(&l.ID, &l.APIKeyID, &l.DeploymentID, &l.ModelAlias, &l.Provider,
			&l.Endpoint, &l.PromptTokens, &l.CompletionTokens, &l.TotalTokens,
			&l.CostUSD, &l.LatencyMS, &l.StatusCode, &l.TraceID, &l.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, nil
}

// GetDailySpendForKey returns aggregated daily spend for a single API
// key over the last `days` days, oldest first (chart-friendly order).
func (db *DB) GetDailySpendForKey(ctx context.Context, keyID uuid.UUID, days int) ([]models.DailySpend, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT api_key_id, team_id, date, total_tokens, total_cost_usd, request_count
		 FROM daily_spend
		 WHERE api_key_id = $1 AND date >= now() - $2::interval
		 ORDER BY date ASC`,
		keyID, fmt.Sprintf("%d days", days))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	spends := make([]models.DailySpend, 0)
	for rows.Next() {
		var s models.DailySpend
		if err := rows.Scan(&s.APIKeyID, &s.TeamID, &s.Date, &s.TotalTokens,
			&s.TotalCostUSD, &s.RequestCount); err != nil {
			return nil, err
		}
		spends = append(spends, s)
	}
	return spends, nil
}

func (db *DB) UpsertDailySpend(ctx context.Context, keyID *uuid.UUID, teamID *uuid.UUID, tokens int, cost float64) error {
	_, err := db.pool.Exec(ctx,
		`INSERT INTO daily_spend (api_key_id, team_id, date, total_tokens, total_cost_usd, request_count)
		 VALUES ($1, $2, CURRENT_DATE, $3, $4, 1)
		 ON CONFLICT (api_key_id, team_id, date)
		 DO UPDATE SET total_tokens = daily_spend.total_tokens + $3,
		              total_cost_usd = daily_spend.total_cost_usd + $4,
		              request_count = daily_spend.request_count + 1`,
		keyID, teamID, tokens, cost)
	return err
}

// --- Custom Pricing operations ---

func (db *DB) GetCustomPricing(ctx context.Context, provider, model string) (*models.CustomPricing, error) {
	p := &models.CustomPricing{}
	err := db.pool.QueryRow(ctx,
		`SELECT id, provider, model, deployment_id, input_cost_per_token, output_cost_per_token,
		        cache_read_cost_per_token, input_cost_per_image, input_cost_per_character,
		        input_cost_per_second, size_pricing, created_at, updated_at
		 FROM custom_pricing WHERE provider = $1 AND model = $2 AND deployment_id IS NULL`,
		provider, model,
	).Scan(&p.ID, &p.Provider, &p.Model, &p.DeploymentID,
		&p.InputCostPerToken, &p.OutputCostPerToken, &p.CacheReadCostPerToken,
		&p.InputCostPerImage, &p.InputCostPerCharacter, &p.InputCostPerSecond,
		&p.SizePricing, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return p, nil
}

func (db *DB) UpsertCustomPricing(ctx context.Context, p *models.CustomPricing) error {
	_, err := db.pool.Exec(ctx,
		`INSERT INTO custom_pricing (provider, model, deployment_id, input_cost_per_token, output_cost_per_token,
		 cache_read_cost_per_token, input_cost_per_image, input_cost_per_character, input_cost_per_second, size_pricing)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (provider, model, deployment_id)
		 DO UPDATE SET input_cost_per_token = $4, output_cost_per_token = $5,
		              cache_read_cost_per_token = $6, input_cost_per_image = $7,
		              input_cost_per_character = $8, input_cost_per_second = $9,
		              size_pricing = $10, updated_at = now()`,
		p.Provider, p.Model, p.DeploymentID, p.InputCostPerToken, p.OutputCostPerToken,
		p.CacheReadCostPerToken, p.InputCostPerImage, p.InputCostPerCharacter,
		p.InputCostPerSecond, p.SizePricing)
	return err
}

func (db *DB) DeleteCustomPricing(ctx context.Context, provider, model string) error {
	_, err := db.pool.Exec(ctx,
		`DELETE FROM custom_pricing WHERE provider = $1 AND model = $2 AND deployment_id IS NULL`,
		provider, model)
	return err
}

func (db *DB) ListCustomPricing(ctx context.Context) ([]models.CustomPricing, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT id, provider, model, deployment_id, input_cost_per_token, output_cost_per_token,
		        cache_read_cost_per_token, input_cost_per_image, input_cost_per_character,
		        input_cost_per_second, size_pricing, created_at, updated_at
		 FROM custom_pricing ORDER BY provider, model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pricing []models.CustomPricing
	for rows.Next() {
		var p models.CustomPricing
		if err := rows.Scan(&p.ID, &p.Provider, &p.Model, &p.DeploymentID,
			&p.InputCostPerToken, &p.OutputCostPerToken, &p.CacheReadCostPerToken,
			&p.InputCostPerImage, &p.InputCostPerCharacter, &p.InputCostPerSecond,
			&p.SizePricing, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		pricing = append(pricing, p)
	}
	return pricing, nil
}

// --- Response objects ---

func (db *DB) StoreResponseObject(ctx context.Context, responseID string, apiKeyID *uuid.UUID, provider, modelAlias, status string, reqBody, respBody []byte) error {
	_, err := db.pool.Exec(ctx,
		`INSERT INTO response_objects (response_id, api_key_id, provider, model_alias, status, request_body, response_body)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (response_id) DO UPDATE SET status = $5, response_body = $7`,
		responseID, apiKeyID, provider, modelAlias, status, reqBody, respBody)
	return err
}

func (db *DB) GetResponseObject(ctx context.Context, responseID string) ([]byte, error) {
	var body []byte
	err := db.pool.QueryRow(ctx,
		`SELECT response_body FROM response_objects WHERE response_id = $1`, responseID,
	).Scan(&body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

// DeleteResponseObject returns the number of rows deleted so callers can
// distinguish "row not found" from "row existed and was deleted".
func (db *DB) DeleteResponseObject(ctx context.Context, responseID string) (int64, error) {
	tag, err := db.pool.Exec(ctx, `DELETE FROM response_objects WHERE response_id = $1`, responseID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// --- Health check helper ---

func (db *DB) HealthCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return db.pool.Ping(ctx)
}

// --- User operations ---

func (db *DB) CreateUser(ctx context.Context, user *models.User) error {
	return db.pool.QueryRow(ctx,
		`INSERT INTO users (email, role, team_id, org_id, password_hash, is_active)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at, updated_at`,
		user.Email, user.Role, user.TeamID, user.OrgID, user.PasswordHash, user.IsActive,
	).Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)
}

func (db *DB) GetUser(ctx context.Context, id uuid.UUID) (*models.User, error) {
	u := &models.User{}
	err := db.pool.QueryRow(ctx,
		`SELECT id, email, role, team_id, org_id, password_hash, is_active, created_at, updated_at
		 FROM users WHERE id = $1`, id,
	).Scan(&u.ID, &u.Email, &u.Role, &u.TeamID, &u.OrgID, &u.PasswordHash, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (db *DB) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	u := &models.User{}
	err := db.pool.QueryRow(ctx,
		`SELECT id, email, role, team_id, org_id, password_hash, is_active, created_at, updated_at
		 FROM users WHERE email = $1`, email,
	).Scan(&u.ID, &u.Email, &u.Role, &u.TeamID, &u.OrgID, &u.PasswordHash, &u.IsActive, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (db *DB) ListUsers(ctx context.Context) ([]models.User, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT id, email, role, team_id, org_id, is_active, created_at, updated_at
		 FROM users ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Email, &u.Role, &u.TeamID, &u.OrgID, &u.IsActive, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

func (db *DB) UpdateUser(ctx context.Context, user *models.User) error {
	_, err := db.pool.Exec(ctx,
		`UPDATE users SET email = $1, role = $2, team_id = $3, org_id = $4, is_active = $5, updated_at = now()
		 WHERE id = $6`,
		user.Email, user.Role, user.TeamID, user.OrgID, user.IsActive, user.ID)
	return err
}

func (db *DB) UpdateUserPassword(ctx context.Context, id uuid.UUID, passwordHash string) error {
	_, err := db.pool.Exec(ctx,
		`UPDATE users SET password_hash = $1, updated_at = now() WHERE id = $2`,
		passwordHash, id)
	return err
}

func (db *DB) DeleteUser(ctx context.Context, id uuid.UUID) error {
	_, err := db.pool.Exec(ctx,
		`UPDATE users SET is_active = false, updated_at = now() WHERE id = $1`, id)
	return err
}

// --- Provider Credential operations ---

func (db *DB) CreateCredential(ctx context.Context, cred *models.ProviderCredential) error {
	return db.pool.QueryRow(ctx,
		`INSERT INTO provider_credentials (name, provider, api_key_enc, api_base, org_id, is_active, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, created_at, updated_at`,
		cred.Name, cred.Provider, cred.APIKeyEnc, cred.APIBase, cred.OrgID, cred.IsActive, cred.CreatedBy,
	).Scan(&cred.ID, &cred.CreatedAt, &cred.UpdatedAt)
}

func (db *DB) GetCredential(ctx context.Context, id uuid.UUID) (*models.ProviderCredential, error) {
	c := &models.ProviderCredential{}
	err := db.pool.QueryRow(ctx,
		`SELECT id, name, provider, api_key_enc, api_base, org_id, is_active, created_by, created_at, updated_at
		 FROM provider_credentials WHERE id = $1`, id,
	).Scan(&c.ID, &c.Name, &c.Provider, &c.APIKeyEnc, &c.APIBase, &c.OrgID, &c.IsActive, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (db *DB) GetCredentialByName(ctx context.Context, name string) (*models.ProviderCredential, error) {
	c := &models.ProviderCredential{}
	err := db.pool.QueryRow(ctx,
		`SELECT id, name, provider, api_key_enc, api_base, org_id, is_active, created_by, created_at, updated_at
		 FROM provider_credentials WHERE name = $1`, name,
	).Scan(&c.ID, &c.Name, &c.Provider, &c.APIKeyEnc, &c.APIBase, &c.OrgID, &c.IsActive, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (db *DB) ListCredentials(ctx context.Context) ([]models.ProviderCredential, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT id, name, provider, api_key_enc, api_base, org_id, is_active, created_by, created_at, updated_at
		 FROM provider_credentials ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var creds []models.ProviderCredential
	for rows.Next() {
		var c models.ProviderCredential
		if err := rows.Scan(&c.ID, &c.Name, &c.Provider, &c.APIKeyEnc, &c.APIBase, &c.OrgID, &c.IsActive, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		creds = append(creds, c)
	}
	return creds, nil
}

func (db *DB) UpdateCredential(ctx context.Context, cred *models.ProviderCredential) error {
	_, err := db.pool.Exec(ctx,
		`UPDATE provider_credentials SET name=$1, provider=$2, api_key_enc=$3, api_base=$4, org_id=$5, is_active=$6, updated_at=now() WHERE id=$7`,
		cred.Name, cred.Provider, cred.APIKeyEnc, cred.APIBase, cred.OrgID, cred.IsActive, cred.ID)
	return err
}

func (db *DB) DeleteCredential(ctx context.Context, id uuid.UUID) error {
	_, err := db.pool.Exec(ctx, `DELETE FROM provider_credentials WHERE id = $1`, id)
	return err
}

// FindFirstActiveCredentialByProvider returns the first active credential
// matching the given provider, or (nil, nil) if none exist.
func (db *DB) FindFirstActiveCredentialByProvider(ctx context.Context, provider string) (*models.ProviderCredential, error) {
	c := &models.ProviderCredential{}
	err := db.pool.QueryRow(ctx,
		`SELECT id, name, provider, api_key_enc, api_base, org_id, is_active, created_by, created_at, updated_at
		 FROM provider_credentials WHERE provider = $1 AND is_active = true
		 ORDER BY created_at ASC LIMIT 1`, provider,
	).Scan(&c.ID, &c.Name, &c.Provider, &c.APIKeyEnc, &c.APIBase, &c.OrgID, &c.IsActive, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// --- Deployment CRUD operations ---

func (db *DB) CreateDeployment(ctx context.Context, dep *models.Deployment) error {
	if dep.RoutingStrategy == "" {
		dep.RoutingStrategy = "round-robin"
	}
	if dep.Weight <= 0 {
		dep.Weight = 1
	}
	return db.pool.QueryRow(ctx,
		`INSERT INTO deployments (model_alias, provider, provider_model, api_key_env, credential_id, api_base, capabilities, priority, is_active, org_id, routing_strategy, weight)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING id, created_at`,
		dep.ModelAlias, dep.Provider, dep.ProviderModel, dep.APIKeyEnv, dep.CredentialID, dep.APIBase, dep.Capabilities, dep.Priority, dep.IsActive, dep.OrgID, dep.RoutingStrategy, dep.Weight,
	).Scan(&dep.ID, &dep.CreatedAt)
}

func (db *DB) ListDeployments(ctx context.Context) ([]models.Deployment, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT d.id, d.model_alias, d.provider, d.provider_model, d.api_key_env,
		        d.credential_id, COALESCE(pc.name, ''), d.org_id, d.api_base, d.capabilities, d.priority, d.is_active, d.created_at, d.routing_strategy, d.weight
		 FROM deployments d LEFT JOIN provider_credentials pc ON d.credential_id = pc.id
		 ORDER BY d.model_alias, d.priority`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deps []models.Deployment
	for rows.Next() {
		var d models.Deployment
		if err := rows.Scan(&d.ID, &d.ModelAlias, &d.Provider, &d.ProviderModel, &d.APIKeyEnv,
			&d.CredentialID, &d.CredentialName, &d.OrgID, &d.APIBase, &d.Capabilities, &d.Priority, &d.IsActive, &d.CreatedAt, &d.RoutingStrategy, &d.Weight); err != nil {
			return nil, err
		}
		deps = append(deps, d)
	}
	return deps, nil
}

func (db *DB) GetActiveDeployments(ctx context.Context) ([]models.Deployment, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT d.id, d.model_alias, d.provider, d.provider_model, d.api_key_env,
		        d.credential_id, COALESCE(pc.name, ''), d.org_id, d.api_base, d.capabilities, d.priority, d.is_active, d.created_at, d.routing_strategy, d.weight, COALESCE(d.variant_id, '')
		 FROM deployments d LEFT JOIN provider_credentials pc ON d.credential_id = pc.id
		 WHERE d.is_active = true ORDER BY d.model_alias, d.priority`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deps []models.Deployment
	for rows.Next() {
		var d models.Deployment
		if err := rows.Scan(&d.ID, &d.ModelAlias, &d.Provider, &d.ProviderModel, &d.APIKeyEnv,
			&d.CredentialID, &d.CredentialName, &d.OrgID, &d.APIBase, &d.Capabilities, &d.Priority, &d.IsActive, &d.CreatedAt, &d.RoutingStrategy, &d.Weight, &d.VariantID); err != nil {
			return nil, err
		}
		deps = append(deps, d)
	}
	return deps, nil
}

func (db *DB) GetActiveDeploymentsForOrg(ctx context.Context, orgID uuid.UUID) ([]models.Deployment, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT d.id, d.model_alias, d.provider, d.provider_model, d.api_key_env,
		        d.credential_id, COALESCE(pc.name, ''), d.org_id, d.api_base, d.capabilities, d.priority, d.is_active, d.created_at, d.routing_strategy, d.weight, COALESCE(d.variant_id, '')
		 FROM deployments d LEFT JOIN provider_credentials pc ON d.credential_id = pc.id
		 WHERE d.is_active = true AND d.org_id = $1 ORDER BY d.model_alias, d.priority`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deps []models.Deployment
	for rows.Next() {
		var d models.Deployment
		if err := rows.Scan(&d.ID, &d.ModelAlias, &d.Provider, &d.ProviderModel, &d.APIKeyEnv,
			&d.CredentialID, &d.CredentialName, &d.OrgID, &d.APIBase, &d.Capabilities, &d.Priority, &d.IsActive, &d.CreatedAt, &d.RoutingStrategy, &d.Weight, &d.VariantID); err != nil {
			return nil, err
		}
		deps = append(deps, d)
	}
	return deps, nil
}

func (db *DB) UpdateDeployment(ctx context.Context, dep *models.Deployment) error {
	if dep.RoutingStrategy == "" {
		dep.RoutingStrategy = "round-robin"
	}
	if dep.Weight <= 0 {
		dep.Weight = 1
	}
	_, err := db.pool.Exec(ctx,
		`UPDATE deployments SET model_alias=$1, provider=$2, provider_model=$3, api_key_env=$4,
		 credential_id=$5, api_base=$6, capabilities=$7, priority=$8, is_active=$9, org_id=$10,
		 routing_strategy=$11, weight=$12 WHERE id=$13`,
		dep.ModelAlias, dep.Provider, dep.ProviderModel, dep.APIKeyEnv,
		dep.CredentialID, dep.APIBase, dep.Capabilities, dep.Priority, dep.IsActive, dep.OrgID,
		dep.RoutingStrategy, dep.Weight, dep.ID)
	return err
}

// UpdateAliasStrategy bulk-updates routing_strategy for all deployments of a given (org_id, model_alias).
// orgID may be nil to target global deployments.
func (db *DB) UpdateAliasStrategy(ctx context.Context, orgID *uuid.UUID, alias string, strategy string) error {
	if orgID == nil {
		_, err := db.pool.Exec(ctx,
			`UPDATE deployments SET routing_strategy=$1 WHERE model_alias=$2 AND org_id IS NULL`,
			strategy, alias)
		return err
	}
	_, err := db.pool.Exec(ctx,
		`UPDATE deployments SET routing_strategy=$1 WHERE model_alias=$2 AND org_id=$3`,
		strategy, alias, *orgID)
	return err
}

func (db *DB) DeleteDeployment(ctx context.Context, id uuid.UUID) error {
	_, err := db.pool.Exec(ctx, `DELETE FROM deployments WHERE id = $1`, id)
	return err
}

func (db *DB) CountDeployments(ctx context.Context) (int, error) {
	var count int
	err := db.pool.QueryRow(ctx, `SELECT COUNT(*) FROM deployments`).Scan(&count)
	return count, err
}

// --- Organization operations ---

func (db *DB) CreateOrganization(ctx context.Context, org *models.Organization) error {
	return db.pool.QueryRow(ctx,
		`INSERT INTO organizations (name, slug, max_budget, is_active)
		 VALUES ($1, $2, $3, $4) RETURNING id, created_at, updated_at`,
		org.Name, org.Slug, org.MaxBudget, org.IsActive,
	).Scan(&org.ID, &org.CreatedAt, &org.UpdatedAt)
}

func (db *DB) GetOrganization(ctx context.Context, id uuid.UUID) (*models.Organization, error) {
	o := &models.Organization{}
	err := db.pool.QueryRow(ctx,
		`SELECT id, name, slug, max_budget, total_spend, is_active, created_at, updated_at
		 FROM organizations WHERE id = $1`, id,
	).Scan(&o.ID, &o.Name, &o.Slug, &o.MaxBudget, &o.TotalSpend, &o.IsActive, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return o, nil
}

func (db *DB) ListOrganizations(ctx context.Context) ([]models.Organization, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT id, name, slug, max_budget, total_spend, is_active, created_at, updated_at
		 FROM organizations ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orgs []models.Organization
	for rows.Next() {
		var o models.Organization
		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &o.MaxBudget, &o.TotalSpend, &o.IsActive, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		orgs = append(orgs, o)
	}
	return orgs, nil
}

func (db *DB) UpdateOrganization(ctx context.Context, org *models.Organization) error {
	_, err := db.pool.Exec(ctx,
		`UPDATE organizations SET name=$1, slug=$2, max_budget=$3, is_active=$4, updated_at=now() WHERE id=$5`,
		org.Name, org.Slug, org.MaxBudget, org.IsActive, org.ID)
	return err
}

func (db *DB) DeleteOrganization(ctx context.Context, id uuid.UUID) error {
	_, err := db.pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, id)
	return err
}

// --- Gateway Settings operations ---

func (db *DB) GetSetting(ctx context.Context, key string) ([]byte, error) {
	var value []byte
	err := db.pool.QueryRow(ctx, `SELECT value FROM gateway_settings WHERE key = $1`, key).Scan(&value)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return value, nil
}

func (db *DB) UpsertSetting(ctx context.Context, key string, value []byte) error {
	_, err := db.pool.Exec(ctx,
		`INSERT INTO gateway_settings (key, value) VALUES ($1, $2)
		 ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = now()`, key, value)
	return err
}

// --- Cascade operations ---

func (db *DB) DeactivateUserKeys(ctx context.Context, userID uuid.UUID) error {
	_, err := db.pool.Exec(ctx,
		`UPDATE api_keys SET is_active = false, updated_at = now() WHERE user_id = $1 AND is_active = true`, userID)
	return err
}

func (db *DB) DeactivateTeamKeys(ctx context.Context, teamID uuid.UUID) error {
	_, err := db.pool.Exec(ctx,
		`UPDATE api_keys SET is_active = false, updated_at = now() WHERE team_id = $1 AND is_active = true`, teamID)
	return err
}

func (db *DB) TransferAPIKey(ctx context.Context, keyID uuid.UUID, newUserID uuid.UUID) error {
	_, err := db.pool.Exec(ctx,
		`UPDATE api_keys SET user_id = $1, updated_at = now() WHERE id = $2`, newUserID, keyID)
	return err
}

func (db *DB) ListAPIKeysForUser(ctx context.Context, userID uuid.UUID) ([]models.APIKey, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT id, name, team_id, user_id, models, rpm_limit, tpm_limit,
		        max_budget, total_spend, is_active, expires_at, created_at, updated_at
		 FROM api_keys WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []models.APIKey
	for rows.Next() {
		var k models.APIKey
		if err := rows.Scan(&k.ID, &k.Name, &k.TeamID, &k.UserID, &k.Models,
			&k.RPMLimit, &k.TPMLimit, &k.MaxBudget, &k.TotalSpend,
			&k.IsActive, &k.ExpiresAt, &k.CreatedAt, &k.UpdatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, nil
}

func (db *DB) GetSpendLogsForUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]models.SpendLog, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT sl.id, sl.api_key_id, sl.deployment_id, sl.model_alias, sl.provider, sl.endpoint,
		        sl.prompt_tokens, sl.completion_tokens, sl.total_tokens, sl.cost_usd, sl.latency_ms, sl.status_code, sl.created_at
		 FROM spend_logs sl
		 WHERE sl.api_key_id IN (SELECT id FROM api_keys WHERE user_id = $1)
		 ORDER BY sl.created_at DESC LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []models.SpendLog
	for rows.Next() {
		var l models.SpendLog
		if err := rows.Scan(&l.ID, &l.APIKeyID, &l.DeploymentID, &l.ModelAlias, &l.Provider,
			&l.Endpoint, &l.PromptTokens, &l.CompletionTokens, &l.TotalTokens,
			&l.CostUSD, &l.LatencyMS, &l.StatusCode, &l.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, nil
}

func (db *DB) GetDailySpendForUser(ctx context.Context, userID uuid.UUID, days int) ([]models.DailySpend, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT ds.api_key_id, ds.team_id, ds.date, ds.total_tokens, ds.total_cost_usd, ds.request_count
		 FROM daily_spend ds
		 WHERE ds.api_key_id IN (SELECT id FROM api_keys WHERE user_id = $1)
		   AND ds.date >= now() - $2::interval
		 ORDER BY ds.date DESC`,
		userID, fmt.Sprintf("%d days", days))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var spends []models.DailySpend
	for rows.Next() {
		var s models.DailySpend
		if err := rows.Scan(&s.APIKeyID, &s.TeamID, &s.Date, &s.TotalTokens,
			&s.TotalCostUSD, &s.RequestCount); err != nil {
			return nil, err
		}
		spends = append(spends, s)
	}
	return spends, nil
}

func (db *DB) GetSpendLogsForOrg(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]models.SpendLog, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT sl.id, sl.api_key_id, sl.deployment_id, sl.model_alias, sl.provider, sl.endpoint,
		        sl.prompt_tokens, sl.completion_tokens, sl.total_tokens, sl.cost_usd, sl.latency_ms, sl.status_code, sl.created_at
		 FROM spend_logs sl
		 WHERE sl.api_key_id IN (
		   SELECT ak.id FROM api_keys ak
		   LEFT JOIN teams t ON ak.team_id = t.id
		   LEFT JOIN users u ON ak.user_id = u.id
		   WHERE t.org_id = $1 OR u.org_id = $1
		 )
		 ORDER BY sl.created_at DESC LIMIT $2 OFFSET $3`, orgID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []models.SpendLog
	for rows.Next() {
		var l models.SpendLog
		if err := rows.Scan(&l.ID, &l.APIKeyID, &l.DeploymentID, &l.ModelAlias, &l.Provider,
			&l.Endpoint, &l.PromptTokens, &l.CompletionTokens, &l.TotalTokens,
			&l.CostUSD, &l.LatencyMS, &l.StatusCode, &l.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, nil
}

func (db *DB) GetDailySpendForOrg(ctx context.Context, orgID uuid.UUID, days int) ([]models.DailySpend, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT ds.api_key_id, ds.team_id, ds.date, ds.total_tokens, ds.total_cost_usd, ds.request_count
		 FROM daily_spend ds
		 WHERE ds.api_key_id IN (
		   SELECT ak.id FROM api_keys ak
		   LEFT JOIN teams t ON ak.team_id = t.id
		   LEFT JOIN users u ON ak.user_id = u.id
		   WHERE t.org_id = $1 OR u.org_id = $1
		 )
		   AND ds.date >= now() - $2::interval
		 ORDER BY ds.date DESC`,
		orgID, fmt.Sprintf("%d days", days))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var spends []models.DailySpend
	for rows.Next() {
		var s models.DailySpend
		if err := rows.Scan(&s.APIKeyID, &s.TeamID, &s.Date, &s.TotalTokens,
			&s.TotalCostUSD, &s.RequestCount); err != nil {
			return nil, err
		}
		spends = append(spends, s)
	}
	return spends, nil
}

func (db *DB) DeploymentExistsByAliasProviderOrg(ctx context.Context, alias, provider string, orgID *uuid.UUID) (bool, error) {
	var exists bool
	var err error
	if orgID != nil {
		err = db.pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM deployments WHERE model_alias=$1 AND provider=$2 AND org_id=$3)`,
			alias, provider, *orgID).Scan(&exists)
	} else {
		err = db.pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM deployments WHERE model_alias=$1 AND provider=$2 AND org_id IS NULL)`,
			alias, provider).Scan(&exists)
	}
	return exists, err
}

func (db *DB) UpdateAPIKey(ctx context.Context, key *models.APIKey) error {
	_, err := db.pool.Exec(ctx,
		`UPDATE api_keys SET name=$1, team_id=$2, user_id=$3, models=$4, rpm_limit=$5, tpm_limit=$6, max_budget=$7, is_active=$8, expires_at=$9, updated_at=now() WHERE id=$10`,
		key.Name, key.TeamID, key.UserID, key.Models, key.RPMLimit, key.TPMLimit, key.MaxBudget, key.IsActive, key.ExpiresAt, key.ID)
	return err
}
