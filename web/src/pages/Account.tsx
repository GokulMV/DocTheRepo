import { useState } from 'react';
import { api } from '@/api/client';
import { keys, useInvalidating, useMe, useTokens } from '@/api/hooks';
import { Button, Card, ErrorNote, Field, Input, PageHeader, Table, Td } from '@/components/ui';
import { relTime } from '@/lib/format';

export default function Account() {
  const me = useMe();
  const tokens = useTokens();
  const [name, setName] = useState('');
  const [days, setDays] = useState(90);
  const [secret, setSecret] = useState<string>();
  const create = useInvalidating((b: object) => api.post<{ token: string }>('/tokens', b), keys.tokens);
  const revoke = useInvalidating((id: string) => api.del(`/tokens/${id}`), keys.tokens);
  return (
    <>
      <PageHeader title="Your account" description={`${me.data?.email} · ${me.data?.role}`} />
      <Card title="Personal access tokens" className="max-w-3xl">
        <p className="mb-3 text-sm text-slate-500">For the dth CLI and scripts: send <code className="font-mono">Authorization: Bearer &lt;token&gt;</code>. Tokens act with your role and repository access.</p>
        <form
          className="mb-4 flex flex-wrap items-end gap-2"
          onSubmit={async (e) => {
            e.preventDefault();
            const r = await create.mutateAsync({ name, expires_in_days: days });
            setSecret(r.token);
            setName('');
          }}
        >
          <Field label="Name"><Input required value={name} onChange={(e) => setName(e.target.value)} placeholder="laptop CLI" /></Field>
          <Field label="Expires in (days, 0 = never)"><Input type="number" min={0} max={3650} value={days} onChange={(e) => setDays(Number(e.target.value))} className="w-32" /></Field>
          <Button type="submit" disabled={create.isPending}>Create token</Button>
        </form>
        {secret && (
          <div className="mb-4 rounded-md border border-amber-300 bg-amber-50 p-3 text-sm dark:bg-amber-950">
            Copy it now — it will not be shown again:
            <code className="mt-1 block break-all font-mono text-xs">{secret}</code>
          </div>
        )}
        <ErrorNote error={create.error ?? revoke.error ?? tokens.error} />
        <Table head={['Name', 'Created', 'Last used', 'Expires', '']}>
          {tokens.data?.map((t) => (
            <tr key={t.id}>
              <Td>{t.name}</Td>
              <Td>{relTime(t.created_at)}</Td>
              <Td>{relTime(t.last_used_at)}</Td>
              <Td>{t.expires_at ? new Date(t.expires_at).toLocaleDateString() : 'never'}</Td>
              <Td><Button size="sm" variant="ghost" onClick={() => revoke.mutate(t.id)}>Revoke</Button></Td>
            </tr>
          ))}
        </Table>
      </Card>
    </>
  );
}
