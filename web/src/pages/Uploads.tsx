import { FileText, Trash2, Upload } from 'lucide-react';
import { useRef, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { api, postForm } from '@/api/client';
import { useMe } from '@/api/hooks';
import { atLeast } from '@/api/types';
import { Markdown } from '@/components/Markdown';
import { Badge, Button, Card, Dialog, DialogFooter, Empty, ErrorNote, Field, Input, PageHeader, Spinner } from '@/components/ui';
import { relTime } from '@/lib/format';

export interface UploadDoc { id: string; collection: string; title: string; summary: string; uploaded_by?: string; updated_at: string; body?: string }
interface UploadResult { documents: { id: string; name: string; title: string }[]; refused?: { name: string; error: string }[]; embedded: number; notes?: string[] }

const ACCEPT = '.md,.markdown,.mdx,.txt,.text,.rst,.html,.htm';
const uploadsKey = ['uploads'];

/** UploadDocs adds Markdown, text or HTML files to the Library as shared team documents. */
export function UploadDocs({ collections }: { collections: string[] }) {
  const [open, setOpen] = useState(false);
  const [collection, setCollection] = useState('');
  const [files, setFiles] = useState<File[]>([]);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<unknown>();
  const [done, setDone] = useState<UploadResult>();
  const input = useRef<HTMLInputElement>(null);
  const qc = useQueryClient();
  const close = () => { setOpen(false); setFiles([]); setDone(undefined); setErr(undefined); };
  const submit = async () => {
    setBusy(true);
    setErr(undefined);
    try {
      const form = new FormData();
      form.set('collection', collection.trim());
      files.forEach((f) => form.append('files', f, f.name));
      setDone(await postForm<UploadResult>('/library/uploads', form));
      await qc.invalidateQueries({ queryKey: uploadsKey });
      await qc.invalidateQueries({ queryKey: ['shelves'] });
    } catch (e) {
      setErr(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <Button variant="secondary" onClick={() => setOpen(true)}><Upload className="h-4 w-4" aria-hidden />Upload documents</Button>
      <Dialog open={open} onOpenChange={(o) => (o ? setOpen(true) : close())} title="Upload documents" description="Runbooks, design notes, onboarding guides: anyone signed in can read them, and Ask cites them like any other doc.">
        {done ? (
          <div className="space-y-3 text-sm">
            {done.documents.length > 0 && <p role="status" className="font-medium text-emerald-700 dark:text-emerald-400">Added {done.documents.length} document{done.documents.length === 1 ? '' : 's'}: {done.documents.map((d) => d.title).join(', ')}.</p>}
            {done.refused?.map((r) => <p key={r.name} className="text-amber-700 dark:text-amber-300">{r.error}</p>)}
            {done.notes?.map((n) => <p key={n} className="text-slate-600 dark:text-slate-400">{n}</p>)}
            <DialogFooter><Button onClick={close}>Done</Button></DialogFooter>
          </div>
        ) : (
          <form className="space-y-3" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
            <Field label="Collection" hint="Groups related documents, e.g. Runbooks or Onboarding. Uploading a file with the same name to the same collection replaces it.">
              <Input value={collection} onChange={(e) => setCollection(e.target.value)} placeholder="Uploads" list="upload-collections" />
              <datalist id="upload-collections">{collections.map((c) => <option key={c} value={c} />)}</datalist>
            </Field>
            <Field label="Files" hint="Markdown, text or HTML, up to 5 MB each and 20 at a time. For PDF or Word, export as Markdown or HTML first.">
              <input ref={input} aria-label="Files" type="file" multiple accept={ACCEPT} className="block w-full text-sm" onChange={(e) => setFiles(Array.from(e.target.files ?? []))} />
            </Field>
            {files.length > 0 && <p className="text-xs text-slate-500">{files.length} file{files.length === 1 ? '' : 's'} selected: {files.map((f) => f.name).join(', ')}</p>}
            <ErrorNote error={err} />
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={close}>Cancel</Button>
              <Button type="submit" disabled={busy || files.length === 0}>{busy ? 'Uploading…' : 'Upload'}</Button>
            </DialogFooter>
          </form>
        )}
      </Dialog>
    </>
  );
}

/** UploadedList shows uploaded documents grouped by collection. */
export function UploadedList() {
  const list = useQuery({ queryKey: uploadsKey, queryFn: () => api.get<{ items: UploadDoc[] }>('/library/uploads') });
  const items = list.data?.items ?? [];
  if (list.isLoading) return <Spinner />;
  if (items.length === 0) return null;
  const groups = new Map<string, UploadDoc[]>();
  for (const u of items) groups.set(u.collection, [...(groups.get(u.collection) ?? []), u]);
  return (
    <Card title="Uploaded documents" className="mt-6">
      <ErrorNote error={list.error} />
      {[...groups.entries()].map(([collection, docs]) => (
        <div key={collection} className="mb-3">
          <h3 className="mb-1 text-xs font-semibold uppercase tracking-wide text-slate-500">{collection}</h3>
          <ul className="divide-y divide-slate-100 dark:divide-slate-800">
            {docs.map((d) => (
              <li key={d.id} className="py-2 text-sm">
                <Link to={`/library/uploads/${d.id}`} className="font-medium text-brand-600">{d.title}</Link>
                <span className="ml-2 text-xs text-slate-500">{relTime(d.updated_at)}{d.uploaded_by ? ` · ${d.uploaded_by}` : ''}</span>
                {d.summary && <p className="line-clamp-2 text-slate-600 dark:text-slate-400">{d.summary}</p>}
              </li>
            ))}
          </ul>
        </div>
      ))}
    </Card>
  );
}

/** UploadView reads one uploaded document. */
export default function UploadView() {
  const { id } = useParams();
  const me = useMe();
  const nav = useNavigate();
  const qc = useQueryClient();
  const doc = useQuery({ queryKey: [...uploadsKey, id], queryFn: () => api.get<UploadDoc>(`/library/uploads/${id}`) });
  const [confirm, setConfirm] = useState(false);
  const [err, setErr] = useState<unknown>();
  const remove = async () => {
    try {
      await api.del(`/library/uploads/${id}`);
      await qc.invalidateQueries({ queryKey: uploadsKey });
      nav('/library');
    } catch (e) {
      setErr(e);
    }
  };
  const d = doc.data;
  return (
    <>
      <Link to="/library" className="mb-3 inline-block text-sm text-slate-500 hover:text-slate-800 dark:hover:text-slate-200">← Library</Link>
      <PageHeader
        title={d?.title ?? 'Document'}
        description={d ? `Uploaded to ${d.collection} ${relTime(d.updated_at)}${d.uploaded_by ? ` by ${d.uploaded_by}` : ''}.` : undefined}
        actions={atLeast(me.data?.role, 'editor') && d && <Button variant="ghost" onClick={() => setConfirm(true)}><Trash2 className="h-4 w-4 text-red-500" aria-hidden />Remove</Button>}
      />
      {doc.isLoading && <Spinner />}
      <ErrorNote error={doc.error ?? err} />
      {d && (d.body ? <Card><Markdown>{d.body}</Markdown></Card> : <Empty icon={FileText} title="This document is empty" />)}
      {d && <Badge>Uploaded</Badge>}
      <Dialog open={confirm} onOpenChange={setConfirm} title={`Remove ${d?.title ?? 'this document'}?`} description="It leaves the Library and search. Answers given earlier keep their text.">
        <DialogFooter>
          <Button variant="ghost" onClick={() => setConfirm(false)}>Cancel</Button>
          <Button variant="danger" onClick={() => void remove()}>Remove</Button>
        </DialogFooter>
      </Dialog>
    </>
  );
}
