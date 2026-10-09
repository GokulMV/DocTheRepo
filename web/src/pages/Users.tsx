import { KeyRound, Link2, LogIn, Pencil, Trash2, UserPlus } from 'lucide-react';
import { useMutation } from '@tanstack/react-query';
import { useEffect, useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { api } from '@/api/client';
import { keys, useAuthConfig, useInvalidating, useMe, useRepos, useUsers } from '@/api/hooks';
import { atLeast, type Role, type User } from '@/api/types';
import { CopyField } from '@/components/CopyField';
import { Badge, Button, Card, Dialog, DialogFooter, ErrorNote, Field, Input, PageHeader, Select, Spinner, Table, Td, cx } from '@/components/ui';
import { relTime } from '@/lib/format';
import { sentence } from '@/lib/labels';

const ROLES: { role: Role; text: string }[] = [
  { role: 'viewer', text: 'Reads docs and asks questions in the repositories they can see.' },
  { role: 'editor', text: 'Also edits docs, triages issues and runs syncs.' },
  { role: 'admin', text: 'Also manages connectors, models, spend and users. Sees every repository.' },
  { role: 'owner', text: 'Everything, including other owners and sign-in settings.' },
];

interface InviteLink { path: string; expires_at: string; emailed?: boolean; email_error?: string }

/** LinkResult says whether the one-time password link was emailed, and shows it to pass on. */
function LinkResult({ who, email, link }: { who: string; email: string; link: InviteLink }) {
  const failed = link.emailed === false;
  return (
    <div className={cx('space-y-2 rounded-xl border p-4 text-sm', failed
      ? 'border-amber-300 bg-amber-50/70 dark:border-amber-500/30 dark:bg-amber-500/10'
      : 'border-emerald-300 bg-emerald-50/70 dark:border-emerald-500/30 dark:bg-emerald-500/10')}>
      {link.emailed ? (
        <p className="font-medium" role="status">Emailed to {email}.</p>
      ) : failed ? (
        <>
          <p className="font-medium" role="alert">The email could not be sent. Send this link to {who} yourself.</p>
          <p className="wrap-break-word text-xs text-amber-800 dark:text-amber-200">{link.email_error}</p>
        </>
      ) : (
        <p className="font-medium">Send this link to {who}.</p>
      )}
      <CopyField value={window.location.origin + link.path} label="password link" />
      <p className="text-xs text-slate-600 dark:text-slate-400">
        They open it, choose a password, and are signed in. It works once and expires {new Date(link.expires_at).toLocaleDateString()}.{' '}
        {link.emailed === undefined && 'Email is not set up on this Hub, so share it the way you normally would (chat, email). '}
        Anyone with the link can set the password, so send it only to them.
      </p>
    </div>
  );
}

function AddUser({ onClose, canOwner, password, sso, mail }: { onClose: () => void; canOwner: boolean; password: boolean; sso: boolean; mail: boolean }) {
  const [email, setEmail] = useState('');
  const [name, setName] = useState('');
  const [role, setRole] = useState<Role>('viewer');
  const [invite, setInvite] = useState(password);
  const [done, setDone] = useState<{ user: User; invite?: InviteLink }>();
  const add = useInvalidating((b: object) => api.post<{ user: User; invite?: InviteLink }>('/users', b), keys.users);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setDone(await add.mutateAsync({ email, name, role, invite: password && invite }));
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title="Add a user" description="Choose what they can do. You can change it any time.">
      {done ? (
        <div className="space-y-3">
          <p className="text-sm"><b className="font-medium">{done.user.email}</b> is added as {done.user.role}.</p>
          {done.invite && <LinkResult who={done.user.name || done.user.email} email={done.user.email} link={done.invite} />}
          {sso && <p className="text-sm text-slate-600 dark:text-slate-400">They can also use “Sign in with single sign-on” with this email address; they get the role you chose.</p>}
          {!done.invite && !sso && <p className="text-sm text-amber-700 dark:text-amber-300">No sign-in method is on, so they cannot sign in yet. Turn on passwords or single sign-on under Sign-in &amp; SSO.</p>}
          <DialogFooter><Button onClick={onClose}>Done</Button></DialogFooter>
        </div>
      ) : (
        <form onSubmit={submit} className="space-y-3">
          <Field label="Email" htmlFor="new-email"><Input id="new-email" type="email" required value={email} onChange={(e) => setEmail(e.target.value)} placeholder="ann@example.com" autoFocus /></Field>
          <Field label="Name (optional)" htmlFor="new-name"><Input id="new-name" value={name} onChange={(e) => setName(e.target.value)} /></Field>
          <fieldset>
            <legend className="mb-1.5 text-sm font-medium">Role</legend>
            <div className="space-y-1.5">
              {ROLES.filter((r) => r.role !== 'owner' || canOwner).map((r) => (
                <label key={r.role} className={cx('flex cursor-pointer gap-2.5 rounded-lg border p-2.5 text-sm', role === r.role ? 'border-brand-400 bg-brand-50/50 dark:border-brand-400/50 dark:bg-brand-500/10' : 'border-slate-200 dark:border-white/10')}>
                  <input type="radio" name="role" className="mt-0.5 accent-brand-600" checked={role === r.role} onChange={() => setRole(r.role)} />
                  <span><span className="font-medium capitalize">{r.role}</span><span className="block text-xs text-slate-500">{r.text}</span></span>
                </label>
              ))}
            </div>
          </fieldset>
          {password ? (
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" className="accent-brand-600" checked={invite} onChange={(e) => setInvite(e.target.checked)} />
              {mail ? 'Email them a link to set a password' : 'Create a link for them to set a password'}
            </label>
          ) : sso ? (
            <p className="text-xs text-slate-500">They sign in with single sign-on using this email; the role applies on their first sign-in.</p>
          ) : null}
          <ErrorNote error={add.error} />
          <DialogFooter>
            <Button type="button" variant="secondary" onClick={onClose}>Cancel</Button>
            <Button type="submit" disabled={add.isPending}>{add.isPending ? 'Adding…' : 'Add user'}</Button>
          </DialogFooter>
        </form>
      )}
    </Dialog>
  );
}

function EditName({ user, onClose }: { user: User; onClose: () => void }) {
  const [name, setName] = useState(user.name);
  const save = useInvalidating(() => api.patch(`/users/${user.id}`, { name }), keys.users);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={`Edit ${user.email}`}>
      <form onSubmit={async (e) => { e.preventDefault(); await save.mutateAsync(undefined); onClose(); }}>
        <Field label="Name" htmlFor="edit-name"><Input id="edit-name" value={name} onChange={(e) => setName(e.target.value)} autoFocus /></Field>
        <p className="mt-2 text-xs text-slate-500">The email is how they sign in, so it cannot change; add a new user instead. People who use single sign-on get their name from it on each sign-in.</p>
        <ErrorNote error={save.error} />
        <DialogFooter>
          <Button type="button" variant="secondary" onClick={onClose}>Cancel</Button>
          <Button type="submit" disabled={save.isPending}>Save</Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
}

function ResetLink({ user, onClose, mail }: { user: User; onClose: () => void; mail: boolean }) {
  const make = useInvalidating(() => api.post<InviteLink>(`/users/${user.id}/invite`, {}), keys.users);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={user.has_password ? `New password for ${user.email}` : `Password link for ${user.email}`}>
      {make.data ? <LinkResult who={user.name || user.email} email={user.email} link={make.data} /> : (
        <p className="text-sm text-slate-600 dark:text-slate-400">
          {user.has_password
            ? 'This makes a one-time link to choose a new password. When they use it, the old password stops working and they are signed out everywhere.'
            : 'This makes a one-time link for them to choose a password.'}
          {mail && ` It is emailed to ${user.email}.`}
        </p>
      )}
      <ErrorNote error={make.error} />
      <DialogFooter>
        <Button variant="secondary" onClick={onClose}>{make.data ? 'Done' : 'Cancel'}</Button>
        {!make.data && <Button onClick={() => make.mutate(undefined)} disabled={make.isPending}>{mail ? 'Email link' : 'Create link'}</Button>}
      </DialogFooter>
    </Dialog>
  );
}

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
  const seesAll = atLeast(user.role, 'admin');
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={`Repository access: ${user.email}`} description="Direct grants replace the user's previous ones. Access through identity-provider groups is kept.">
      {seesAll && <p className="mb-2 rounded-lg bg-slate-100 p-2 text-xs text-slate-600 dark:bg-white/5 dark:text-slate-400">As {user.role}, they already see every repository.</p>}
      <div className="max-h-80 space-y-1 overflow-y-auto">
        {repos.data?.map((r) => (
          <label key={r.id} className="flex items-center gap-2 text-sm">
            <input type="checkbox" className="accent-brand-600" checked={ids.includes(r.id)} onChange={(e) => setIds((cur) => (e.target.checked ? [...cur, r.id] : cur.filter((x) => x !== r.id)))} />
            {r.full_name}
          </label>
        ))}
      </div>
      <ErrorNote error={loadError ?? save.error} />
      <DialogFooter>
        <Button variant="secondary" onClick={onClose}>Cancel</Button>
        <Button disabled={!loaded || save.isPending} onClick={async () => { await save.mutateAsync(undefined); onClose(); }}>Save</Button>
      </DialogFooter>
    </Dialog>
  );
}

/** SignInSummary says how people get in, with the way to change it. */
function SignInSummary({ sso, password, owner, mail }: { sso: boolean; password: boolean; owner: boolean; mail: boolean }) {
  const test = useMutation({ mutationFn: () => api.post<{ sent_to: string }>('/auth/email/test', {}) });
  const methods = [sso && 'single sign-on', password && 'email and password'].filter(Boolean).join(' or ');
  return (
    <div className="mb-4 flex flex-wrap items-center gap-3 rounded-xl border border-slate-200/80 bg-white/70 p-4 text-sm shadow-card dark:border-white/6 dark:bg-slate-900/40">
      <LogIn className="h-5 w-5 shrink-0 text-brand-500" aria-hidden />
      <div className="min-w-0 flex-1">
        <p className="font-medium">{methods ? `People sign in with ${methods}.` : 'No sign-in method is on.'}</p>
        <p className="text-xs text-slate-500">
          {password && (mail ? 'Add someone and the Hub emails them their password link. ' : 'Add someone and send them their password link (email is not set up, so you pass it on). ')}
          {sso && 'Anyone from an allowed domain can sign in with SSO and starts as a viewer; add them first to give another role. '}
          {!sso && 'Single sign-on (Google, Microsoft, Okta, Keycloak) is not set up.'}
        </p>
        {test.data && <p className="mt-1 text-xs text-emerald-700 dark:text-emerald-300" role="status">Test email sent to {test.data.sent_to}.</p>}
        <ErrorNote error={test.error} />
      </div>
      {owner && mail && <Button size="sm" variant="secondary" disabled={test.isPending} onClick={() => test.mutate()}>{test.isPending ? 'Sending…' : 'Send test email'}</Button>}
      {owner && <Link to="/sign-in" className="inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-sm font-medium text-brand-600 hover:bg-brand-50 dark:text-brand-300 dark:hover:bg-brand-500/10"><KeyRound className="h-4 w-4" aria-hidden />Sign-in &amp; SSO</Link>}
    </div>
  );
}

function signInStatus(u: User): { text: string; tone: 'gray' | 'amber' | 'green' | 'blue' } {
  if (u.sso && u.has_password) return { text: 'SSO and password', tone: 'green' };
  if (u.sso) return { text: 'SSO', tone: 'green' };
  if (u.has_password) return { text: 'Password', tone: 'green' };
  if (u.invite_pending) return { text: 'Link sent, not used yet', tone: 'amber' };
  return { text: 'Not signed in yet', tone: 'gray' };
}

export default function Users() {
  const me = useMe();
  const users = useUsers();
  const cfg = useAuthConfig();
  const [dialog, setDialog] = useState<{ kind: 'add' } | { kind: 'access' | 'edit' | 'reset'; user: User }>();
  const update = useInvalidating((v: { id: string; role?: Role; disabled?: boolean }) => api.patch(`/users/${v.id}`, { role: v.role, disabled: v.disabled }), keys.users);
  const remove = useInvalidating((id: string) => api.del(`/users/${id}`), keys.users);
  const isOwner = me.data?.role === 'owner';
  const password = !!cfg.data?.password;
  const sso = !!cfg.data?.sso;
  const mail = !!cfg.data?.email;
  const close = () => setDialog(undefined);
  return (
    <>
      <PageHeader
        title="People"
        description="Who can use the Hub, what they can do, and which repositories they see."
        actions={<Button onClick={() => setDialog({ kind: 'add' })}><UserPlus className="h-4 w-4" aria-hidden />Add user</Button>}
      />
      {cfg.data && <SignInSummary sso={sso} password={password} owner={isOwner} mail={mail} />}
      {users.isLoading && <Spinner />}
      <ErrorNote error={users.error ?? update.error ?? remove.error} />
      <Card>
        <Table head={['User', 'Role', 'Sign-in', 'Last sign-in', '']}>
          {users.data?.items.map((u) => {
            const self = u.id === me.data?.id;
            const ownerOnly = u.role === 'owner' && !isOwner; // admins cannot change owners
            const st = signInStatus(u);
            return (
              <tr key={u.id} className={cx(u.disabled && 'opacity-60')}>
                <Td>
                  <span className="font-medium">{u.name || u.email}</span>
                  {self && <span className="ml-1.5 text-xs text-slate-400">(you)</span>}
                  <p className="text-xs text-slate-500">{u.email}</p>
                  {u.disabled && <Badge tone="red">Disabled</Badge>}
                </Td>
                <Td>
                  <Select aria-label={`Role for ${u.email}`} value={u.role} disabled={self || ownerOnly} onChange={(e) => update.mutate({ id: u.id, role: e.target.value as Role })} className="w-28">
                    {ROLES.filter((r) => r.role !== 'owner' || isOwner || u.role === 'owner').map((r) => <option key={r.role} value={r.role}>{sentence(r.role)}</option>)}
                  </Select>
                </Td>
                <Td><Badge tone={st.tone}>{st.text}</Badge></Td>
                <Td>{u.last_login_at ? relTime(u.last_login_at) : <span className="text-slate-400">Never</span>}</Td>
                <Td>
                  <div className="flex flex-wrap justify-end gap-1">
                    <Button size="sm" variant="secondary" onClick={() => setDialog({ kind: 'access', user: u })}>Repositories</Button>
                    <Button size="sm" variant="ghost" aria-label={`Edit ${u.email}`} title="Edit name" disabled={ownerOnly} onClick={() => setDialog({ kind: 'edit', user: u })}><Pencil className="h-3.5 w-3.5" aria-hidden /></Button>
                    {password && !u.disabled && (
                      <Button size="sm" variant="ghost" aria-label={`Password link for ${u.email}`} title={u.has_password ? 'Reset password' : 'Password link'} disabled={ownerOnly} onClick={() => setDialog({ kind: 'reset', user: u })}>
                        <Link2 className="h-3.5 w-3.5" aria-hidden />
                      </Button>
                    )}
                    {!self && (
                      <>
                        <Button size="sm" variant="ghost" disabled={ownerOnly} onClick={() => update.mutate({ id: u.id, disabled: !u.disabled })}>{u.disabled ? 'Enable' : 'Disable'}</Button>
                        <Button size="sm" variant="ghost" aria-label={`Remove ${u.email}`} title="Remove" disabled={ownerOnly}
                          onClick={() => confirm(`Remove ${u.email}? Their questions, tokens and grants are deleted. To keep them, disable the user instead.`) && remove.mutate(u.id)}>
                          <Trash2 className="h-3.5 w-3.5 text-red-500" aria-hidden />
                        </Button>
                      </>
                    )}
                  </div>
                </Td>
              </tr>
            );
          })}
        </Table>
      </Card>
      {dialog?.kind === 'add' && <AddUser onClose={close} canOwner={isOwner} password={password} sso={sso} mail={mail} />}
      {dialog?.kind === 'access' && <RepoAccess user={dialog.user} onClose={close} />}
      {dialog?.kind === 'edit' && <EditName user={dialog.user} onClose={close} />}
      {dialog?.kind === 'reset' && <ResetLink user={dialog.user} onClose={close} mail={mail} />}
    </>
  );
}
