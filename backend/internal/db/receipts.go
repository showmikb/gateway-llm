package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gateway-llm/gateway-llm/internal/receipt"
	"github.com/google/uuid"
)

// InsertReceipt appends a signed receipt. All fields of the envelope are
// stored in a single `body` JSONB column so an auditor can reconstruct
// the canonical bytes and re-verify the signature without any server-
// side logic. Columns outside `body` are denormalized for indexed lookup.
func (db *DB) InsertReceipt(ctx context.Context, r *receipt.Receipt) error {
	if r == nil {
		return fmt.Errorf("nil receipt")
	}
	body, err := json.Marshal(r)
	if err != nil {
		return err
	}
	id, err := uuid.Parse(r.ID)
	if err != nil {
		return fmt.Errorf("receipt id: %w", err)
	}
	var orgID, keyID, prevID *uuid.UUID
	if r.OrgID != "" {
		if u, err := uuid.Parse(r.OrgID); err == nil {
			orgID = &u
		}
	}
	if r.APIKeyID != "" {
		if u, err := uuid.Parse(r.APIKeyID); err == nil {
			keyID = &u
		}
	}
	if r.PrevReceiptID != "" {
		if u, err := uuid.Parse(r.PrevReceiptID); err == nil {
			prevID = &u
		}
	}
	_, err = db.pool.Exec(ctx, `
		INSERT INTO receipts (
			id, created_at, trace_id, org_id, api_key_id,
			alias, provider, provider_model, region,
			req_hash, resp_hash,
			prompt_tokens, output_tokens, total_tokens, cost_usd,
			prev_receipt_id, prev_hash, public_key_id, sig, body
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9,
			$10, $11,
			$12, $13, $14, $15,
			$16, $17, $18, $19, $20
		) ON CONFLICT (id) DO NOTHING`,
		id, r.Timestamp, nullableStr(r.TraceID), orgID, keyID,
		r.Alias, r.Provider, r.ProviderModel, nullableStr(r.Region),
		r.ReqHash, r.RespHash,
		r.PromptTokens, r.OutputTokens, r.TotalTokens, r.CostUSD,
		prevID, nullableStr(r.PrevHash), r.PublicKeyID, r.Signature, string(body),
	)
	return err
}

// GetReceipt retrieves the stored receipt body for id. The body is the
// canonical signed envelope; the caller can re-verify it.
func (db *DB) GetReceipt(ctx context.Context, id string) (*receipt.Receipt, error) {
	u, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("receipt id: %w", err)
	}
	var body []byte
	if err := db.pool.QueryRow(ctx,
		`SELECT body FROM receipts WHERE id = $1`, u,
	).Scan(&body); err != nil {
		return nil, err
	}
	var r receipt.Receipt
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	return &r, nil
}
