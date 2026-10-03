import { ChevronDown, ChevronRight, ExternalLink, ShieldAlert, Wrench } from 'lucide-react';
import { useMemo, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/api/client';
import { useRepos } from '@/api/hooks';
import { Badge, Button, Card, Dialog, DialogFooter, Empty, ErrorNote, PageHeader, Spinner, Table, Td, Toggle, cx, type Tone } from '@/components/ui';
import { relTime } from '@/lib/format';
import { sentence } from '@/lib/labels';

export interface SecurityModule { name: string; description: string; static: boolean; reason?: string; default: boolean }
interface Fix { files: { path: string }[]; tests: { path: string }[]; risk: string }
export interface Finding {
  id: string; dimension: string; title: string; surface: string; file: string; line_start: number; line_end: number; repro: string[];
  evidence: string; severity: string; exploitability: string; priority: string; status: string; fix_status: string; fix?: Fix;
  fix_pr_url?: string; fix_error?: string;
}
export interface Scan {
  id: string; repo_id: string; repo: string; status: string; commit_sha: string; modules: string[]; verdict: string;
  summary: { counts?: Record<string, number>; notes?: string[]; files_read?: number; cached?: string[]; tokens?: number };
  error?: string; started_by?: string; created_at: string; finished_at?: string; findings?: Finding[];
}
interface Plan { modules: string[]; files: Record<string, string[]>; files_read: number; estimated_tokens: number; cached?: string[] }

const PRIORITY_TONE: Record<string, Tone> = { P0: 'red', P1: 'amber', P2: 'blue', P3: 'gray' };
const running = (s?: Scan) => s && (s.status === 'queued' || s.status === 'running');
const fixable = (f: Finding) => f.status !== 'rejected' && f.fix_status !== 'queued' && f.fix_status !== 'pr_opened';

function Verdict({ scan }: { scan?: Scan }) {
  if (!scan) return <span className="text-slate-400">Not scanned</span>;
  if (running(scan)) return <Badge tone="blue">{scan.status === 'queued' ? 'Queued' : 'Scanning…'}</Badge>;
  if (scan.status !== 'done') return <Badge tone="amber">{sentence(scan.status)}</Badge>;
  return <Badge tone={scan.verdict === 'GO' ? 'green' : 'red'}>{scan.verdict}</Badge>;
}

function Counts({ scan }: { scan?: Scan }) {
  const c = scan?.summary?.counts ?? {};
  return (
    <span className="flex flex-wrap gap-1">
      {(['P0', 'P1', 'P2', 'P3'] as const).filter((p) => c[p]).map((p) => <Badge key={p} tone={PRIORITY_TONE[p]}>{`${p}: ${c[p]}`}</Badge>)}
      {scan?.status === 'done' && !['P0', 'P1', 'P2', 'P3'].some((p) => c[p]) && <span className="text-xs text-slate-500">No findings</span>}
    </span>
  );
}

/** Attribution: the modules come from kryptonite. */
function About() {
  return (
    <p className="mb-4 text-xs text-slate-500">
      Scans use the attack modules of <a className="underline" href="https://github.com/levitasOrg/kryptonite" target="_blank" rel="noreferrer">kryptonite</a> (MIT),
      applied to your code statically: the Hub reads the tracked branch and never sends requests to your running systems. Every finding is
      checked a second time, ranked P0 to P3, and nothing changes in a repository until you select findings and click Fix, which opens a
      pull request for review.
    </p>
  );
}

export default function Security() {
  const { repoId } = useParams();
  return repoId ? <RepoSecurity repoId={repoId} /> : <Overview />;
}

function Overview() {
  const repos = useRepos();
  const scans = useQuery({ queryKey: ['security'], queryFn: () => api.get<{ items: Scan[] }>('/security'), refetchInterval: (q) => (q.state.data?.items.some(running) ? 4000 : false) });
  const byRepo = new Map((scans.data?.items ?? []).map((s) => [s.repo_id, s]));
  return (
    <>
      <PageHeader title="Security" description="Find weaknesses in your repositories, then fix the ones you choose." />
      <About />
      {(repos.isLoading || scans.isLoading) && <Spinner />}
      <ErrorNote error={repos.error ?? scans.error} />
      {repos.data?.length === 0 && <Empty icon={ShieldAlert} title="No repositories yet">Track a repository under Repositories to scan it.</Empty>}
      {!!repos.data?.length && (
        <Card>
          <Table head={['Repository', 'Verdict', 'Open findings', 'Last scan', '']}>
            {repos.data.map((r) => {
              const s = byRepo.get(r.id);
              return (
                <tr key={r.id}>
                  <Td><Link to={`/security/${r.id}`} className="font-medium text-brand-600">{r.full_name}</Link></Td>
                  <Td><Verdict scan={s} /></Td>
                  <Td><Counts scan={s} /></Td>
                  <Td>{s ? relTime(s.created_at) : <span className="text-slate-400">Never</span>}</Td>
                  <Td><Link to={`/security/${r.id}`} className="text-sm text-brand-600">{s ? 'Open' : 'Scan'}</Link></Td>
                </tr>
              );
            })}
          </Table>
        </Card>
      )}
    </>
  );
}

/** NewScan is kryptonite's scenario gate: pick modules, see what will be read and the estimate, then start. */
function NewScan({ repoId, onStarted }: { repoId: string; onStarted: () => void }) {
  const [open, setOpen] = useState(false);
  const modules = useQuery({ queryKey: ['security-modules'], queryFn: () => api.get<{ items: SecurityModule[] }>('/security/modules'), enabled: open });
  const [picked, setPicked] = useState<string[] | null>(null);
  const chosen = picked ?? (modules.data?.items.filter((m) => m.default).map((m) => m.name) ?? []);
  const plan = useMutation({ mutationFn: () => api.post<Plan>(`/security/repos/${repoId}/plan`, { modules: chosen }) });
  const start = useMutation({ mutationFn: () => api.post<{ scan_id: string }>(`/security/repos/${repoId}/scans`, { modules: chosen }), onSuccess: () => { setOpen(false); plan.reset(); onStarted(); } });
  const toggle = (name: string) => { plan.reset(); setPicked(chosen.includes(name) ? chosen.filter((n) => n !== name) : [...chosen, name]); };
  return (
    <>
      <Button onClick={() => setOpen(true)}><ShieldAlert className="h-4 w-4" aria-hidden />New scan</Button>
      <Dialog open={open} onOpenChange={(o) => { setOpen(o); if (!o) plan.reset(); }} title="New security scan" description="Choose what to attack. The Hub shows what it will read and roughly what it costs before anything runs.">
        {modules.isLoading && <Spinner />}
        <fieldset className="max-h-72 space-y-1.5 overflow-y-auto pr-1">
          <legend className="sr-only">Modules</legend>
          {modules.data?.items.map((m) => (
            <label key={m.name} className={cx('flex gap-2.5 rounded-lg border p-2 text-sm', m.static ? 'cursor-pointer border-slate-200 dark:border-white/10' : 'border-dashed border-slate-200 opacity-60 dark:border-white/10')}>
              <input type="checkbox" className="mt-0.5 accent-brand-600" disabled={!m.static} checked={chosen.includes(m.name)} onChange={() => toggle(m.name)} aria-label={m.name} />
              <span>
                <span className="font-medium">{sentence(m.name)}</span>
                <span className="block text-xs text-slate-500">{m.static ? m.description : `Needs a running app: ${m.reason}`}</span>
              </span>
            </label>
          ))}
        </fieldset>
        {plan.data && (
          <div className="mt-3 rounded-lg bg-slate-50 p-3 text-xs dark:bg-white/[0.04]" role="status">
            <p className="font-medium text-slate-700 dark:text-slate-200">
              Reads {plan.data.files_read} files · about {Math.round(plan.data.estimated_tokens / 1000)}k tokens on the security route
              {plan.data.cached?.length ? ` · ${plan.data.cached.length} module(s) reuse an earlier result (unchanged code)` : ''}
            </p>
            <ul className="mt-1 space-y-0.5 text-slate-600 dark:text-slate-400">
              {Object.entries(plan.data.files).map(([m, fs]) => <li key={m}><b className="font-medium">{sentence(m)}</b>: {fs.length ? `${fs.length} file(s)` : 'nothing to read'}</li>)}
            </ul>
          </div>
        )}
        <ErrorNote error={plan.error ?? start.error} />
        <DialogFooter>
          <Button variant="ghost" onClick={() => setOpen(false)}>Cancel</Button>
          {!plan.data ? (
            <Button variant="secondary" disabled={chosen.length === 0 || plan.isPending} onClick={() => plan.mutate()}>{plan.isPending ? 'Reading the code…' : 'Show estimate'}</Button>
          ) : (
            <Button disabled={start.isPending} onClick={() => start.mutate()}>Start scan</Button>
          )}
        </DialogFooter>
      </Dialog>
    </>
  );
}

function FindingRow({ f, selected, onSelect }: { f: Finding; selected: boolean; onSelect: (on: boolean) => void }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <tr className={cx(f.status === 'rejected' && 'opacity-60')}>
        <Td><input type="checkbox" className="accent-brand-600" aria-label={`Select ${f.title}`} disabled={!fixable(f)} checked={selected} onChange={(e) => onSelect(e.target.checked)} /></Td>
        <Td><Badge tone={PRIORITY_TONE[f.priority] ?? 'gray'}>{f.priority}</Badge></Td>
        <Td>
          <button type="button" className="flex items-start gap-1 text-left font-medium" aria-expanded={open} onClick={() => setOpen(!open)}>
            {open ? <ChevronDown className="mt-0.5 h-4 w-4 shrink-0" aria-hidden /> : <ChevronRight className="mt-0.5 h-4 w-4 shrink-0" aria-hidden />}{f.title}
          </button>
          <p className="ml-5 text-xs text-slate-500">{sentence(f.dimension)} · {sentence(f.severity)} severity · {sentence(f.exploitability)} to exploit</p>
        </Td>
        <Td><code className="break-all text-xs">{f.file}:{f.line_start}</code></Td>
        <Td><Badge tone={f.status === 'confirmed' ? 'red' : f.status === 'plausible' ? 'amber' : 'gray'}>{sentence(f.status)}</Badge></Td>
        <Td>
          {f.fix_status === 'pr_opened' && f.fix_pr_url ? (
            <a href={f.fix_pr_url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-sm text-brand-600">Pull request<ExternalLink className="h-3 w-3" aria-hidden /></a>
          ) : f.fix_status === 'queued' || f.fix_status === 'proposed' ? (
            <Badge tone="blue">Fixing…</Badge>
          ) : f.fix_status === 'failed' ? (
            <span title={f.fix_error}><Badge tone="amber">Not fixed</Badge></span>
          ) : null}
        </Td>
      </tr>
      {open && (
        <tr>
          <td colSpan={6} className="bg-slate-50 px-4 py-3 text-sm dark:bg-white/[0.03]">
            {f.surface && <p><b className="font-medium">Where:</b> {f.surface} ({f.file} lines {f.line_start}–{f.line_end})</p>}
            {f.repro?.length > 0 && (
              <div className="mt-2"><b className="font-medium">How it is triggered</b>
                <ol className="ml-5 list-decimal">{f.repro.map((r, i) => <li key={i}>{r}</li>)}</ol>
              </div>
            )}
            <div className="mt-2"><b className="font-medium">Evidence</b><pre className="mt-1 whitespace-pre-wrap break-words rounded bg-white p-2 text-xs dark:bg-slate-900">{f.evidence}</pre></div>
            {f.fix_error && <p className="mt-2 text-amber-700 dark:text-amber-300"><b className="font-medium">Why it was not fixed:</b> {f.fix_error}</p>}
            {f.fix?.risk && <p className="mt-2"><b className="font-medium">Fix risk:</b> {f.fix.risk}</p>}
          </td>
        </tr>
      )}
    </>
  );
}

function RepoSecurity({ repoId }: { repoId: string }) {
  const qc = useQueryClient();
  const repos = useRepos();
  const repo = repos.data?.find((r) => r.id === repoId);
  const scans = useQuery({ queryKey: ['security', repoId], queryFn: () => api.get<{ items: Scan[] }>(`/security/repos/${repoId}/scans`) });
  const [scanId, setScanId] = useState<string>();
  const current = scanId ?? scans.data?.items[0]?.id;
  const scan = useQuery({
    queryKey: ['security-scan', current], enabled: !!current,
    queryFn: () => api.get<Scan>(`/security/scans/${current}`),
    refetchInterval: (q) => (running(q.state.data) || q.state.data?.findings?.some((f) => f.fix_status === 'queued' || f.fix_status === 'proposed') ? 3000 : false),
  });
  const [showRejected, setShowRejected] = useState(false);
  const [selected, setSelected] = useState<string[]>([]);
  const [confirm, setConfirm] = useState(false);
  const findings = useMemo(() => (scan.data?.findings ?? []).filter((f) => showRejected || f.status !== 'rejected'), [scan.data, showRejected]);
  const fix = useMutation({
    mutationFn: () => api.post<{ job_id: string; queued: number }>(`/security/repos/${repoId}/fix`, { finding_ids: selected }),
    onSuccess: () => { setConfirm(false); setSelected([]); void qc.invalidateQueries({ queryKey: ['security-scan', current] }); },
  });
  const s = scan.data;
  const rejected = (s?.findings ?? []).filter((f) => f.status === 'rejected').length;
  return (
    <>
      <Link to="/security" className="mb-3 inline-block text-sm text-slate-500 hover:text-slate-800 dark:hover:text-slate-200">← All repositories</Link>
      <PageHeader title={repo?.full_name ?? 'Security'} description="Findings from the latest scan. Select the ones to fix; the Hub opens one pull request with a minimal fix and two tests for each."
        actions={<NewScan repoId={repoId} onStarted={() => { setScanId(undefined); void qc.invalidateQueries({ queryKey: ['security'] }); }} />} />
      <About />
      {(scans.isLoading || scan.isLoading) && <Spinner />}
      <ErrorNote error={scans.error ?? scan.error} />
      {scans.data?.items.length === 0 && <Empty icon={ShieldAlert} title="Not scanned yet">Start a scan to see what kryptonite's modules find in this repository.</Empty>}
      {s && (
        <Card className="mb-4">
          <div className="flex flex-wrap items-center gap-3 text-sm">
            <Verdict scan={s} />
            <Counts scan={s} />
            <span className="text-slate-500">
              {sentence(s.status)} {relTime(s.finished_at ?? s.created_at)}{s.commit_sha ? ` at ${s.commit_sha.slice(0, 8)}` : ''}{s.started_by ? ` · started by ${s.started_by}` : ''}
              {s.summary?.tokens ? ` · ${Math.round(s.summary.tokens / 1000)}k tokens` : ''}{s.summary?.cached?.length ? ` · ${s.summary.cached.length} module(s) reused` : ''}
            </span>
            {(scans.data?.items.length ?? 0) > 1 && (
              <select aria-label="Scan" className="ml-auto rounded-md border border-slate-200 bg-transparent px-2 py-1 text-xs dark:border-white/10" value={current} onChange={(e) => setScanId(e.target.value)}>
                {scans.data?.items.map((x) => <option key={x.id} value={x.id}>{new Date(x.created_at).toLocaleString()} · {x.verdict || sentence(x.status)}</option>)}
              </select>
            )}
          </div>
          <p className="mt-1 text-xs text-slate-500">Modules: {s.modules.map(sentence).join(', ')}</p>
          {s.error && <p className="mt-2 text-sm text-red-700 dark:text-red-400">{s.error}</p>}
          {s.summary?.notes?.map((n) => <p key={n} className="mt-1 text-xs text-slate-500">{n}</p>)}
        </Card>
      )}
      {s?.status === 'done' && (
        <Card
          title={`Findings (${findings.length})`}
          actions={
            <div className="flex items-center gap-3">
              {rejected > 0 && <Toggle label={`Show ${rejected} rejected`} checked={showRejected} onChange={setShowRejected} />}
              <Button disabled={selected.length === 0} onClick={() => setConfirm(true)}><Wrench className="h-4 w-4" aria-hidden />Fix selected{selected.length ? ` (${selected.length})` : ''}</Button>
            </div>
          }
        >
          {findings.length === 0 ? (
            <p className="text-sm text-slate-500">No open findings in the modules scanned.</p>
          ) : (
            <Table head={['', 'Priority', 'Finding', 'Where', 'Verified', 'Fix']}>
              {findings.map((f) => <FindingRow key={f.id} f={f} selected={selected.includes(f.id)} onSelect={(on) => setSelected(on ? [...selected, f.id] : selected.filter((x) => x !== f.id))} />)}
            </Table>
          )}
        </Card>
      )}
      <Dialog open={confirm} onOpenChange={setConfirm} title={`Fix ${selected.length} finding${selected.length === 1 ? '' : 's'}?`}
        description={`The Hub designs a minimal fix and two tests for each, then opens one pull request on ${repo?.full_name ?? 'the repository'}. Nothing is merged: review it and let CI run the tests.`}>
        <ul className="max-h-48 list-disc space-y-0.5 overflow-y-auto pl-5 text-sm">
          {(s?.findings ?? []).filter((f) => selected.includes(f.id)).map((f) => <li key={f.id}>{f.priority} · {f.title}</li>)}
        </ul>
        <ErrorNote error={fix.error} />
        <DialogFooter>
          <Button variant="ghost" onClick={() => setConfirm(false)}>Cancel</Button>
          <Button disabled={fix.isPending} onClick={() => fix.mutate()}>{fix.isPending ? 'Starting…' : 'Open a pull request'}</Button>
        </DialogFooter>
      </Dialog>
    </>
  );
}
