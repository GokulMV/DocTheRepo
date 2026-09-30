import { useEffect, useState } from 'react';
import { api } from '@/api/client';
import { keys, useInvalidating, useMe, useRepos, useUsers } from '@/api/hooks';
import type { Role, User } from '@/api/types';
import { Badge, Button, Card, Dialog, ErrorNote, PageHeader, Select, Spinner, Table, Td } from '@/components/ui';
import { relTime } from '@/lib/format';

function RepoAccess({ user, onClose }: { user: User; onClose: () => void }) {
  const repos = useRepos();
  const [ids, setIds] = useState<string[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [loadError, setLoadError] = useState<unknown>();
  useEffect(() => {
    api.get<{ items: { repo_id: string }[] }>(`/users/${user.id}/repo-access`).then(
      (r) => { setIds(r.items.map((g) => g.repo_id)); setLoaded(true); },
      setLoadError,
    );
  }, [user.id]);
  const save = useInvalidating(() => api.put(`/users/${user.id}/repo-access`, { repo_ids: ids, level: 'read' }));
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={`Repository access: ${user.email}`} description="Direct grants replace the user's previous ones. Access through identity-provider groups is kept.">
      <div className="max-h-80 space-y-1 overflow-y-auto">
        {repos.data?.map((r) => (
          <label key={r.id} className="flex items-center gap-2 text-sm">
            <input type="checkbox" className="accent-brand-600" checked={ids.includes(r.id)} onChange={(e) => setIds((cur) => (e.target.checked ? [...cur, r.id] : cur.filter((x) => x !== r.id)))} />
            {r.full_name}
          </label>
        ))}
      </div>
      <ErrorNote error={loadError ?? save.error} />
      <Button className="mt-3" disabled={!loaded || save.isPending} onClick={async () => { await save.mutateAsync(undefined); onClose(); }}>Save</Button>
    </Dialog>
  );
}

export default function Users() {
  const me = useMe();
  const users = useUsers();
  const [access, setAccess] = useState<User>();
  const update = useInvalidating((v: { id: string; role?: Role; disabled?: boolean }) => api.patch(`/users/${v.id}`, { role: v.role, disabled: v.disabled }), keys.users);
  return (
    <>
      <PageHeader title="Users & access" description="People sign in with your identity provider; new users start as viewers. Admins and owners can read every repository." />
      {users.isLoading && <Spinner />}
      <ErrorNote error={users.error ?? update.error} />
      <Card>
        <Table head={['User', 'Role', 'Sign-in', 'Last login', '']}>
          {users.data?.items.map((u) => (
            <tr key={u.id}>
              <Td><span className="font-medium">{u.name || u.email}</span><p className="text-xs text-slate-500">{u.email}</p>{u.disabled && <Badge tone="red">disabled</Badge>}</Td>
              <Td>
                <Select aria-label={`Role for ${u.email}`} value={u.role} disabled={u.id === me.data?.id} onChange={(e) => update.mutate({ id: u.id, role: e.target.value as Role })} className="w-28">
                  {['viewer', 'editor', 'admin', 'owner'].map((r) => <option key={r}>{r}</option>)}
                </Select>
              </Td>
              <Td>{u.sso ? 'SSO' : 'password'}</Td>
              <Td>{relTime(u.last_login_at)}</Td>
              <Td>
                <div className="flex gap-1">
                  <Button size="sm" variant="secondary" onClick={() => setAccess(u)}>Repositories</Button>
                  {u.id !== me.data?.id && <Button size="sm" variant="ghost" onClick={() => update.mutate({ id: u.id, disabled: !u.disabled })}>{u.disabled ? 'Enable' : 'Disable'}</Button>}
                </div>
              </Td>
            </tr>
          ))}
        </Table>
      </Card>
      {access && <RepoAccess user={access} onClose={() => setAccess(undefined)} />}
    </>
  );
}
