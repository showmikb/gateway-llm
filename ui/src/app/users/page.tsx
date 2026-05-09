'use client';

import { useEffect, useState } from 'react';
import { getUsers, createUser, updateUser, deleteUser, getTeams, getOrganizations } from '@/lib/api';
import type { User, Team, Organization, UserRole } from '@/lib/types';
import { PageHeader, Alert, LoadingSkeleton, EmptyState, Badge } from '@/components/ui';
import { ContactTeamModal } from '@/components/ContactTeamModal';
import { useAuth } from '@/contexts/AuthContext';

const ROLES: UserRole[] = ['super_admin', 'org_admin', 'team_admin', 'member', 'viewer'];

export default function UsersPage() {
  const { isAdmin, isTeamAdmin } = useAuth();
  // Only org/super/team admins (or the master key) can create users. Everyone
  // else sees a polite "contact the team" overlay instead of a broken create
  // form, which matches the backend's AdminOnly middleware.
  const canManageUsers = isAdmin || isTeamAdmin;

  const [users, setUsers] = useState<User[]>([]);
  const [teams, setTeams] = useState<Team[]>([]);
  const [orgs, setOrgs] = useState<Organization[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [showContactModal, setShowContactModal] = useState(false);

  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [role, setRole] = useState<UserRole>('member');
  const [teamId, setTeamId] = useState('');

  const [editingId, setEditingId] = useState<string | null>(null);
  const [editRole, setEditRole] = useState<UserRole>('member');

  async function load() {
    try {
      const [u, t, o] = await Promise.all([getUsers(), getTeams(), getOrganizations()]);
      setUsers(u);
      setTeams(t);
      setOrgs(o);
      setError('');
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to load users');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => { load(); }, []);

  async function handleCreate() {
    try {
      await createUser({ email, password, role, team_id: teamId || null });
      setEmail('');
      setPassword('');
      setRole('member');
      setTeamId('');
      setShowCreate(false);
      load();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to create user');
    }
  }

  async function handleRoleChange(id: string) {
    try {
      await updateUser(id, { role: editRole });
      setEditingId(null);
      load();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to update user');
    }
  }

  async function handleDeactivate(id: string) {
    if (!confirm('Deactivate this user? All their API keys will also be deactivated.')) return;
    try {
      await deleteUser(id);
      load();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to deactivate user');
    }
  }

  async function handleReactivate(id: string) {
    try {
      await updateUser(id, { is_active: true });
      load();
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to reactivate user');
    }
  }

  if (loading) return <LoadingSkeleton rows={5} />;

  return (
    <div className="space-y-6">
      <PageHeader
        title="Users"
        description={
          canManageUsers
            ? 'Manage user accounts and roles.'
            : 'View team members. Adding or changing members requires an admin.'
        }
        action={
          <button
            onClick={() => {
              if (!canManageUsers) {
                setShowContactModal(true);
                return;
              }
              setShowCreate(!showCreate);
            }}
            className="gatewayllm-btn"
          >
            {showCreate && canManageUsers ? 'Cancel' : 'Create User'}
          </button>
        }
      />

      <ContactTeamModal
        open={showContactModal}
        onClose={() => setShowContactModal(false)}
      />

      {error && <Alert variant="error" onDismiss={() => setError('')}>{error}</Alert>}

      {showCreate && canManageUsers && (
        <div className="gatewayllm-card p-5 space-y-4">
          <h2 className="text-lg font-medium text-white">Create New User</h2>
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm text-zinc-400 mb-1">Email</label>
              <input
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                className="gatewayllm-input"
                placeholder="user@example.com"
              />
            </div>
            <div>
              <label className="block text-sm text-zinc-400 mb-1">Password</label>
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                className="gatewayllm-input"
                placeholder="Strong password"
              />
            </div>
            <div>
              <label className="block text-sm text-zinc-400 mb-1">Role</label>
              <select
                value={role}
                onChange={(e) => setRole(e.target.value as UserRole)}
                className="gatewayllm-input"
              >
                {ROLES.map((r) => (
                  <option key={r} value={r}>{r.replace('_', ' ')}</option>
                ))}
              </select>
            </div>
            <div>
              <label className="block text-sm text-zinc-400 mb-1">Team (optional)</label>
              <select
                value={teamId}
                onChange={(e) => setTeamId(e.target.value)}
                className="gatewayllm-input"
              >
                <option value="">No team</option>
                {teams.map((t) => (
                  <option key={t.id} value={t.id}>{t.name}</option>
                ))}
              </select>
            </div>
          </div>
          <button
            onClick={handleCreate}
            disabled={!email || !password}
            className="gatewayllm-btn disabled:opacity-50 disabled:cursor-not-allowed"
          >
            Create
          </button>
        </div>
      )}

      <div className="gatewayllm-table-wrap">
        <table className="gatewayllm-table">
          <thead>
            <tr>
              <th className="gatewayllm-th">Email</th>
              <th className="gatewayllm-th">Role</th>
              <th className="gatewayllm-th">Team</th>
              <th className="gatewayllm-th">Status</th>
              <th className="gatewayllm-th">Created</th>
              <th className="gatewayllm-th text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            {users.length === 0 ? (
              <tr>
                <td colSpan={6}>
                  <EmptyState
                    message="No users yet."
                    action={canManageUsers ? () => setShowCreate(true) : () => setShowContactModal(true)}
                    actionLabel={canManageUsers ? 'Create your first user' : 'Contact the team to add members'}
                  />
                </td>
              </tr>
            ) : (
              users.map((u) => {
                const team = teams.find((t) => t.id === u.team_id);
                const roleColor = ['super_admin', 'org_admin', 'admin'].includes(u.role)
                  ? 'bg-amber-900/40 text-amber-300 ring-1 ring-amber-700/50'
                  : u.role === 'team_admin'
                  ? 'bg-violet-900/40 text-violet-300 ring-1 ring-violet-700/50'
                  : u.role === 'viewer'
                  ? 'bg-sky-900/40 text-sky-300 ring-1 ring-sky-700/50'
                  : 'bg-zinc-800 text-zinc-300 ring-1 ring-zinc-700/50';
                return (
                  <tr key={u.id} className="border-b border-zinc-800/50 hover:bg-zinc-800/30">
                    <td className="px-4 py-3 text-white">{u.email}</td>
                    <td className="px-4 py-3">
                      {editingId === u.id ? (
                        <div className="flex items-center gap-2">
                          <select
                            value={editRole}
                            onChange={(e) => setEditRole(e.target.value as UserRole)}
                            className="rounded border border-zinc-700 bg-zinc-800 px-2 py-1 text-xs text-white"
                          >
                            {ROLES.map((r) => (
                              <option key={r} value={r}>{r.replace('_', ' ')}</option>
                            ))}
                          </select>
                          <button onClick={() => handleRoleChange(u.id)} className="text-xs text-emerald-400 hover:text-emerald-300">Save</button>
                          <button onClick={() => setEditingId(null)} className="text-xs text-zinc-400 hover:text-zinc-300">Cancel</button>
                        </div>
                      ) : (
                        <span className={`inline-block rounded-full px-2.5 py-0.5 text-xs font-medium ${roleColor}`}>
                          {u.role.replace('_', ' ')}
                        </span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-zinc-400">{team?.name ?? '—'}</td>
                    <td className="px-4 py-3">
                      <Badge variant={u.is_active ? 'success' : 'danger'}>
                        {u.is_active ? 'Active' : 'Inactive'}
                      </Badge>
                    </td>
                    <td className="px-4 py-3 text-zinc-400">{new Date(u.created_at).toLocaleDateString()}</td>
                    <td className="px-4 py-3 text-right space-x-2">
                      {canManageUsers ? (
                        <>
                          {editingId !== u.id && (
                            <button onClick={() => { setEditingId(u.id); setEditRole(u.role); }} className="text-xs text-emerald-400 hover:text-emerald-300">Edit Role</button>
                          )}
                          {u.is_active ? (
                            <button onClick={() => handleDeactivate(u.id)} className="text-xs text-red-400 hover:text-red-300">Deactivate</button>
                          ) : (
                            <button onClick={() => handleReactivate(u.id)} className="text-xs text-emerald-400 hover:text-emerald-300">Reactivate</button>
                          )}
                        </>
                      ) : (
                        <button
                          type="button"
                          onClick={() => setShowContactModal(true)}
                          className="text-xs text-zinc-400 hover:text-zinc-200"
                        >
                          Request change
                        </button>
                      )}
                    </td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}
