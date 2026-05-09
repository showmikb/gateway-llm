'use client';

import { createContext, useContext, useEffect, useState, type ReactNode } from 'react';
import type { AuthSession, User } from '@/lib/types';

interface AuthContextValue {
  session: AuthSession | null;
  user: User | null;
  isMasterKey: boolean;
  isAdmin: boolean;
  isTeamAdmin: boolean;
  isViewer: boolean;
  logout: () => void;
}

const AuthContext = createContext<AuthContextValue>({
  session: null,
  user: null,
  isMasterKey: false,
  isAdmin: false,
  isTeamAdmin: false,
  isViewer: false,
  logout: () => {},
});

export function AuthProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<AuthSession | null>(null);

  useEffect(() => {
    const masterKey = localStorage.getItem('gatewayllm_master_key');
    const token = localStorage.getItem('gatewayllm_token');
    const userJson = localStorage.getItem('gatewayllm_user');

    if (token && userJson) {
      try {
        const user = JSON.parse(userJson) as User;
        setSession({ type: 'user_session', token, user });
      } catch {
        setSession(null);
      }
    } else if (masterKey) {
      setSession({ type: 'master_key', token: masterKey });
    }
  }, []);

  function logout() {
    localStorage.removeItem('gatewayllm_master_key');
    localStorage.removeItem('gatewayllm_token');
    localStorage.removeItem('gatewayllm_user');
    setSession(null);
    window.location.href = '/login';
  }

  const user = session?.user ?? null;
  const isMasterKey = session?.type === 'master_key';
  const isAdmin =
    isMasterKey ||
    (user?.role !== undefined &&
      ['super_admin', 'org_admin', 'admin'].includes(user.role));
  const isTeamAdmin =
    isAdmin ||
    (user?.role !== undefined &&
      ['team_admin'].includes(user.role));
  const isViewer = !isMasterKey && user?.role === 'viewer';

  return (
    <AuthContext.Provider value={{ session, user, isMasterKey, isAdmin, isTeamAdmin, isViewer, logout }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  return useContext(AuthContext);
}
