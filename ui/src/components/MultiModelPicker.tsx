'use client';

import { useEffect, useMemo, useState } from 'react';

import { discoverProviderModels } from '@/lib/api';
import type { DiscoveredModel, ModelPricing, ProviderInfo } from '@/lib/types';

import { Badge } from './ui';

interface MultiModelPickerProps {
  /** Provider id (e.g. "openai") for hint and discovery resolution. */
  provider: string;
  /** Provider catalog metadata; required for hints when discovery is unavailable. */
  providerInfo?: ProviderInfo;
  /** Selected stored credential id; needed to call discovery. */
  credentialId?: string;
  /** Pricing catalog rows the parent already loaded; used as a static suggestion fallback. */
  pricingCatalog?: ModelPricing[];
  /** Currently checked model ids. */
  value: Set<string>;
  /** Called whenever the user toggles a model on/off. */
  onChange: (next: Set<string>) => void;
  /** Optional pre-warmed discovery results (avoids loading flash when the parent already fetched). */
  preloadedDiscovery?: DiscoveredModel[];
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
 * MultiModelPicker is the multi-select sibling of ModelPicker used by the
 * create-virtual-model wizard. It surfaces discovered models (when the
 * provider supports it) plus catalog-known "common models" as checkboxes
 * so users can pick any combination of backend models for a single
 * credential in one shot.
 */
export function MultiModelPicker({
  provider,
  providerInfo,
  credentialId,
  pricingCatalog,
  value,
  onChange,
  preloadedDiscovery,
}: MultiModelPickerProps) {
  const supportsDiscovery = !!providerInfo?.supports_discovery;
  const [discovered, setDiscovered] = useState<DiscoveredModel[]>(preloadedDiscovery ?? []);
  const [discoveryLoading, setDiscoveryLoading] = useState(false);
  const [discoveryError, setDiscoveryError] = useState<string | null>(null);
  const [search, setSearch] = useState('');
  const [manualEntry, setManualEntry] = useState('');

  useEffect(() => {
    if (!supportsDiscovery || !credentialId) {
      setDiscovered([]);
      setDiscoveryError(null);
      return;
    }
    if (preloadedDiscovery && preloadedDiscovery.length > 0) {
      setDiscovered(preloadedDiscovery);
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
  }, [supportsDiscovery, credentialId, preloadedDiscovery]);

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
    if (!search) return catalogForProvider.slice(0, 24);
    const q = search.toLowerCase();
    return catalogForProvider.filter((p) => p.model.toLowerCase().includes(q)).slice(0, 24);
  }, [catalogForProvider, search]);

  function toggle(modelId: string) {
    const next = new Set(value);
    if (next.has(modelId)) next.delete(modelId);
    else next.add(modelId);
    onChange(next);
  }

  function addManual() {
    const id = manualEntry.trim();
    if (!id) return;
    const next = new Set(value);
    next.add(id);
    onChange(next);
    setManualEntry('');
  }

  function selectAllVisible() {
    const next = new Set(value);
    if (filteredDiscovered.length > 0) {
      for (const m of filteredDiscovered) next.add(m.id);
    } else {
      for (const p of filteredCatalog) next.add(p.model);
    }
    onChange(next);
  }

  function clearAll() {
    onChange(new Set());
  }

  const docsUrl = providerInfo?.docs_url;
  const placeholder = providerInfo?.model_id_hint || 'Backend model id';

  const showDiscoveryList = supportsDiscovery && !!credentialId;

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <input
          type="search"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={showDiscoveryList ? 'Search available models...' : 'Search common models...'}
          className="gatewayllm-input flex-1 text-xs"
        />
        <button
          type="button"
          onClick={selectAllVisible}
          className="text-xs text-emerald-400 hover:text-emerald-300"
        >
          Select all
        </button>
        <button
          type="button"
          onClick={clearAll}
          className="text-xs text-zinc-400 hover:text-zinc-300"
        >
          Clear
        </button>
        {discoveryLoading && <span className="text-xs text-zinc-500">Loading...</span>}
      </div>

      {discoveryError && (
        <p className="text-xs text-red-400">
          Discovery failed: {discoveryError}. You can still type model ids manually below.
        </p>
      )}

      {showDiscoveryList ? (
        filteredDiscovered.length > 0 ? (
          <ul className="max-h-64 overflow-y-auto rounded-md border border-zinc-800 bg-zinc-950/40 divide-y divide-zinc-800/60">
            {filteredDiscovered.map((m) => {
              const checked = value.has(m.id);
              return (
                <li key={m.id}>
                  <label
                    className={`flex cursor-pointer items-center gap-3 px-3 py-2 text-xs transition-colors hover:bg-zinc-800/50 ${
                      checked ? 'bg-emerald-500/10 text-emerald-200' : 'text-zinc-300'
                    }`}
                  >
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={() => toggle(m.id)}
                      className="rounded border-zinc-600"
                    />
                    <span className="flex-1 truncate font-mono">{m.id}</span>
                    <span className="flex flex-wrap gap-1">
                      {(m.capabilities ?? []).slice(0, 3).map(capabilityBadge)}
                    </span>
                  </label>
                </li>
              );
            })}
          </ul>
        ) : !discoveryLoading && !discoveryError ? (
          <p className="text-xs text-zinc-500">No models match.</p>
        ) : null
      ) : (
        <div className="space-y-2 rounded-md border border-zinc-800/80 bg-zinc-950/30 p-3">
          <p className="text-xs text-zinc-400">
            {supportsDiscovery
              ? 'Pick a provider credential to load available models.'
              : 'This provider does not expose a live model list. Pick from common models below or add manually.'}
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
                {filteredCatalog.map((p) => {
                  const checked = value.has(p.model);
                  return (
                    <button
                      key={`${p.provider}/${p.model}`}
                      type="button"
                      onClick={() => toggle(p.model)}
                      className={`rounded-md border px-2 py-0.5 font-mono text-[11px] transition-colors ${
                        checked
                          ? 'border-emerald-500 bg-emerald-500/15 text-emerald-200'
                          : 'border-zinc-700 bg-zinc-900 text-zinc-300 hover:border-emerald-500/40 hover:text-emerald-200'
                      }`}
                    >
                      {p.model}
                    </button>
                  );
                })}
              </div>
            </div>
          )}
        </div>
      )}

      <div className="flex items-center gap-2">
        <input
          value={manualEntry}
          onChange={(e) => setManualEntry(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault();
              addManual();
            }
          }}
          placeholder={`Add by id: ${placeholder}`}
          className="gatewayllm-input flex-1 text-xs"
        />
        <button
          type="button"
          onClick={addManual}
          disabled={!manualEntry.trim()}
          className="gatewayllm-btn-secondary text-xs disabled:opacity-50"
        >
          Add
        </button>
      </div>

      {value.size > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {Array.from(value).map((id) => (
            <span
              key={id}
              className="inline-flex items-center gap-1 rounded-md border border-emerald-500/40 bg-emerald-500/10 px-2 py-0.5 font-mono text-[11px] text-emerald-200"
            >
              {id}
              <button
                type="button"
                onClick={() => toggle(id)}
                className="text-emerald-300 hover:text-emerald-100"
                aria-label={`Remove ${id}`}
              >
                ×
              </button>
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
