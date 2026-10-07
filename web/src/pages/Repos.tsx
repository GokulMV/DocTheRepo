import { FolderGit2 } from 'lucide-react';
import { useState } from 'react';
import { api } from '@/api/client';
import { keys, useConnectors, useInvalidating, useMe, useRepos } from '@/api/hooks';
import { atLeast, type PushMode, type Repo } from '@/api/types';
import { Badge, Button, Card, Dialog, DialogFooter, Empty, ErrorNote, Field, Input, PageHeader, Select, Spinner, Table, Td, Toggle } from '@/components/ui';
import { shortSha } from '@/lib/format';
import { capFirst, pushModeLabel } from '@/lib/labels';

const MODES: { id: PushMode; label: string }[] = [
  { id: 'pr_auto_merge', label: 'Pull request + auto-merge (default)' },
  { id: 'direct', label: 'Direct commit' },
  { id: 'pr_with_approver', label: 'Pull request + human approver' },
];

function AddRepo({ onAdded }: { onAdded: (fullName: string, webhook?: string) => void }) {
  const [open, setOpen] = useState(false);
  const conns = useConnectors(open);
  const [connectorId, setConnectorId] = useState('');
  const [fullName, setFullName] = useState('');
  const add = useInvalidating((b: object) => api.post<{ id: string; webhook?: string }>('/repos', b), keys.repos);
  const git = conns.data?.filter((c) => c.type === 'github' || c.type === 'gitlab') ?? [];
  return (
    <>
      <Button onClick={() => setOpen(true)}>Track repository</Button>
      <Dialog open={open} onOpenChange={setOpen} title="Track a repository" description="Its docs are written right away, then updated on every commit to the tracked branch (the default branch unless you change it). Files listed in a .dthignore file are left out.">
        <form
          className="space-y-3"
          onSubmit={async (e) => {
            e.preventDefault();
            const r = await add.mutateAsync({ connector_id: connectorId || git[0]?.id, full_name: fullName.trim() });
            setOpen(false);
            onAdded(fullName.trim(), r.webhook);
            setFullName('');
          }}
        >
          <Field label="Git connector">
            <Select value={connectorId} onChange={(e) => setConnectorId(e.target.value)}>
              {git.map((c) => (
                <option key={c.id} value={c.id}>{capFirst(c.name)} ({c.type === 'github' ? 'GitHub' : c.type === 'gitlab' ? 'GitLab' : c.type})</option>
              ))}
            </Select>
          </Field>
          {git.length === 0 && <p className="text-sm text-amber-700">Add a GitHub or GitLab connector first.</p>}
          <Field label="Repository" hint="owner/name, or group/sub/project on GitLab">
            <Input required value={fullName} onChange={(e) => setFullName(e.target.value)} placeholder="acme/checkout" />
          </Field>
          <ErrorNote error={add.error} />
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => setOpen(false)}>Cancel</Button>
            <Button type="submit" disabled={add.isPending || git.length === 0}>Track</Button>
          </DialogFooter>
        </form>
      </Dialog>
    </>
  );
}

function EditRepo({ repo, onClose }: { repo: Repo; onClose: () => void }) {
  const [tracked, setTracked] = useState(repo.tracked_branch ?? '');
  const [docsPath, setDocsPath] = useState(repo.docs_path);
  const [mode, setMode] = useState<PushMode>(repo.push.mode);
  const [approver, setApprover] = useState(repo.push.approver ?? '');
  const [service, setService] = useState(repo.service_name ?? '');
  const [conflict, setConflict] = useState(repo.push.conflict_strategy);
  const [enabled, setEnabled] = useState(repo.enabled);
  const save = useInvalidating((b: object) => api.patch(`/repos/${repo.id}`, b), keys.repos);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={repo.full_name}>
      <form
        className="space-y-3"
        onSubmit={async (e) => {
          e.preventDefault();
          await save.mutateAsync({ tracked_branch: tracked, docs_path: docsPath, push_mode: mode, approver, service_name: service,
            pr_conflict_strategy: conflict, enabled });
          onClose();
        }}
      >
        <Field label="Tracked branch" hint={`Empty = default branch (${repo.default_branch})`}>
          <Input value={tracked} onChange={(e) => setTracked(e.target.value)} />
        </Field>
        <Field label="Generated docs path" hint="Relative directory ending in /. Only files here are ever written.">
          <Input value={docsPath} onChange={(e) => setDocsPath(e.target.value)} />
        </Field>
        <Field label="How docs land">
          <Select value={mode} onChange={(e) => setMode(e.target.value as PushMode)}>
            {MODES.map((m) => <option key={m.id} value={m.id}>{m.label}</option>)}
          </Select>
        </Field>
        {mode === 'pr_with_approver' && (
          <Field label="Approver" hint="GitHub user or org/team, GitLab user or group. They approve with their own account.">
            <Input value={approver} onChange={(e) => setApprover(e.target.value)} />
          </Field>
        )}
        <Field label="When a docs PR conflicts">
          <Select value={conflict} onChange={(e) => setConflict(e.target.value)}>
            <option value="auto_rebase">Rebase, regenerate if that fails</option>
            <option value="requeue">Close and regenerate</option>
            <option value="leave_open">Leave for a human</option>
          </Select>
        </Field>
        <Field label="Service name" hint="Links this repo to errors and alerts from that service.">
          <Input value={service} onChange={(e) => setService(e.target.value)} />
        </Field>
        <Toggle label="Enabled" checked={enabled} onChange={setEnabled} />
        <ErrorNote error={save.error} />
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={() => onClose()}>Cancel</Button>
          <Button type="submit" disabled={save.isPending}>Save</Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
}

function DryRun({ repo, onClose }: { repo: Repo; onClose: () => void }) {
  const [res, setRes] = useState<{ status: string; message: string; result: Record<string, unknown> }>();
  const [err, setErr] = useState<unknown>();
  const [busy, setBusy] = useState(false);
  const run = async () => {
    setBusy(true);
    setErr(undefined);
    try {
      setRes(await api.post(`/repos/${repo.id}/dry-run`, {}));
    } catch (e) {
      setErr(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={`Dry run: ${repo.full_name}`} description="What the next push would document, and roughly what it costs. Nothing is written or charged.">
      <Button onClick={run} disabled={busy}>{busy ? 'Running…' : 'Run dry run'}</Button>
      <ErrorNote error={err} />
      {res && (
        <div className="mt-3 space-y-2 text-sm">
          <p><Badge>{res.status}</Badge> {res.message}</p>
          <pre className="max-h-80 overflow-auto rounded bg-slate-100 p-2 text-xs dark:bg-slate-800">{JSON.stringify(res.result, null, 2)}</pre>
        </div>
      )}
    </Dialog>
  );
}

export default function Repos() {
  const me = useMe();
  const repos = useRepos();
  const [editing, setEditing] = useState<Repo>();
  const [dry, setDry] = useState<Repo>();
  const [added, setAdded] = useState<{ name: string; webhook?: string }>();
  const [notice, setNotice] = useState<string>();
  const imp = useInvalidating(async (r: Repo) => {
    await api.post(`/repos/${r.id}/import`, {});
    setNotice(`Importing the existing Markdown docs of ${r.full_name}. They appear under Docs in a minute; progress is under Activity.`);
  });
  const gen = useInvalidating(async (r: Repo) => {
    await api.post(`/repos/${r.id}/generate-docs`, {});
    setNotice(`Writing docs for all of ${r.full_name}. Files appear under Docs as they finish; progress is under Activity.`);
  });
  const admin = atLeast(me.data?.role, 'admin');
  return (
    <>
      <PageHeader title="Repositories" description="Repositories whose pushes keep docs and the index up to date." actions={admin && <AddRepo onAdded={(name, webhook) => setAdded({ name, webhook })} />} />
      {added && (
        <div role="status" className="mb-4 rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm text-emerald-800 dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-300">
          Tracking {added.name}. Its docs are being written now and appear under Docs when they finish; after that they update when the code changes.
          {added.webhook && <span className="block text-xs opacity-80">Webhook: {added.webhook}</span>}
        </div>
      )}
      {repos.isLoading && <Spinner />}
      {notice && (
        <div role="status" className="mb-4 flex items-start gap-2 rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-2 text-sm text-emerald-800 dark:border-emerald-500/30 dark:bg-emerald-500/10 dark:text-emerald-300">
          <span className="flex-1">{notice}</span>
          <button type="button" className="text-xs underline" onClick={() => setNotice(undefined)}>Dismiss</button>
        </div>
      )}
      <ErrorNote error={repos.error ?? imp.error ?? gen.error} />
      {repos.data?.length === 0 && <Empty icon={FolderGit2} title="No repositories tracked">{admin ? 'Track one to start generating docs.' : 'Ask an admin to track repositories.'}</Empty>}
      {!!repos.data?.length && (
        <Card>
          <Table head={['Repository', 'Branch', 'Docs path', 'Landing', 'Processed', '', '']}>
            {repos.data.map((r) => (
              <tr key={r.id}>
                <Td>
                  <span className="font-medium">{r.full_name}</span> <Badge>{r.connector_type}</Badge>
                  {!r.enabled && <Badge tone="amber">disabled</Badge>}
                  {r.service_name && <p className="text-xs text-slate-500">service: {r.service_name}</p>}
                </Td>
                <Td>{r.tracked_branch || r.default_branch}</Td>
                <Td className="font-mono text-xs">{r.docs_path}</Td>
                <Td>{pushModeLabel(r.push.mode)}</Td>
                <Td className="font-mono text-xs">{r.last_processed_sha ? shortSha(r.last_processed_sha) : <span className="font-sans text-slate-400">not yet</span>}</Td>
                <Td>{atLeast(me.data?.role, 'editor') && <Button size="sm" variant="secondary" onClick={() => setDry(r)}>Dry run</Button>}</Td>
                <Td>
                  {admin && (
                    <div className="flex gap-1">
                      <Button size="sm" onClick={() => gen.mutate(r)} disabled={gen.isPending} title="Document every file in the repository now">Generate docs</Button>
                      <Button size="sm" variant="secondary" onClick={() => setEditing(r)}>Settings</Button>
                      <Button size="sm" variant="ghost" onClick={() => imp.mutate(r)} disabled={imp.isPending} title="Bring in Markdown docs that already exist in the repository">Import docs</Button>
                    </div>
                  )}
                </Td>
              </tr>
            ))}
          </Table>
        </Card>
      )}
      {editing && <EditRepo repo={editing} onClose={() => setEditing(undefined)} />}
      {dry && <DryRun repo={dry} onClose={() => setDry(undefined)} />}
    </>
  );
}
