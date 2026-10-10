import { useState, type ReactNode } from 'react';
import { api } from '@/api/client';
import { CopyField } from '@/components/CopyField';
import { Button, ErrorNote, Field, Input, Select } from '@/components/ui';

/** What POST /aws/role/setup returns. */
export interface AwsRoleSetupResult {
  available: boolean;
  reason?: string;
  template_path?: string;
  setup?: {
    hub_principal: string;
    external_id: string;
    access: 'hub' | 'readonly';
    role_name: string;
    stack_name: string;
    permissions: string[];
    quick_create_url?: string;
    template_file: string;
    deploy_command: string;
  };
}

/** What POST /aws/role/check returns. */
export interface AwsRoleCheck {
  ok: boolean;
  role_arn?: string;
  account_id?: string;
  problem?: string;
  message: string;
  missing?: string[];
}

/**
 * AwsRoleSetup gives the Hub read-only access to an AWS account without keys: one button opens the AWS
 * console's quick-create page for a role that trusts only this Hub (or shows the template and command),
 * then the account ID is checked by assuming the role with its External ID.
 */
export function AwsRoleSetup({ uses, region, defaultAccess = 'hub', onReady, fallback }: {
  /** The connector type ("cloudwatch", "sqs", …) or "mcp": which read calls to check. */
  uses: string;
  region?: string;
  defaultAccess?: 'hub' | 'readonly';
  /** Called once the Hub can assume the role. */
  onReady: (roleArn: string, externalId: string) => void;
  /** What to suggest when the Hub has no AWS identity of its own. */
  fallback?: ReactNode;
}) {
  const [access, setAccess] = useState(defaultAccess);
  const [setup, setSetup] = useState<AwsRoleSetupResult>();
  const [account, setAccount] = useState('');
  const [result, setResult] = useState<AwsRoleCheck>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const s = setup?.setup;

  const start = async () => {
    setBusy(true);
    setError(undefined);
    setResult(undefined);
    try {
      const r = await api.post<AwsRoleSetupResult>('/aws/role/setup', { access, region: region?.trim() || undefined });
      setSetup(r);
      if (r.setup?.quick_create_url) window.open(r.setup.quick_create_url, '_blank', 'noopener');
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  };

  const check = async () => {
    if (!s) return;
    setBusy(true);
    setError(undefined);
    try {
      const v = account.trim();
      const r = await api.post<AwsRoleCheck>('/aws/role/check', {
        ...(v.startsWith('arn:') ? { role_arn: v } : { account_id: v }),
        external_id: s.external_id, uses, region: region?.trim() || undefined,
      });
      setResult(r);
      if (r.ok && r.role_arn) onReady(r.role_arn, s.external_id);
    } catch (e) {
      setError(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-3 rounded-lg border border-slate-200 p-3 dark:border-white/10">
      {!s && (
        <>
          <p className="text-sm text-slate-600 dark:text-slate-300">No keys to paste: AWS creates a read-only role that only this Hub can use.</p>
          <Field label="Permissions">
            <Select aria-label="Permissions" value={access} onChange={(e) => setAccess(e.target.value as 'hub' | 'readonly')}>
              <option value="hub">Only what the Hub reads (recommended)</option>
              <option value="readonly">Read-only access to everything (AWS ReadOnlyAccess)</option>
            </Select>
          </Field>
          <Button variant="secondary" disabled={busy} onClick={() => void start()}>{busy ? 'Preparing…' : 'Create a read-only role in AWS'}</Button>
          {setup && !setup.available && (
            <div role="status" className="space-y-1 text-sm text-amber-800 dark:text-amber-200">
              <p>{setup.reason}</p>
              {fallback}
            </div>
          )}
        </>
      )}
      {s && (
        <>
          {s.quick_create_url ? (
            <p className="text-sm text-slate-700 dark:text-slate-300">
              In the AWS console tab, tick the IAM acknowledgement and click <b>Create stack</b>.{' '}
              <a className="text-brand-600 hover:underline dark:text-brand-300" href={s.quick_create_url} target="_blank" rel="noreferrer">Open the AWS console</a>
            </p>
          ) : (
            <div className="space-y-2 text-sm text-slate-700 dark:text-slate-300">
              <p>
                <a className="text-brand-600 hover:underline dark:text-brand-300" href={setup?.template_path} download={s.template_file}>Download the template</a>{' '}
                and create the stack in the AWS console (CloudFormation → Create stack → Upload a template file), or run:
              </p>
              <CopyField label="Command" value={s.deploy_command} />
            </div>
          )}
          <p className="text-xs text-slate-500">It may only: {s.permissions.join(', ')}. External ID {s.external_id}.</p>
          <Field label="AWS account ID" hint="12 digits, top right in the AWS console. Or paste the stack's RoleArn output.">
            <div className="flex gap-2">
              <Input aria-label="AWS account ID" inputMode="numeric" placeholder="123456789012" value={account}
                onChange={(e) => setAccount(e.target.value)}
                onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); void check(); } }} />
              <Button disabled={busy || !account.trim()} onClick={() => void check()}>{busy ? 'Checking…' : 'Check access'}</Button>
            </div>
          </Field>
          {result && (
            <p role="status" className={`text-sm ${result.ok ? 'text-emerald-700 dark:text-emerald-300' : 'text-red-700 dark:text-red-300'}`}>
              {result.ok ? <>Access works. The Hub will use <code className="break-all">{result.role_arn}</code>.</> : capital(result.message)}
            </p>
          )}
        </>
      )}
      <ErrorNote error={error} />
    </div>
  );
}

function capital(s: string) {
  return s.charAt(0).toUpperCase() + s.slice(1) + (s.endsWith('.') ? '' : '.');
}
