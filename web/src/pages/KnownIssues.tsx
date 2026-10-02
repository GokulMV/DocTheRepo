import { useState } from 'react';
import { Link } from 'react-router-dom';
import { api } from '@/api/client';
import { useInvalidating, useKnownIssues, useMe, useSuggestions } from '@/api/hooks';
import { atLeast, KNOWN_REASONS, type KnownIssue, type LinkedDoc, type Match, type Suggestion, type TextSuggestion } from '@/api/types';
import { MatchEditor, RuleTester, reasonLabel } from '@/components/signals';
import { Badge, Button, Card, Dialog, Empty, ErrorNote, Field, Input, PageHeader, Select, Spinner, Table, Td, Textarea, cx } from '@/components/ui';
import { num, relTime } from '@/lib/format';

const describe = (m: Match) =>
  [
    m.fingerprints?.length ? `${m.fingerprints.length} fingerprint${m.fingerprints.length === 1 ? '' : 's'}` : '',
    m.services?.length ? `service ${m.services.join(', ')}` : '',
    m.environments?.length ? `env ${m.environments.join(', ')}` : '',
    m.sources?.length ? `source ${m.sources.join(', ')}` : '',
    m.message_regex ? `message ~ /${m.message_regex}/` : '',
    m.max_severity ? `up to ${m.max_severity}` : '',
  ].filter(Boolean).join(' · ') || 'everything (invalid)';

// origin records where a saved proposal came from (pasted text, or a fetched Jira issue / Confluence page).
type Origin = { source: string; jira_key?: string; confluence_page_id?: string; ticket_url?: string; explanation?: string; source_text?: string };

function originOf(res: TextSuggestion, pasted: string): Origin {
  const l = res.link;
  if (!l) return { source: 'pasted', explanation: res.explanation, source_text: pasted };
  return { source: l.source, jira_key: l.jira_key, confluence_page_id: l.confluence_page_id, ticket_url: l.url, explanation: res.explanation, source_text: l.source_text };
}

function RuleDialog({ initial, origin, onDone }: { initial?: Partial<KnownIssue>; origin?: Origin; onDone: () => void }) {
  const [title, setTitle] = useState(initial?.title ?? '');
  const [reason, setReason] = useState(initial?.reason ?? 'known_bug');
  const [action, setAction] = useState(initial?.action ?? 'suppress');
  const [match, setMatch] = useState<Match>(initial?.match ?? {});
  const [description, setDescription] = useState(initial?.description ?? '');
  const save = useInvalidating(() => api.post<KnownIssue>('/known-issues', { title, reason, action, match, description, ...origin }), ['known-issues'], ['issues']);
  return (
    <Dialog open onOpenChange={(o) => !o && onDone()} title="New known-issue rule" description="Every field you fill must match. Test it before saving.">
      <form className="space-y-3" onSubmit={async (e) => { e.preventDefault(); await save.mutateAsync(undefined); onDone(); }}>
        <Field label="Title"><Input value={title} onChange={(e) => setTitle(e.target.value)} required /></Field>
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="Reason">
            <Select value={reason} onChange={(e) => setReason(e.target.value)}>{KNOWN_REASONS.map((r) => <option key={r} value={r}>{reasonLabel(r)}</option>)}</Select>
          </Field>
          <Field label="Action" hint="Label only keeps decoding and shows the label.">
            <Select value={action} onChange={(e) => setAction(e.target.value as 'suppress' | 'label_only')}>
              <option value="suppress">suppress</option>
              <option value="label_only">label only</option>
            </Select>
          </Field>
        </div>
        <MatchEditor value={match} onChange={setMatch} />
        <Field label="Notes"><Textarea rows={2} value={description} onChange={(e) => setDescription(e.target.value)} /></Field>
        <RuleTester match={match} />
        <ErrorNote error={save.error} />
        <Button type="submit" disabled={save.isPending}>Save rule</Button>
      </form>
    </Dialog>
  );
}

function Rules({ canEdit }: { canEdit: boolean }) {
  const rules = useKnownIssues();
  const [creating, setCreating] = useState(false);
  const toggle = useInvalidating((k: KnownIssue) => api.patch(`/known-issues/${k.id}`, { enabled: !k.enabled }), ['known-issues']);
  const del = useInvalidating((k: KnownIssue) => api.del(`/known-issues/${k.id}`), ['known-issues']);
  return (
    <Card title="Rules" actions={canEdit && <Button size="sm" onClick={() => setCreating(true)}>New rule</Button>}>
      {rules.isLoading && <Spinner />}
      <ErrorNote error={rules.error ?? toggle.error ?? del.error} />
      {rules.data?.length === 0 && <Empty title="No rules yet">Mark issues as known from the Inbox, paste a runbook under “From text”, or accept a suggestion.</Empty>}
      {!!rules.data?.length && (
        <Table head={['Rule', 'Matches', 'Hits', 'Status', '']}>
          {rules.data.map((k) => (
            <tr key={k.id}>
              <Td className="max-w-sm">
                <span className="font-medium">{k.title}</span>
                <p className="text-xs text-slate-500">
                  {reasonLabel(k.reason)} · {k.source}{k.label_managed && ' (label)'}{k.action === 'label_only' && ' · label only'}
                  {k.ticket_url && <> · <a className="text-brand-700 hover:underline dark:text-brand-100" href={k.ticket_url} target="_blank" rel="noreferrer">{k.jira_key || 'page'}</a></>}
                  {k.upstream_status && <> · {k.upstream_status}</>}
                </p>
                {k.upstream_note && <p className="mt-1"><Badge tone="amber">verify</Badge> <span className="text-xs text-amber-800 dark:text-amber-200">{k.upstream_note}</span></p>}
              </Td>
              <Td className="max-w-md text-xs">{describe(k.match)}</Td>
              <Td>{num(k.hits)}{k.last_hit_at && <p className="text-xs text-slate-500">{relTime(k.last_hit_at)}</p>}</Td>
              <Td>
                {k.enabled ? <Badge tone="green">active</Badge> : <Badge>disabled</Badge>}
                {k.expires_at && <p className="text-xs text-slate-500">until {new Date(k.expires_at).toLocaleDateString()}</p>}
              </Td>
              <Td>
                {canEdit && (
                  <div className="flex gap-1">
                    <Button size="sm" variant="ghost" onClick={() => toggle.mutate(k)}>{k.enabled ? 'Disable' : 'Enable'}</Button>
                    <Button size="sm" variant="ghost" onClick={() => confirm(`Delete “${k.title}”? Matching events show up again.`) && del.mutate(k)}>Delete</Button>
                  </div>
                )}
              </Td>
            </tr>
          ))}
        </Table>
      )}
      {creating && <RuleDialog onDone={() => setCreating(false)} />}
    </Card>
  );
}

function SuggestionCard({ s }: { s: Suggestion }) {
  const [title, setTitle] = useState('');
  const [reason, setReason] = useState('expected_noise');
  const decide = useInvalidating((accept: boolean) => api.post(`/known-issues/suggestions/${s.id}/${accept ? 'accept' : 'reject'}`, accept ? { title: title || undefined, reason } : {}),
    ['suggestions', 'pending'], ['known-issues'], ['issues']);
  return (
    <li className="space-y-2 py-3">
      <p className="whitespace-pre-line text-sm">{s.rationale}</p>
      <p className="text-xs text-slate-500">Proposed rule: {describe(s.proposed_match)} · {s.issue_ids.length} issue{s.issue_ids.length === 1 ? '' : 's'}:{' '}
        {s.issue_ids.slice(0, 5).map((id) => <Link key={id} className="mr-1 text-brand-700 hover:underline dark:text-brand-100" to={`/inbox/${id}`}>{id.slice(0, 8)}</Link>)}
      </p>
      <RuleTester match={s.proposed_match} />
      <div className="flex flex-wrap items-end gap-2">
        <Field label="Title"><Input value={title} onChange={(e) => setTitle(e.target.value)} placeholder="Accepted suggestion" /></Field>
        <Field label="Reason">
          <Select value={reason} onChange={(e) => setReason(e.target.value)}>{KNOWN_REASONS.map((r) => <option key={r} value={r}>{reasonLabel(r)}</option>)}</Select>
        </Field>
        <Button onClick={() => decide.mutate(true)} disabled={decide.isPending}>Accept and enable</Button>
        <Button variant="secondary" onClick={() => decide.mutate(false)} disabled={decide.isPending}>Reject</Button>
      </div>
      <ErrorNote error={decide.error} />
    </li>
  );
}

function Suggestions() {
  const sugg = useSuggestions('pending');
  return (
    <Card title="Suggestions" actions={<span className="text-xs text-slate-500">Issues decoded as noise, frequent, unacknowledged for 3+ days</span>}>
      {sugg.isLoading && <Spinner />}
      <ErrorNote error={sugg.error} />
      {sugg.data?.length === 0 && <Empty title="No suggestions">The Hub proposes rules once a day. Nothing is suppressed until someone accepts.</Empty>}
      <ul className="divide-y divide-slate-100 dark:divide-slate-800">{sugg.data?.map((s) => <SuggestionCard key={s.id} s={s} />)}</ul>
    </Card>
  );
}

function LinkedDocNote({ l }: { l: LinkedDoc }) {
  return (
    <p className="text-xs text-slate-500">
      From {l.source === 'jira' ? 'Jira' : 'Confluence'}: <a className="text-brand-700 hover:underline dark:text-brand-100" href={l.url} target="_blank" rel="noreferrer">{l.title}</a>
      {l.status && <> · {l.status}</>}
      {l.done && <> <Badge tone="amber">done upstream</Badge> consider “label only” so its errors stay visible</>}
    </p>
  );
}

function FromText() {
  const [text, setText] = useState('');
  const [url, setUrl] = useState('');
  const [res, setRes] = useState<TextSuggestion>();
  const [saving, setSaving] = useState(false);
  const run = useInvalidating((t: string) => api.post<TextSuggestion>('/known-issues/from-text', { text: t }));
  const fromLink = useInvalidating((u: string) => api.post<TextSuggestion>('/known-issues/from-link', { url: u }));
  return (
    <Card title="From text or a link" actions={<span className="text-xs text-slate-500">Paste an incident note, runbook, or ticket — or a Jira issue / Confluence page URL. Secrets are scrubbed before the model sees it.</span>}>
      <form className="mb-4 flex flex-wrap items-end gap-2" onSubmit={async (e) => { e.preventDefault(); setText(''); setRes(await fromLink.mutateAsync(url.trim())); }}>
        <div className="min-w-[18rem] flex-1">
          <Field label="Jira issue or Confluence page URL">
            <Input type="url" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://acme.atlassian.net/browse/ENG-412" />
          </Field>
        </div>
        <Button type="submit" disabled={fromLink.isPending || !url.trim()}>Explain link</Button>
      </form>
      <ErrorNote error={fromLink.error} />
      <form className="space-y-3" onSubmit={async (e) => { e.preventDefault(); setRes(await run.mutateAsync(text)); }}>
        <Textarea aria-label="Text" rows={6} value={text} onChange={(e) => setText(e.target.value)} placeholder={'e.g. "upstream request timeout" from checkout during the nightly batch is expected until ORD-412 ships.'} />
        <Button type="submit" disabled={run.isPending || !text.trim()}>Find matching issues</Button>
      </form>
      <ErrorNote error={run.error} />
      {res && (
        <div className="mt-4 space-y-3 text-sm">
          {res.link && <LinkedDocNote l={res.link} />}
          <p>{res.explanation} <Badge tone={res.confidence === 'high' ? 'green' : res.confidence === 'medium' ? 'amber' : 'gray'}>{res.confidence}</Badge></p>
          <p className="text-xs text-slate-500">Proposed rule: {describe(res.proposed_match)} · would match {res.matching_issues_last_7d} issue(s) in the last 7 days.</p>
          {res.candidates.length > 0 && (
            <ul className="space-y-1 text-xs">
              {res.candidates.map((c) => (
                <li key={c.issue_id} className={cx(res.proposed_match.fingerprints?.includes(c.fingerprint) && 'font-medium')}>
                  <Link className="hover:underline" to={`/inbox/${c.issue_id}`}>{c.title}</Link> <span className="text-slate-500">· {c.service} · {num(c.occurrences)} events</span>
                </li>
              ))}
            </ul>
          )}
          {(res.proposed_match.fingerprints?.length ?? 0) > 0 && <Button onClick={() => setSaving(true)}>Review and save as a rule</Button>}
        </div>
      )}
      {saving && res && (
        <RuleDialog
          initial={{ title: res.link?.title ?? res.candidates[0]?.title ?? '', reason: res.reason, match: res.proposed_match,
            action: res.link?.done ? 'label_only' : 'suppress', description: (res.link ? res.link.url : text).slice(0, 2000) }}
          origin={originOf(res, text)}
          onDone={() => setSaving(false)}
        />
      )}
    </Card>
  );
}

const TABS = ['Rules', 'Suggestions', 'From text'] as const; // "From text" also explains Jira/Confluence links

export default function KnownIssues() {
  const me = useMe();
  const canEdit = atLeast(me.data?.role, 'editor');
  const [tab, setTab] = useState<(typeof TABS)[number]>('Rules');
  return (
    <>
      <PageHeader title="Known issues" description="Rules that keep understood problems out of the Inbox and away from paid decoding. Matching events are still counted." />
      {canEdit && (
        <div className="mb-4 flex gap-1" role="tablist">
          {TABS.map((t) => (
            <button key={t} role="tab" aria-selected={tab === t} onClick={() => setTab(t)}
              className={cx('rounded px-3 py-1.5 text-sm', tab === t ? 'bg-brand-600 text-white' : 'text-slate-600 hover:bg-slate-100 dark:text-slate-300 dark:hover:bg-slate-800')}>
              {t}
            </button>
          ))}
        </div>
      )}
      {tab === 'Rules' && <Rules canEdit={canEdit} />}
      {canEdit && tab === 'Suggestions' && <Suggestions />}
      {canEdit && tab === 'From text' && <FromText />}
    </>
  );
}
