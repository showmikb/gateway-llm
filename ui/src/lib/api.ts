import type {
  APIKey,
  AuditEvent,
  CallbackInfo,
  CreateKeyResponse,
  CustomPricingPayload,
  DailySavings,
  DailySpend,
  DeploymentRow,
  DiscoveredModel,
  GatewayConfig,
  GatewayMetrics,
  ModelPricing,
  OperatorPriceCatalog,
  Organization,
  OrgProviderDiscount,
  ProviderCredential,
  ProviderInfo,
  RoutingPolicy,
  RoutingSavings,
  SavingsVerifyResult,
  SpendLog,
  SyncResult,
  Team,
  User,
} from '@/lib/types';

const API_URL = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';

const MGMT = '/v1/management';

function getAuthToken(): string {
  if (typeof window === 'undefined') return '';
  return localStorage.getItem('gatewayllm_token') || localStorage.getItem('gatewayllm_master_key') || '';
}

export async function fetchAPI(path: string, options?: RequestInit): Promise<unknown> {
  const token = getAuthToken();
  if (!token && typeof window !== 'undefined') {
    window.location.href = '/login';
    throw new Error('Not authenticated');
  }
  const res = await fetch(`${API_URL}${path}`, {
    ...options,
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  });
  if (res.status === 401 && typeof window !== 'undefined') {
    localStorage.removeItem('gatewayllm_master_key');
    localStorage.removeItem('gatewayllm_token');
    localStorage.removeItem('gatewayllm_user');
    window.location.href = '/login';
    throw new Error('Session expired');
  }
  if (!res.ok) {
    const body = await res.text();
    // Backend returns errors as {"error": {"message": "...", "type": "..."}}.
    // Surface the human-readable message so UI alerts stay readable.
    try {
      const parsed = JSON.parse(body) as { error?: { message?: string }; message?: string };
      const msg = parsed?.error?.message ?? parsed?.message ?? body;
      throw new Error(msg || `HTTP ${res.status}`);
    } catch (e) {
      if (e instanceof Error && e.message && e.message !== 'Unexpected end of JSON input') {
        throw e;
      }
      throw new Error(body || `HTTP ${res.status}`);
    }
  }
  if (res.status === 204) return null;
  const text = await res.text();
  if (!text) return null;
  return JSON.parse(text);
}

function pmPath(provider: string, model: string) {
  return `${MGMT}/pricing/${encodeURIComponent(provider)}/${encodeURIComponent(model)}`;
}

// --- API Keys ---

export async function getKeys(): Promise<APIKey[]> {
  const data = (await fetchAPI(`${MGMT}/keys`)) as { data?: APIKey[] };
  return data.data ?? [];
}

export async function createKey(body: {
  name: string;
  team_id?: string | null;
  user_id?: string | null;
  models?: string[];
  rpm_limit?: number | null;
  tpm_limit?: number | null;
  max_budget?: number | null;
  expires_at?: string | null;
}): Promise<CreateKeyResponse> {
  return fetchAPI(`${MGMT}/keys`, { method: 'POST', body: JSON.stringify(body) }) as Promise<CreateKeyResponse>;
}

export async function updateKey(
  id: string,
  body: Partial<{
    name: string;
    team_id: string | null;
    user_id: string | null;
    models: string[];
    rpm_limit: number | null;
    tpm_limit: number | null;
    max_budget: number | null;
    is_active: boolean;
  }>,
): Promise<APIKey> {
  return fetchAPI(`${MGMT}/keys/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(body),
  }) as Promise<APIKey>;
}

export async function deleteKey(id: string): Promise<void> {
  await fetchAPI(`${MGMT}/keys/${encodeURIComponent(id)}`, { method: 'DELETE' });
}

export async function getKeyUsage(
  id: string,
  limit = 100,
  offset = 0,
): Promise<{ data: SpendLog[]; key: APIKey }> {
  const q = new URLSearchParams({ limit: String(limit), offset: String(offset) });
  return (await fetchAPI(`${MGMT}/keys/${encodeURIComponent(id)}/usage?${q}`)) as {
    data: SpendLog[];
    key: APIKey;
  };
}

export async function getKeyDailyUsage(id: string, days = 30): Promise<DailySpend[]> {
  const q = new URLSearchParams({ days: String(days) });
  const data = (await fetchAPI(`${MGMT}/keys/${encodeURIComponent(id)}/usage/daily?${q}`)) as {
    data?: DailySpend[];
  };
  return data.data ?? [];
}

// --- Teams ---

export async function getTeams(): Promise<Team[]> {
  const data = (await fetchAPI(`${MGMT}/teams`)) as { data?: Team[] };
  return data.data ?? [];
}

export async function createTeam(body: {
  name: string;
  org_id?: string | null;
  models?: string[];
  rpm_limit?: number | null;
  tpm_limit?: number | null;
  max_budget?: number | null;
}): Promise<Team> {
  return fetchAPI(`${MGMT}/teams`, { method: 'POST', body: JSON.stringify(body) }) as Promise<Team>;
}

export async function deleteTeam(id: string): Promise<void> {
  await fetchAPI(`${MGMT}/teams/${encodeURIComponent(id)}`, { method: 'DELETE' });
}

// --- Usage ---

export async function getUsage(limit = 50, offset = 0): Promise<SpendLog[]> {
  const q = new URLSearchParams({ limit: String(limit), offset: String(offset) });
  const data = (await fetchAPI(`${MGMT}/usage?${q}`)) as { data?: SpendLog[] };
  return data.data ?? [];
}

export async function getDailyUsage(days = 30): Promise<DailySpend[]> {
  const q = new URLSearchParams({ days: String(days) });
  const data = (await fetchAPI(`${MGMT}/usage/daily?${q}`)) as { data?: DailySpend[] };
  return data.data ?? [];
}

// --- Smart-routing moat: savings ledger ---

export interface SavingsAliasBreakdown {
  requested_alias: string;
  served_alias: string;
  requests: number;
  routed: number;
  savings_usd: number;
  baseline_usd: number;
  actual_usd: number;
  quality_pass_pct?: number | null;
  avg_quality_score?: number | null;
}

export interface SavingsListResponse {
  data: RoutingSavings[];
  alias_breakdown?: SavingsAliasBreakdown[];
}

export async function getSavings(limit = 50, offset = 0, alias?: string): Promise<SavingsListResponse> {
  const params = new URLSearchParams({ limit: String(limit), offset: String(offset) });
  if (alias) params.set('alias', alias);
  return (await fetchAPI(`${MGMT}/savings?${params}`)) as SavingsListResponse;
}

export async function getDailySavings(days = 30): Promise<DailySavings[]> {
  const q = new URLSearchParams({ days: String(days) });
  const data = (await fetchAPI(`${MGMT}/savings/daily?${q}`)) as { data?: DailySavings[] };
  return data.data ?? [];
}

export async function getSavingsByID(id: string): Promise<RoutingSavings> {
  return (await fetchAPI(`${MGMT}/savings/${encodeURIComponent(id)}`)) as RoutingSavings;
}

export async function getSavingsByTrace(traceID: string): Promise<RoutingSavings> {
  return (await fetchAPI(`${MGMT}/savings/by-trace/${encodeURIComponent(traceID)}`)) as RoutingSavings;
}

export async function verifySavings(id: string): Promise<SavingsVerifyResult> {
  return (await fetchAPI(`${MGMT}/savings/${encodeURIComponent(id)}/verify`)) as SavingsVerifyResult;
}

// --- Routing policies ---

export async function getRoutingPolicies(): Promise<RoutingPolicy[]> {
  const data = (await fetchAPI(`${MGMT}/routing-policy`)) as { data?: RoutingPolicy[] };
  return data.data ?? [];
}

export async function upsertRoutingPolicy(alias: string, body: Partial<RoutingPolicy>): Promise<RoutingPolicy> {
  return (await fetchAPI(`${MGMT}/routing-policy/${encodeURIComponent(alias)}`, {
    method: 'PUT',
    body: JSON.stringify(body),
  })) as RoutingPolicy;
}

export async function deleteRoutingPolicy(alias: string): Promise<void> {
  await fetchAPI(`${MGMT}/routing-policy/${encodeURIComponent(alias)}`, { method: 'DELETE' });
}

// --- Operator-signed price catalog ---

export async function getOperatorCatalog(): Promise<OperatorPriceCatalog[]> {
  const data = (await fetchAPI(`${MGMT}/operator/catalog`)) as { data?: OperatorPriceCatalog[] };
  return data.data ?? [];
}

// --- Negotiated discounts ---

export async function getDiscounts(): Promise<OrgProviderDiscount[]> {
  const data = (await fetchAPI(`${MGMT}/discounts`)) as { data?: OrgProviderDiscount[] };
  return data.data ?? [];
}

export async function declareDiscount(body: {
  provider: string;
  discount_pct: number;
  evidence_url?: string;
  effective_from?: string;
  effective_to?: string;
}): Promise<OrgProviderDiscount> {
  return (await fetchAPI(`${MGMT}/discounts`, {
    method: 'POST',
    body: JSON.stringify(body),
  })) as OrgProviderDiscount;
}

export async function countersignDiscount(id: string): Promise<OrgProviderDiscount> {
  return (await fetchAPI(`${MGMT}/discounts/${encodeURIComponent(id)}/countersign`, {
    method: 'POST',
  })) as OrgProviderDiscount;
}

export async function revokeDiscount(id: string): Promise<void> {
  await fetchAPI(`${MGMT}/discounts/${encodeURIComponent(id)}`, { method: 'DELETE' });
}

// --- Gateway-wide metrics ---

// Returns aggregated quality signals plus live smart-route / semantic-cache
// counters from the gateway. Backed by GET /v1/metrics.
export async function getGatewayMetrics(days = 7): Promise<GatewayMetrics> {
  const q = new URLSearchParams({ days: String(days) });
  const out = (await fetchAPI(`/v1/metrics?${q}`)) as Partial<GatewayMetrics>;
  return {
    days: out.days,
    data: out.data ?? [],
    smart_route: out.smart_route ?? { enabled: false },
    semantic_cache: out.semantic_cache ?? { enabled: false },
  };
}

// --- Audit log ---

export async function getAuditEvents(limit = 25): Promise<AuditEvent[]> {
  const q = new URLSearchParams({ limit: String(limit) });
  const data = (await fetchAPI(`/v1/audit?${q}`)) as { data?: AuditEvent[] };
  return data.data ?? [];
}

// --- Deployments ---

export async function getDeployments(): Promise<DeploymentRow[]> {
  const data = (await fetchAPI(`${MGMT}/deployments`)) as { data?: DeploymentRow[] };
  return data.data ?? [];
}

export async function createDeployment(body: {
  model_alias: string;
  provider: string;
  provider_model: string;
  api_key_env?: string;
  credential_id?: string | null;
  org_id?: string | null;
  api_base?: string;
  priority?: number;
  weight?: number;
  routing_strategy?: string;
}): Promise<DeploymentRow> {
  return fetchAPI(`${MGMT}/deployments`, { method: 'POST', body: JSON.stringify(body) }) as Promise<DeploymentRow>;
}

export async function updateAliasStrategy(body: {
  model_alias: string;
  routing_strategy: string;
  org_id?: string | null;
}): Promise<{ model_alias: string; routing_strategy: string }> {
  return fetchAPI(`${MGMT}/deployments/alias-strategy`, {
    method: 'POST',
    body: JSON.stringify(body),
  }) as Promise<{ model_alias: string; routing_strategy: string }>;
}

export async function updateDeployment(
  id: string,
  body: Partial<DeploymentRow>,
): Promise<DeploymentRow> {
  return fetchAPI(`${MGMT}/deployments/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(body),
  }) as Promise<DeploymentRow>;
}

export async function deleteDeployment(id: string): Promise<void> {
  await fetchAPI(`${MGMT}/deployments/${encodeURIComponent(id)}`, { method: 'DELETE' });
}

// --- Pricing ---

export async function getPricing(): Promise<ModelPricing[]> {
  const data = (await fetchAPI(`${MGMT}/pricing`)) as { data?: ModelPricing[] };
  return data.data ?? [];
}

export async function setPricing(
  provider: string,
  model: string,
  body: CustomPricingPayload,
): Promise<{ status: string }> {
  return fetchAPI(pmPath(provider, model), {
    method: 'PUT',
    body: JSON.stringify(body),
  }) as Promise<{ status: string }>;
}

export async function deletePricing(provider: string, model: string): Promise<void> {
  await fetchAPI(pmPath(provider, model), { method: 'DELETE' });
}

// --- Config ---

export async function getConfig(): Promise<GatewayConfig> {
  return fetchAPI('/api/config') as Promise<GatewayConfig>;
}

export async function updateConfig(body: Record<string, unknown>): Promise<{ status: string }> {
  return fetchAPI('/api/config', { method: 'PUT', body: JSON.stringify(body) }) as Promise<{ status: string }>;
}

// --- Users ---

export async function getUsers(): Promise<User[]> {
  const data = (await fetchAPI(`${MGMT}/users`)) as { data?: User[] };
  return data.data ?? [];
}

export async function createUser(body: {
  email: string;
  password: string;
  role: string;
  team_id?: string | null;
}): Promise<User> {
  return fetchAPI(`${MGMT}/users`, { method: 'POST', body: JSON.stringify(body) }) as Promise<User>;
}

export async function updateUser(
  id: string,
  body: { email?: string; role?: string; team_id?: string | null; is_active?: boolean },
): Promise<User> {
  return fetchAPI(`${MGMT}/users/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(body),
  }) as Promise<User>;
}

export async function deleteUser(id: string): Promise<void> {
  await fetchAPI(`${MGMT}/users/${encodeURIComponent(id)}`, { method: 'DELETE' });
}

export async function loginUser(
  email: string,
  password: string,
): Promise<{ token: string; expires_at: string; user: User }> {
  const API = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';
  const res = await fetch(`${API}${MGMT}/users/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password }),
  });
  if (!res.ok) throw new Error(await res.text());
  return res.json();
}

export async function registerUser(
  email: string,
  password: string,
  name?: string,
): Promise<{
  token: string;
  expires_at: string;
  user: User;
  organization: Organization;
  team: Team;
}> {
  const API = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';
  const res = await fetch(`${API}${MGMT}/users/register`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, password, name }),
  });
  if (!res.ok) {
    const body = await res.text();
    try {
      const parsed = JSON.parse(body);
      throw new Error(parsed?.error?.message || body || 'Registration failed');
    } catch {
      throw new Error(body || 'Registration failed');
    }
  }
  return res.json();
}

// --- Credentials ---

export async function getCredentials(): Promise<ProviderCredential[]> {
  const data = (await fetchAPI(`${MGMT}/credentials`)) as { data?: ProviderCredential[] };
  return data.data ?? [];
}

export async function createCredential(body: {
  name: string;
  provider: string;
  api_key: string;
  api_base?: string;
  org_id?: string | null;
}): Promise<ProviderCredential> {
  return fetchAPI(`${MGMT}/credentials`, { method: 'POST', body: JSON.stringify(body) }) as Promise<ProviderCredential>;
}

export async function updateCredential(
  id: string,
  body: Partial<{ name: string; provider: string; api_key: string; api_base: string; is_active: boolean }>,
): Promise<ProviderCredential> {
  return fetchAPI(`${MGMT}/credentials/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(body),
  }) as Promise<ProviderCredential>;
}

export async function deleteCredential(id: string): Promise<void> {
  await fetchAPI(`${MGMT}/credentials/${encodeURIComponent(id)}`, { method: 'DELETE' });
}

export async function testCredential(id: string): Promise<{ status: string; message: string }> {
  return fetchAPI(`${MGMT}/credentials/${encodeURIComponent(id)}/test`, {
    method: 'POST',
  }) as Promise<{ status: string; message: string }>;
}

export async function testRawCredential(body: {
  provider: string;
  api_key: string;
  api_base?: string;
}): Promise<{ status: string; message: string }> {
  return fetchAPI(`${MGMT}/credentials/test-raw`, {
    method: 'POST',
    body: JSON.stringify(body),
  }) as Promise<{ status: string; message: string }>;
}

// --- Organizations ---

export async function getOrganizations(): Promise<Organization[]> {
  const data = (await fetchAPI(`${MGMT}/organizations`)) as { data?: Organization[] };
  return data.data ?? [];
}

export async function createOrganization(body: {
  name: string;
  slug?: string;
  max_budget?: number | null;
}): Promise<Organization> {
  return fetchAPI(`${MGMT}/organizations`, { method: 'POST', body: JSON.stringify(body) }) as Promise<Organization>;
}

export async function updateOrganization(
  id: string,
  body: Partial<{ name: string; slug: string; max_budget: number | null; is_active: boolean }>,
): Promise<Organization> {
  return fetchAPI(`${MGMT}/organizations/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(body),
  }) as Promise<Organization>;
}

export async function deleteOrganization(id: string): Promise<void> {
  await fetchAPI(`${MGMT}/organizations/${encodeURIComponent(id)}`, { method: 'DELETE' });
}

// --- Callbacks ---

export async function getCallbacks(): Promise<CallbackInfo[]> {
  const data = (await fetchAPI(`${MGMT}/callbacks`)) as { data?: CallbackInfo[] };
  return data.data ?? [];
}

export async function testCallbacks(): Promise<Record<string, string>> {
  const data = (await fetchAPI(`${MGMT}/callbacks/test`, { method: 'POST' })) as { results?: Record<string, string> };
  return data.results ?? {};
}

// --- Provider Discovery & Route Sync ---

export async function discoverProviderModels(credentialId: string): Promise<DiscoveredModel[]> {
  const data = (await fetchAPI(`${MGMT}/providers/discover`, {
    method: 'POST',
    body: JSON.stringify({ credential_id: credentialId }),
  })) as { data?: DiscoveredModel[] };
  return data.data ?? [];
}

export async function listProviders(): Promise<ProviderInfo[]> {
  const data = (await fetchAPI(`${MGMT}/providers`)) as { data?: ProviderInfo[] };
  return data.data ?? [];
}

export async function syncRoutes(body: {
  credential_id: string;
  org_id?: string;
  models: string[];
  skip_existing: boolean;
}): Promise<SyncResult> {
  return fetchAPI(`${MGMT}/routes/sync`, {
    method: 'POST',
    body: JSON.stringify(body),
  }) as Promise<SyncResult>;
}
