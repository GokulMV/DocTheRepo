import { useState } from 'react';
import { api } from '@/api/client';
import { useActivity, useInvalidating, useJobs, useMe } from '@/api/hooks';
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

export default function Activity() {
  const [types, setTypes] = useState('');
  const [status, setStatus] = useState('');
  const [type, setType] = useState('');
  const [open, setOpen] = useState<Job>();
  const feed = useActivity(types);
  const jobs = useJobs({ status, type });
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
                  <Badge tone={a.kind === 'audit' ? 'blue' : statusTone(a.action.split(':')[1] ?? '')}>{a.action}</Badge>
                  {a.detail && <span className="ml-2 break-all text-slate-500">{a.detail}</span>}
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
