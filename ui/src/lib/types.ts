/** Mirrors backend models and cost types. */

export interface APIKey {
  id: string;
  name: string;
  team_id?: string | null;
  user_id?: string | null;
  models?: string[];
  rpm_limit?: number | null;
  tpm_limit?: number | null;
  max_budget?: number | null;
  total_spend: number;
  is_active: boolean;
  expires_at?: string | null;
  created_at: string;
  updated_at: string;
}

// Session keys are auto-provisioned at signup with name "session:<email>" and a
// short expiry; they authenticate the dashboard user but are not real gateway
// API keys. Use this helper to exclude them from any user-facing key listing
// or onboarding "did the user create a key?" checks.
export function isUserApiKey(k: APIKey): boolean {
  return !k.name.startsWith('session:');
}

export interface Team {
  id: string;
  name: string;
  org_id?: string | null;
  models?: string[];
  rpm_limit?: number | null;
  tpm_limit?: number | null;
  max_budget?: number | null;
  total_spend: number;
  created_at: string;
  updated_at: string;
}

export interface SpendLog {
  id: string;
  api_key_id?: string | null;
  deployment_id?: string | null;
  model_alias: string;
  provider: string;
  endpoint: string;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  cost_usd: number;
  latency_ms: number;
  status_code: number;
  trace_id?: string;
  created_at: string;
}

export interface DailySpend {
  id: string;
  api_key_id?: string | null;
  team_id?: string | null;
  date: string;
  total_tokens: number;
  total_cost_usd: number;
  request_count: number;
}

export interface DeploymentRow {
  id?: string;
  model_alias: string;
  provider: string;
  provider_model: string;
  api_key_env?: string;
  credential_id?: string | null;
  credential_name?: string;
  org_id?: string | null;
  api_base?: string;
  priority: number;
  weight?: number;
  routing_strategy?: string;
  is_active?: boolean;
}

export type RoutingStrategy =
  | 'round-robin'
  | 'least-latency'
  | 'priority'
  | 'cheapest'
  | 'weighted';

export interface ModelPricing {
  provider: string;
  model: string;
  mode: string;
  input_cost_per_token: number;
  output_cost_per_token: number;
  input_cost_per_image?: number;
  input_cost_per_character?: number;
  input_cost_per_second?: number;
  cache_read_cost_per_token?: number;
  max_input_tokens?: number;
  max_output_tokens?: number;
  size_pricing?: Record<string, number>;
}

// Aggregated quality signal returned by GET /v1/metrics. One row per
// (model_alias, variant_id, metric) tuple over the requested window.
export interface FeedbackMetric {
  model_alias: string;
  variant_id?: string | null;
  metric: string;
  count: number;
  avg_float?: number | null;
  true_rate?: number | null;
}

export interface SmartRouteStats {
  enabled: boolean;
  total_decisions?: number;
  tier_overrides?: Record<string, number>;
  ml_model?: { loaded: boolean; trained_at?: string; samples?: number };
  shadow?: {
    launched?: number;
    completed?: number;
    shadow_wins?: number;
    primary_wins?: number;
  };
}

export interface SemanticCacheStats {
  enabled: boolean;
  hits?: number;
  misses?: number;
}

export interface GatewayMetrics {
  days?: number;
  data: FeedbackMetric[];
  smart_route: SmartRouteStats;
  semantic_cache: SemanticCacheStats;
}

export interface AuditEvent {
  id: string;
  occurred_at: string;
  actor_type: string;
  actor_id?: string;
  action: string;
  resource?: string;
  org_id?: string | null;
  team_id?: string | null;
  api_key_id?: string | null;
  ip?: string;
  user_agent?: string;
  metadata?: Record<string, unknown>;
}

export interface CustomPricingPayload {
  input_cost_per_token?: number | null;
  output_cost_per_token?: number | null;
  cache_read_cost_per_token?: number | null;
  input_cost_per_image?: number | null;
  input_cost_per_character?: number | null;
  input_cost_per_second?: number | null;
  size_pricing?: Record<string, number>;
}

export interface CreateKeyResponse {
  key: string;
  id: string;
  name: string;
  team_id?: string | null;
  models?: string[];
}

export type UserRole = 'super_admin' | 'org_admin' | 'team_admin' | 'member' | 'viewer' | 'admin' | 'user';

export interface User {
  id: string;
  email: string;
  role: UserRole;
  team_id?: string | null;
  org_id?: string | null;
  is_active: boolean;
  created_at: string;
  updated_at: string;
}

export interface Organization {
  id: string;
  name: string;
  slug: string;
  max_budget?: number | null;
  total_spend: number;
  is_active: boolean;
  created_at: string;
  updated_at: string;
}

export interface ProviderCredential {
  id: string;
  name: string;
  provider: string;
  api_key_masked: string;
  api_base?: string;
  org_id?: string | null;
  is_active: boolean;
  created_by?: string | null;
  created_at: string;
  updated_at: string;
}

export interface CallbackInfo {
  name: string;
  type: string;
}

export interface GatewayConfig {
  server?: Record<string, unknown>;
  database?: Record<string, unknown>;
  redis?: Record<string, unknown>;
  auth?: Record<string, unknown>;
  model_list?: unknown[];
  routing?: Record<string, unknown>;
  rate_limiting?: Record<string, unknown>;
  logging?: Record<string, unknown>;
  callbacks?: Array<{
    type: string;
    endpoint?: string;
    service_name?: string;
    project?: string;
    model_id?: string;
  }>;
  [key: string]: unknown;
}

export interface AuthSession {
  type: 'master_key' | 'user_session';
  token: string;
  user?: User;
}

export interface DiscoveredModel {
  id: string;
  provider: string;
  owned_by: string;
  capabilities: string[];
}

export interface ProviderInfo {
  id: string;
  label: string;
  supports_discovery: boolean;
  requires_api_base: boolean;
  api_key_hint?: string;
  api_base_hint?: string;
  model_id_hint?: string;
  docs_url?: string;
}

export interface SyncResult {
  created: number;
  skipped: number;
  routes: DeploymentRow[];
}

// ---- Smart-routing moat ----

// RoutingSavings mirrors backend models.RoutingSavings (only the fields
// the dashboard needs for table + drill-down rendering).
export interface RoutingSavings {
  id: string;
  recording_id?: string | null;
  trace_id: string;
  org_id?: string | null;
  api_key_id?: string | null;
  requested_alias: string;
  served_alias: string;
  served_provider?: string;
  served_model?: string;
  baseline_provider?: string;
  baseline_model?: string;
  baseline_input_tokens: number;
  baseline_output_tokens: number;
  served_input_tokens: number;
  served_output_tokens: number;
  baseline_cost_usd: number;
  actual_cost_usd: number;
  discount_pct_applied: number;
  savings_usd: number;
  quality_score?: number | null;
  quality_pass?: boolean | null;
  quality_scorer?: string;
  retried: boolean;
  strategy: string;
  complexity_score: number;
  complexity_bucket?: string;
  overridden: boolean;
  prev_hash?: string;
  signature?: string;
  signed_by_key_id?: string;
  signed_at?: string | null;
  created_at: string;
}

// DailySavings mirrors backend models.DailySavings used for the
// time-series chart and our_cut billing rollups.
export interface DailySavings {
  date: string;
  org_id?: string | null;
  total_requests: number;
  routed_requests: number;
  total_savings_usd: number;
  quality_pass_pct: number;
  total_retries: number;
  our_cut_usd: number;
}

// SavingsVerifyResult is returned from GET /savings/{id}/verify.
export interface SavingsVerifyResult {
  verified: boolean;
  reason?: string;
  signed_by_key_id?: string;
  signed_at?: string | null;
  row: RoutingSavings;
}

// RoutingPolicy mirrors backend models.RoutingPolicy.
export interface RoutingPolicy {
  id: string;
  model_alias: string;
  org_id?: string | null;
  strategy: string;
  quality_threshold: number;
  baseline_provider_model?: string;
  judge_alias?: string;
  retry_when_streaming: boolean;
  sample_pct: number;
  min_samples_before_routing: number;
  created_at: string;
  updated_at: string;
}

// OperatorPriceCatalog mirrors backend models.OperatorPriceCatalog.
export interface OperatorPriceCatalog {
  id: string;
  provider: string;
  model: string;
  input_cost_per_token: number;
  output_cost_per_token: number;
  effective_from: string;
  effective_to?: string | null;
  signed_at?: string | null;
  signed_by_key_id?: string;
  signature?: string;
}

// OrgProviderDiscount mirrors backend models.OrgProviderDiscount.
export interface OrgProviderDiscount {
  id: string;
  org_id: string;
  provider: string;
  discount_pct: number;
  evidence_url?: string;
  declared_by?: string;
  declared_at?: string | null;
  attested_by?: string | null;
  attested_at?: string | null;
  operator_signature?: string;
  effective_from: string;
  effective_to?: string | null;
}
