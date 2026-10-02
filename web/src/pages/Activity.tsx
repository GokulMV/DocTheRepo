import { useState } from 'react';
import { api } from '@/api/client';
import { useActivity, useConnectors, useInvalidating, useJobs, useMe, useProviders, useRepos, useUsers } from '@/api/hooks';
import { atLeast, type Job } from '@/api/types';
import { Badge, Button, Card, Dialog, ErrorNote, PageHeader, Select, Spinner, Table, Td, statusTone } from '@/components/ui';
import { relTime } from '@/lib/format';

const STATUSES = ['', 'queued', 'processing', 'done', 'failed', 'aborted', 'spend_blocked', 'pending_approval', 'needs_human', 'dead'];
const TYPES = ['', 'code_push', 'import_docs', 'reindex', 'pr_review'];

function JobDetail({ job, onClose }: { job: Job; onClose: () => void }) {
  const me = useMe();
  const retry = useInvalidating((override: boolean) => api.post<{ job_id: string }>(`/jobs/${job.job_id}/retry`, { override_ceiling: override }), ['jobs']);
  const retryable = ['failed', 'dead', 'aborted', 'spend_blocked', 'needs_human'].includes(job.status);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={`${job.type} · ${job.status}`} description={`Job ${job.job_id} · correlation ${job.correlation_id}`}>
      {job.error && <p className="mb-2 text-sm text-red-700">{job.error}</p>}
      <pre className="max-h-96 overflow-auto rounded bg-slate-100 p-2 text-xs dark:bg-slate-800">{JSON.stringify(job.result ?? job.payload, null, 2)}</pre>
      {atLeast(me.data?.role, 'admin') && retryable && (
        <div className="mt-3 flex gap-2">
          {job.status === 'spend_blocked' ? (
            <Button variant="danger" onClick={() => confirm('Run once above the spend ceiling? This is audited.') && retry.mutate(true)}>Retry above ceiling</Button>
          ) : (
            <Button onClick={() => retry.mutate(false)}>Retry</Button>
          )}
        </div>
      )}
      {retry.data && <p className="mt-2 text-sm text-emerald-700">Queued job {retry.data.job_id}.</p>}
      <ErrorNote error={retry.error} />
    </Dialog>
  );
}

// Plain words for the feed; the raw action stays in the tooltip.
const ACTIONS: Record<string, string> = {
  'auth.login': 'signed in', 'auth.logout': 'signed out', 'repo.create': 'repository tracked', 'repo.update': 'repository settings changed',
  'repo.generate_docs': 'docs requested', 'route.set': 'model route saved', 'provider.create': 'provider added', 'provider.update': 'provider changed',
  'provider.delete': 'provider removed', 'connector.create': 'connector added', 'connector.update': 'connector changed', 'connector.delete': 'connector removed',
  'github.connect.app': 'GitHub App created', 'github.connect.installed': 'GitHub App installed', 'github.connect.existing': 'existing GitHub App connected',
  'token.create': 'access token created', 'token.revoke': 'access token revoked', 'spend.set': 'spend limits saved', 'settings.apply': 'settings file applied',
  'code_push:done': 'docs updated', 'code_push:aborted': 'nothing to document', 'code_push:failed': 'docs update failed', 'code_push:spend_blocked': 'blocked by spend limit',
  'import_docs:done': 'docs imported', 'docs_pr:opened': 'docs PR opened', 'docs_pr:merged': 'docs PR merged', 'docs_pr:closed': 'docs PR closed',
};

/** useNames resolves "repo:<id>"-style references in the feed to names. */
function useNames(): (text: string) => string {
  const me = useMe();
  const admin = atLeast(me.data?.role, 'admin');
  const repos = useRepos();
  const conns = useConnectors(admin);
  const provs = useProviders();
  const users = useUsers();
  const names = new Map<string, string>();
  for (const r of repos.data ?? []) names.set(`repo:${r.id}`, r.full_name);
  for (const c of conns.data ?? []) names.set(`connector:${c.id}`, `connector ${c.name}`);
  for (const p of provs.data?.items ?? []) names.set(`llm_provider:${p.id}`, `provider ${p.name}`);
  for (const u of users.data?.items ?? []) names.set(`user:${u.id}`, u.name || u.email);
  return (text) =>
    text
      .replace(/\b(repo|connector|llm_provider|user):[0-9a-f-]{36}\b/g, (m) => names.get(m) ?? m)
      .replace(/\bmodel_route:([a-z_]+)/g, 'route for $1');
}

export default function Activity() {
  const [types, setTypes] = useState('');
  const [status, setStatus] = useState('');
  const [type, setType] = useState('');
  const [open, setOpen] = useState<Job>();
  const feed = useActivity(types);
  const jobs = useJobs({ status, type });
  const name = useNames();
  return (
    <>
      <PageHeader title="Activity" description="Pushes processed, docs landed, PR lifecycle, and admin actions." />
      <div className="grid gap-6 xl:grid-cols-2">
        <Card
          title="Feed"
          actions={
            <Select aria-label="Feed type" value={types} onChange={(e) => setTypes(e.target.value)} className="w-36">
              <option value="">everything</option>
              <option value="job">jobs</option>
              <option value="pr">docs PRs</option>
              <option value="audit">admin actions</option>
            </Select>
          }
        >
          {feed.isLoading && <Spinner />}
          <ErrorNote error={feed.error} />
          <ul className="divide-y divide-slate-100 text-sm dark:divide-slate-800">
            {feed.data?.items.map((a) => (
              <li key={a.kind + a.ref_id + a.at} className="flex items-baseline justify-between gap-3 py-2">
                <span>
                  <span title={a.action}><Badge tone={a.kind === 'audit' ? 'blue' : statusTone(a.action.split(':')[1] ?? '')}>{ACTIONS[a.action] ?? a.action}</Badge></span>
                  {a.detail && <span title={a.detail} className="mt-1 line-clamp-2 break-all text-slate-500">{name(a.detail)}</span>}
                </span>
                <span className="shrink-0 text-xs text-slate-400">{relTime(a.at)}</span>
              </li>
            ))}
            {feed.data?.items.length === 0 && <li className="py-2 text-slate-500">Nothing yet.</li>}
          </ul>
        </Card>
        <Card
          title="Jobs"
          actions={
            <>
              <Select aria-label="Job type" value={type} onChange={(e) => setType(e.target.value)} className="w-36">
                {TYPES.map((t) => <option key={t} value={t}>{t || 'all types'}</option>)}
              </Select>
              <Select aria-label="Job status" value={status} onChange={(e) => setStatus(e.target.value)} className="w-40">
                {STATUSES.map((s) => <option key={s} value={s}>{s || 'all statuses'}</option>)}
              </Select>
            </>
          }
        >
          {jobs.isLoading && <Spinner />}
          <ErrorNote error={jobs.error} />
          <Table head={['Type', 'Status', 'Attempts', 'Updated']}>
            {jobs.data?.items.map((j) => (
              <tr key={j.job_id} className="cursor-pointer hover:bg-slate-50 dark:hover:bg-slate-800" onClick={() => setOpen(j)}>
                <Td>{j.type}</Td>
                <Td><Badge tone={statusTone(j.status)}>{j.status}</Badge></Td>
                <Td>{j.attempts}/{j.max_attempts}</Td>
                <Td>{relTime(j.updated_at)}</Td>
              </tr>
            ))}
          </Table>
        </Card>
      </div>
      {open && <JobDetail job={open} onClose={() => setOpen(undefined)} />}
    </>
  );
}
