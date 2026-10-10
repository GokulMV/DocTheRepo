import { ChevronDown } from 'lucide-react';
import { useState, type ReactNode } from 'react';
import { api } from '@/api/client';
import { keys, useConnectors, useInvalidating } from '@/api/hooks';
import { CopyField } from '@/components/CopyField';
import { SealedHint } from '@/components/Sealed';
import { Button, Dialog, DialogFooter, ErrorNote, Field, Input, Textarea } from '@/components/ui';
import { seal } from '@/lib/seal';
import { AtlassianSignIn } from './ConnectAtlassian';
import { knowledgeSpec, sourceSpec, type KnowledgeSpec, type SourceSpec } from './signalSources';

export function randomSecret() {
  const b = new Uint8Array(24);
  crypto.getRandomValues(b);
  return Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');
}

/**
 * How each alert tool proves a delivery is genuine, and what to do in the tool.
 * - link: the key travels inside the link (?token=…), so there is one thing to paste.
 * - theirs: the tool makes its own signing secret, which is pasted back here (Sentry, PagerDuty).
 * - header: the tool sends the key in a header of its own (Firehose access key, EventBridge connection).
 */
interface Guide {
  secret: 'link' | 'theirs' | 'header';
  steps: ReactNode[];
  /** For "header": what the tool calls the key field. */
  keyField?: string;
  /** For "theirs": what the tool calls its secret. */
  theirName?: string;
}

const b = (s: string) => <b>{s}</b>;

export const GUIDES: Record<string, Guide> = {
  sentry: { secret: 'theirs', theirName: 'Client Secret', steps: [
    <>In Sentry, open {b('Settings → Developer Settings → Custom Integrations')} and choose {b('Create New Integration → Internal Integration')}.</>,
    <>Paste the link above as the {b('Webhook URL')}, turn on {b('Alert Rule Action')}, tick {b('issue')} under Webhooks, and save.</>,
    <>Copy the integration’s {b('Client Secret')} and paste it below.</>,
  ] },
  pagerduty: { secret: 'theirs', theirName: 'signing secret', steps: [
    <>In PagerDuty, open {b('Integrations → Generic Webhooks (v3)')} and choose {b('New Webhook')}.</>,
    <>Paste the link above as the {b('Webhook URL')}, pick the account or a service, select the incident events, and add it.</>,
    <>PagerDuty shows a {b('signing secret')} once: paste it below.</>,
  ] },
  datadog: { secret: 'link', steps: [
    <>In Datadog, open {b('Integrations')}, find {b('Webhooks')} and add a new one named {b('dth')}.</>,
    <>Paste the link above as its {b('URL')} and save.</>,
    <>Add {b('@webhook-dth')} to the message of each monitor that should reach the Hub.</>,
  ] },
  grafana: { secret: 'link', steps: [
    <>In Grafana, open {b('Alerting → Contact points → Add contact point')} and choose the {b('Webhook')} integration.</>,
    <>Paste the link above as the {b('URL')} and save.</>,
    <>Under {b('Notification policies')}, send the alerts you want to this contact point.</>,
  ] },
  opsgenie: { secret: 'link', steps: [
    <>In Opsgenie, open {b('Settings → Integrations → Add integration')} and choose {b('Webhook')}.</>,
    <>Paste the link above as the {b('Webhook URL')}, turn on {b('Add Alert Description to Payload')}, and save.</>,
  ] },
  alertmanager: { secret: 'link', steps: [
    <>Add a receiver to your Alertmanager configuration with the link above as its URL:<pre className="mt-1 overflow-x-auto rounded-md bg-slate-100 p-2 font-mono text-[12px] dark:bg-white/6">{'receivers:\n  - name: dth\n    webhook_configs:\n      - url: <the link above>'}</pre></>,
    <>Route the alerts you want to the {b('dth')} receiver, then reload Alertmanager.</>,
  ] },
  generic: { secret: 'link', steps: [
    <>Make your tool send an HTTP {b('POST')} with a JSON body to the link above. Common fields (title, severity, service, id) are found automatically.</>,
  ] },
  wiz: { secret: 'link', steps: [
    <>In Wiz, open {b('Settings → Integrations')} and add a {b('Webhook')} integration with the link above as its URL.</>,
    <>Create an {b('Automation rule')} that sends issues to it.</>,
  ] },
  splunk: { secret: 'link', steps: [
    <>On a Splunk alert, add the {b('Webhook')} action and paste the link above as its URL.</>,
  ] },
  gcp: { secret: 'link', steps: [
    <>In Google Cloud, open {b('Monitoring → Alerting → Edit notification channels')} and add a {b('Webhook')} with the link above as its endpoint.</>,
    <>Add that channel to the alert policies you want.</>,
  ] },
  cloudwatch: { secret: 'header', keyField: 'API key value (key name X-DTH-Token)', steps: [
    <>In Amazon EventBridge, create an {b('API destination')}: endpoint = the link above, method {b('POST')}, and a connection with authorization type {b('API key')}, key name {b('X-DTH-Token')} and the key below as its value.</>,
    <>Create a rule for {b('CloudWatch Alarm State Change')} events with that API destination as its target.</>,
  ] },
  eventbridge: { secret: 'header', keyField: 'API key value (key name X-DTH-Token)', steps: [
    <>In Amazon EventBridge, create an {b('API destination')}: endpoint = the link above, method {b('POST')}, and a connection with authorization type {b('API key')}, key name {b('X-DTH-Token')} and the key below as its value.</>,
    <>Point the rules you want at that API destination.</>,
  ] },
  firehose: { secret: 'header', keyField: 'Access key', steps: [
    <>In Amazon Data Firehose, create a stream with destination {b('HTTP endpoint')}: endpoint URL = the link above, {b('Access key')} = the key below.</>,
    <>Send CloudWatch Logs to it with a subscription filter on each log group.</>,
  ] },
};

/** uniqueName picks a connector name not taken yet ("Sentry", then "Sentry 2"…). */
function useUniqueName() {
  const conns = useConnectors();
  return (base: string) => {
    const taken = new Set((conns.data ?? []).map((c) => c.name.toLowerCase()));
    if (!taken.has(base.toLowerCase())) return base;
    for (let i = 2; ; i++) if (!taken.has(`${base} ${i}`.toLowerCase())) return `${base} ${i}`;
  };
}

function More({ children, label = 'More options' }: { children: ReactNode; label?: string }) {
  return (
    <details className="group rounded-lg border border-slate-200 dark:border-white/10">
      <summary className="flex cursor-pointer list-none items-center justify-between px-3 py-2 text-sm text-slate-600 dark:text-slate-300">
        {label}<ChevronDown className="h-4 w-4 transition-transform group-open:rotate-180" aria-hidden />
      </summary>
      <div className="space-y-3 border-t border-slate-200 p-3 dark:border-white/10">{children}</div>
    </details>
  );
}

function ConfigFields({ spec, config, set, which }: { spec: SourceSpec; config: Record<string, string>; set: (c: Record<string, string>) => void; which: 'required' | 'optional' }) {
  return (
    <>
      {spec.config?.filter((c) => (which === 'required') === !!c.required).map((c) => (
        <Field key={c.key} label={c.key.replace(/_/g, ' ').replace(/^./, (x) => x.toUpperCase())} hint={c.hint || undefined}>
          <Input value={config[c.key] ?? ''} required={c.required} onChange={(e) => set({ ...config, [c.key]: e.target.value })} />
        </Field>
      ))}
    </>
  );
}

/** ConnectAlerts connects one alert tool with as little as possible to fill in, then says exactly what to do in the tool. */
export function ConnectAlerts({ type, onClose }: { type: string; onClose: () => void }) {
  const spec = sourceSpec(type)!;
  const guide = GUIDES[type];
  const webhook = spec.modes.includes('webhook') || spec.modes.includes('both');
  const pollOnly = !webhook;
  const [pull, setPull] = useState(pollOnly);
  const [config, setConfig] = useState<Record<string, string>>({});
  const [creds, setCreds] = useState('');
  const [noLLM, setNoLLM] = useState(type === 'wiz');
  const [made, setMade] = useState<{ id: string; path: string; secret: string }>();
  const [theirs, setTheirs] = useState('');
  const [finished, setFinished] = useState(false);
  const uniqueName = useUniqueName();
  const create = useInvalidating((body: object) => api.post<{ id: string; webhook_path: string }>('/connectors', body), keys.connectors);
  const saveSecret = useInvalidating((s: string) => seal(s, 'connector.webhook_secret').then((v) => api.patch(`/connectors/${made!.id}`, { webhook_secret: v })), keys.connectors);
  const canPull = spec.modes.some((m) => m === 'poll' || m === 'both');

  const submit = async () => {
    // A "theirs" secret is set after the tool shows it; until then a random one keeps deliveries out.
    const secret = webhook ? randomSecret() : '';
    const mode = pollOnly ? 'poll' : pull ? 'both' : 'webhook';
    const cfg: Record<string, string> = Object.fromEntries(Object.entries(config).filter(([, v]) => v.trim() !== ''));
    if (noLLM) cfg.never_send_to_llm = 'true';
    const r = await create.mutateAsync({
      type, name: uniqueName(spec.label), mode, config: cfg,
      credentials: pull && creds ? await seal(creds, 'connector.credentials') : undefined,
      webhook_secret: secret ? await seal(secret, 'connector.webhook_secret') : undefined,
    });
    if (webhook) setMade({ id: r.id, path: r.webhook_path, secret });
    else setFinished(true);
  };

  if (finished) {
    return (
      <Dialog open onOpenChange={(o) => !o && onClose()} title={`${spec.label} connected`} description="The Hub starts reading within a few minutes. Its health shows in the list once it has.">
        <DialogFooter><Button onClick={onClose}>Done</Button></DialogFooter>
      </Dialog>
    );
  }
  if (made) {
    const base = `${window.location.origin}${made.path}`;
    const link = guide?.secret === 'link' ? `${base}?token=${made.secret}` : base;
    return (
      <Dialog open onOpenChange={(o) => !o && onClose()} title={`Finish in ${spec.label}`} description="Two minutes in the other tool. This page shows the key only once.">
        <div className="space-y-3 text-sm">
          <CopyField label="Link" value={link} />
          {guide?.secret === 'header' && <><p className="text-xs text-slate-500">{guide.keyField}</p><CopyField label="Key" value={made.secret} /></>}
          <ol className="list-decimal space-y-2 pl-5 text-slate-700 dark:text-slate-300">
            {(guide?.steps ?? [<>{spec.help}</>]).map((s, i) => <li key={i}>{s}</li>)}
          </ol>
          {guide?.secret === 'theirs' && (
            <Field label={`${spec.label} ${guide.theirName}`} hint={<SealedHint />}>
              <div className="flex gap-2">
                <Input type="password" autoComplete="off" value={theirs} onChange={(e) => setTheirs(e.target.value)} />
                <Button disabled={!theirs.trim() || saveSecret.isPending} onClick={async () => { await saveSecret.mutateAsync(theirs.trim()); onClose(); }}>{saveSecret.isPending ? 'Saving…' : 'Save'}</Button>
              </div>
            </Field>
          )}
          <ErrorNote error={saveSecret.error} />
        </div>
        {guide?.secret !== 'theirs' && <DialogFooter><Button onClick={onClose}>Done</Button></DialogFooter>}
      </Dialog>
    );
  }
  const required = spec.config?.some((c) => c.required);
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={`Connect ${spec.label}`} description={pollOnly ? 'The Hub reads it on a schedule. Read-only: nothing changes in it.' : `The Hub makes a link for ${spec.label} to send alerts to. Nothing to fill in here.`}>
      <form className="space-y-3" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
        {pollOnly && required && <ConfigFields spec={spec} config={config} set={setConfig} which="required" />}
        {pollOnly && spec.credentials && (
          <Field label="Credentials" hint={`${spec.credentials}. Stored encrypted.`}>
            <Textarea rows={3} className="font-mono text-xs" value={creds} onChange={(e) => setCreds(e.target.value)} />
          </Field>
        )}
        <More>
          {webhook && canPull && (
            <label className="flex items-start gap-2 text-sm">
              <input type="checkbox" className="mt-1" checked={pull} onChange={(e) => setPull(e.target.checked)} />
              <span>Also pull from {spec.label}’s API<span className="block text-xs text-slate-500">For data the tool does not send by webhook.</span></span>
            </label>
          )}
          {pull && !pollOnly && required && <ConfigFields spec={spec} config={config} set={setConfig} which="required" />}
          {pull && <ConfigFields spec={spec} config={config} set={setConfig} which="optional" />}
          {pull && !pollOnly && spec.credentials && (
            <Field label="Credentials" hint={`${spec.credentials}. Stored encrypted.`}>
              <Textarea rows={3} className="font-mono text-xs" value={creds} onChange={(e) => setCreds(e.target.value)} />
            </Field>
          )}
          <label className="flex items-start gap-2 text-sm">
            <input type="checkbox" className="mt-1" checked={noLLM} onChange={(e) => setNoLLM(e.target.checked)} />
            <span>Never send this tool’s data to an AI model<span className="block text-xs text-slate-500">Issues are grouped and shown, but not explained.</span></span>
          </label>
        </More>
        <ErrorNote error={create.error} />
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>Cancel</Button>
          <Button type="submit" disabled={create.isPending}>{create.isPending ? 'Connecting…' : webhook ? 'Create link' : 'Connect'}</Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
}

const TOKEN_STEPS: Record<KnowledgeSpec['type'], ReactNode> = {
  confluence: <>Atlassian Cloud: create an API token at {b('id.atlassian.com → Security → API tokens')}, for the account e-mail above. Data Center: a personal access token from your profile (leave the e-mail empty).</>,
  jira: <>Atlassian Cloud: create an API token at {b('id.atlassian.com → Security → API tokens')}, for the account e-mail above. Data Center: a personal access token from your profile (leave the e-mail empty).</>,
  notion: <>In Notion: {b('Settings → Connections → Develop or manage integrations → New integration')} (read content only), copy its token, then share the pages to sync with it from each page’s {b('••• → Connections')}.</>,
};

/**
 * ConnectDocs connects Confluence, Jira or Notion. Confluence and Jira Cloud sign in with Atlassian (recommended);
 * the form below takes the site, what to sync, and an API token (Cloud) or personal access token (Data Center).
 */
export function ConnectDocs({ type, onClose, onDone }: { type: KnowledgeSpec['type']; onClose: () => void; onDone: (type: string) => void }) {
  const spec = knowledgeSpec(type)!;
  const atlassian = type === 'confluence' || type === 'jira';
  const [config, setConfig] = useState<Record<string, string>>({});
  const [token, setToken] = useState('');
  const uniqueName = useUniqueName();
  const create = useInvalidating((body: object) => api.post<{ id: string }>('/connectors', body), keys.connectors);
  const main = spec.config.filter((c) => c.required || c.key === 'email');
  const extra = spec.config.filter((c) => !c.required && c.key !== 'email');
  const field = (c: KnowledgeSpec['config'][number]) => (
    <Field key={c.key} label={c.label + (c.required ? '' : ' (optional)')} hint={c.hint}>
      <Input value={config[c.key] ?? ''} required={c.required} placeholder={c.placeholder} onChange={(e) => setConfig({ ...config, [c.key]: e.target.value })} />
    </Field>
  );
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()} title={`Connect ${spec.label}`} description={spec.help}>
      <form
        className="space-y-3"
        onSubmit={async (e) => {
          e.preventDefault();
          const cfg = Object.fromEntries(Object.entries(config).filter(([, v]) => v.trim() !== ''));
          await create.mutateAsync({ type, name: uniqueName(spec.label), mode: 'poll', config: cfg, credentials: await seal(token, 'connector.credentials') });
          onDone(type);
        }}
      >
        {atlassian && (
          <>
            <AtlassianSignIn type={type} keys={config[type === 'jira' ? 'projects' : 'spaces']} site={config.base_url} />
            <p className="pt-1 text-sm font-medium">Or use an API token (Cloud) or a personal access token (Data Center)</p>
          </>
        )}
        {main.map(field)}
        <Field label={spec.token?.label ?? 'API token'} hint={TOKEN_STEPS[type]}>
          <Input type="password" required value={token} onChange={(e) => setToken(e.target.value)} autoComplete="off" />
        </Field>
        {extra.length > 0 && <More>{extra.map(field)}</More>}
        <ErrorNote error={create.error} />
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>Cancel</Button>
          <Button type="submit" disabled={create.isPending}>{create.isPending ? 'Connecting…' : 'Connect'}</Button>
        </DialogFooter>
      </form>
    </Dialog>
  );
}
