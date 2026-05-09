'use client';

import { useEffect, useMemo, useState } from 'react';

import { discoverProviderModels } from '@/lib/api';
import type { DiscoveredModel, ModelPricing, ProviderInfo } from '@/lib/types';

import { Badge } from './ui';

interface ModelPickerProps {
  /** Provider id (e.g. "openai") for hint and discovery resolution. */
  provider: string;
  /** Provider catalog metadata; required for hints when discovery is unavailable. */
  providerInfo?: ProviderInfo;
  /** Selected stored credential id; needed to call discovery. */
  credentialId?: string;
  /** Pricing catalog rows the parent already loaded; used as a static suggestion fallback. */
  pricingCatalog?: ModelPricing[];
  value: string;
  onChange: (model: string) => void;
  /** Optional id used by external <datalist> consumers; kept for compatibility. */
  inputId?: string;
}

const CAPABILITY_VARIANT: Record<string, 'success' | 'warning' | 'default'> = {
  chat: 'success',
  responses: 'success',
  embedding: 'warning',
  embeddings: 'warning',
  image: 'warning',
  images: 'warning',
  audio: 'default',
  speech: 'default',
  moderation: 'default',
};

function capabilityBadge(c: string) {
  const v = CAPABILITY_VARIANT[c] ?? 'default';
  return (
    <Badge key={c} variant={v}>
      {c}
    </Badge>
  );
}

/**
 * ModelPicker is the unified backend-model selector used by both the
 * create-route wizard and the edit-target modal.
 *
 *  - When the chosen provider supports live discovery and a credential
 *    is selected, it fetches models from the upstream and renders a
 *    searchable, picker list with capability badges.
 *  - When discovery is unavailable, it shows a free-text input plus a
 *    provider-specific placeholder/docs link.
 *  - In both cases the parent's `pricingCatalog` (if any) is surfaced
 *    as a "common models" shortlist so users always see something to
 *    click on.
 */
export function ModelPicker({
  provider,
  providerInfo,
  credentialId,
  pricingCatalog,
  value,
  onChange,
  inputId,
}: ModelPickerProps) {
  const supportsDiscovery = !!providerInfo?.supports_discovery;
  const [discovered, setDiscovered] = useState<DiscoveredModel[]>([]);
  const [discoveryLoading, setDiscoveryLoading] = useState(false);
  const [discoveryError, setDiscoveryError] = useState<string | null>(null);
  const [search, setSearch] = useState('');

  useEffect(() => {
    if (!supportsDiscovery || !credentialId) {
      setDiscovered([]);
      setDiscoveryError(null);
      return;
    }
    let cancelled = false;
    setDiscoveryLoading(true);
    setDiscoveryError(null);
    discoverProviderModels(credentialId)
      .then((models) => {
        if (!cancelled) setDiscovered(models);
      })
      .catch((e: unknown) => {
        if (!cancelled) setDiscoveryError(e instanceof Error ? e.message : 'discovery failed');
      })
      .finally(() => {
        if (!cancelled) setDiscoveryLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [supportsDiscovery, credentialId]);

  const catalogForProvider = useMemo(
    () => (pricingCatalog ?? []).filter((p) => p.provider === provider),
    [pricingCatalog, provider],
  );

  const filteredDiscovered = useMemo(() => {
    if (!search) return discovered;
    const q = search.toLowerCase();
    return discovered.filter((m) => m.id.toLowerCase().includes(q));
  }, [discovered, search]);

  const filteredCatalog = useMemo(() => {
    if (!search) return catalogForProvider.slice(0, 12);
    const q = search.toLowerCase();
    return catalogForProvider.filter((p) => p.model.toLowerCase().includes(q)).slice(0, 12);
  }, [catalogForProvider, search]);

  const placeholder = providerInfo?.model_id_hint || 'Backend model id';
  const docsUrl = providerInfo?.docs_url;

  return (
    <div className="space-y-2">
      <input
        id={inputId}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="gatewayllm-input text-sm"
        aria-label="Backend model"
      />

      {supportsDiscovery && credentialId ? (
        <div className="space-y-2">
          <div className="flex items-center justify-between gap-2">
            <input
              type="search"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search available models..."
              className="gatewayllm-input text-xs"
            />
            {discoveryLoading && <span className="text-xs text-zinc-500">Loading...</span>}
          </div>
          {discoveryError && (
            <p className="text-xs text-red-400">Discovery failed: {discoveryError}. You can still type a model id above.</p>
          )}
          {filteredDiscovered.length > 0 ? (
            <ul className="max-h-48 overflow-y-auto rounded-md border border-zinc-800 bg-zinc-950/40 divide-y divide-zinc-800/60">
              {filteredDiscovered.map((m) => (
                <li key={m.id}>
                  <button
                    type="button"
                    onClick={() => onChange(m.id)}
                    className={`flex w-full items-center justify-between gap-3 px-3 py-2 text-left text-xs transition-colors hover:bg-zinc-800/50 ${
                      value === m.id ? 'bg-emerald-500/10 text-emerald-200' : 'text-zinc-300'
                    }`}
                  >
                    <span className="truncate font-mono">{m.id}</span>
                    <span className="flex flex-wrap gap-1">
                      {(m.capabilities ?? []).slice(0, 3).map(capabilityBadge)}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          ) : !discoveryLoading && !discoveryError ? (
            <p className="text-xs text-zinc-500">No models match.</p>
          ) : null}
        </div>
      ) : (
        <div className="space-y-2 rounded-md border border-zinc-800/80 bg-zinc-950/30 p-3">
          <p className="text-xs text-zinc-400">
            {supportsDiscovery
              ? 'Pick a provider credential to load available models.'
              : 'This provider does not expose a live model list. Enter the upstream model id manually.'}
            {docsUrl && (
              <>
                {' '}
                <a
                  href={docsUrl}
                  target="_blank"
                  rel="noreferrer"
                  className="text-emerald-400 hover:text-emerald-300 underline-offset-2 hover:underline"
                >
                  See {providerInfo?.label || provider} model ids
                </a>
                .
              </>
            )}
          </p>
          {filteredCatalog.length > 0 && (
            <div>
              <p className="mb-1 text-[11px] uppercase tracking-wide text-zinc-500">Common models</p>
              <div className="flex flex-wrap gap-1.5">
                {filteredCatalog.map((p) => (
                  <button
                    key={`${p.provider}/${p.model}`}
                    type="button"
                    onClick={() => onChange(p.model)}
                    className={`rounded-md border px-2 py-0.5 font-mono text-[11px] transition-colors ${
                      value === p.model
                        ? 'border-emerald-500 bg-emerald-500/15 text-emerald-200'
                        : 'border-zinc-700 bg-zinc-900 text-zinc-300 hover:border-emerald-500/40 hover:text-emerald-200'
                    }`}
                  >
                    {p.model}
                  </button>
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
