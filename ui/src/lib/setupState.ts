'use client';

import { useCallback, useEffect, useState } from 'react';
import { usePathname } from 'next/navigation';
import { getCredentials, getDeployments, getKeys } from '@/lib/api';

export interface SetupState {
  loading: boolean;
  hasProviderKeys: boolean;
  hasDeployments: boolean;
  hasAPIKeys: boolean;
}

export const SETUP_CHANGED_EVENT = 'gatewayllm:setup-changed';

export function notifySetupChanged(): void {
  if (typeof window !== 'undefined') {
    window.dispatchEvent(new Event(SETUP_CHANGED_EVENT));
  }
}

export function useSetupState(): SetupState {
  const pathname = usePathname();
  const [state, setState] = useState<SetupState>({
    loading: true,
    hasProviderKeys: false,
    hasDeployments: false,
    hasAPIKeys: false,
  });

  const refresh = useCallback(async (cancelledRef?: { cancelled: boolean }) => {
    try {
      const [creds, deps, keys] = await Promise.all([
        getCredentials().catch(() => []),
        getDeployments().catch(() => []),
        getKeys().catch(() => []),
      ]);
      if (cancelledRef?.cancelled) return;
      setState({
        loading: false,
        hasProviderKeys: creds.length > 0,
        hasDeployments: deps.length > 0,
        hasAPIKeys: keys.length > 0,
      });
    } catch {
      if (!cancelledRef?.cancelled) {
        setState((s) => ({ ...s, loading: false }));
      }
    }
  }, []);

  useEffect(() => {
    const cancelledRef = { cancelled: false };
    void refresh(cancelledRef);
    return () => {
      cancelledRef.cancelled = true;
    };
  }, [pathname, refresh]);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    const handler = () => {
      void refresh();
    };
    window.addEventListener(SETUP_CHANGED_EVENT, handler);
    return () => {
      window.removeEventListener(SETUP_CHANGED_EVENT, handler);
    };
  }, [refresh]);

  return state;
}
