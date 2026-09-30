import { useState } from 'react';
import { api } from '@/api/client';
import { keys, useConnectors, useInvalidating, useMe, useRepos } from '@/api/hooks';
import { atLeast, type PushMode, type Repo } from '@/api/types';
import { Badge, Button, Card, Dialog, Empty, ErrorNote, Field, Input, PageHeader, Select, Spinner, Table, Td, Toggle } from '@/components/ui';
import { shortSha } from '@/lib/format';

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
      <Dialog open={open} onOpenChange={setOpen} title="Track a repository" description="Docs are generated for pushes to its tracked branch (default branch unless you change it).">
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
                <option key={c.id} value={c.id}>{c.name} ({c.type})</option>
              ))}
            </Select>
          </Field>
          {git.length === 0 && <p className="text-sm text-amber-700">Add a GitHub or GitLab connector first.</p>}
          <Field label="Repository" hint="owner/name, or group/sub/project on GitLab">
            <Input required value={fullName} onChange={(e) => setFullName(e.target.value)} placeholder="acme/checkout" />
          </Field>
          <ErrorNote error={add.error} />
          <Button type="submit" disabled={add.isPending || git.length === 0}>Track</Button>
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
        <Button type="submit" disabled={save.isPending}>Save</Button>
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
  const imp = useInvalidating((id: string) => api.post(`/repos/${id}/import`, {}));
  const admin = atLeast(me.data?.role, 'admin');
  return (
    <>
      <PageHeader title="Repositories" description="Repositories whose pushes keep docs and the index up to date." actions={admin && <AddRepo onAdded={(name, webhook) => setAdded({ name, webhook })} />} />
      {added && (
        <p role="status" className="mb-4 text-sm text-slate-600">
          Tracking {added.name}.{added.webhook && <> Webhook: {added.webhook}.</>}
        </p>
      )}
      {repos.isLoading && <Spinner />}
      <ErrorNote error={repos.error ?? imp.error} />
      {repos.data?.length === 0 && <Empty title="No repositories tracked">{admin ? 'Track one to start generating docs.' : 'Ask an admin to track repositories.'}</Empty>}
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
                <Td>{r.push.mode.replaceAll('_', ' ')}</Td>
                <Td className="font-mono text-xs">{shortSha(r.last_processed_sha)}</Td>
                <Td>{atLeast(me.data?.role, 'editor') && <Button size="sm" variant="secondary" onClick={() => setDry(r)}>Dry run</Button>}</Td>
                <Td>
                  {admin && (
                    <div className="flex gap-1">
                      <Button size="sm" variant="secondary" onClick={() => setEditing(r)}>Settings</Button>
                      <Button size="sm" variant="ghost" onClick={() => imp.mutate(r.id)} disabled={imp.isPending}>Import docs</Button>
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
