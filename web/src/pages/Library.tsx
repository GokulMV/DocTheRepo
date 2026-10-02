import { Library as LibraryIcon } from 'lucide-react';
import { useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { api } from '@/api/client';
import { keys, useInvalidating, useMe, useShelf, useShelves } from '@/api/hooks';
import { atLeast } from '@/api/types';
import { Badge, Button, Card, Dialog, Empty, ErrorNote, Field, Input, PageHeader, Spinner, Textarea, Toggle } from '@/components/ui';

function NewShelf() {
  const [open, setOpen] = useState(false);
  const [slug, setSlug] = useState('');
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const create = useInvalidating((b: object) => api.post('/library/shelves', b), keys.shelves);
  return (
    <>
      <Button onClick={() => setOpen(true)}>New shelf</Button>
      <Dialog open={open} onOpenChange={setOpen} title="New curated shelf" description="Curated shelves are filled by people; pin docs or entities to them.">
        <form
          className="space-y-3"
          onSubmit={async (e) => {
            e.preventDefault();
            await create.mutateAsync({ slug, title, description, curated: true });
            setOpen(false);
          }}
        >
          <Field label="Title">
            <Input required value={title} onChange={(e) => { setTitle(e.target.value); setSlug(e.target.value.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '')); }} />
          </Field>
          <Field label="Slug">
            <Input required value={slug} onChange={(e) => setSlug(e.target.value)} />
          </Field>
          <Field label="Description">
            <Textarea rows={2} value={description} onChange={(e) => setDescription(e.target.value)} />
          </Field>
          <ErrorNote error={create.error} />
          <Button type="submit" disabled={create.isPending}>Create</Button>
        </form>
      </Dialog>
    </>
  );
}

export default function Library() {
  const { slug } = useParams();
  const me = useMe();
  const shelves = useShelves();
  const shelf = useShelf(slug);
  const [showEmpty, setShowEmpty] = useState(false);
  return (
    <>
      <PageHeader
        title="Library"
        description="Documentation organised by topic — architecture, services, APIs, runbooks, decisions — whichever repository it came from."
        actions={atLeast(me.data?.role, 'editor') && <NewShelf />}
      />
      {shelves.isLoading && <Spinner />}
      <ErrorNote error={shelves.error} />
      {!slug && (
        <>
          <Toggle label="Show empty shelves" checked={showEmpty} onChange={setShowEmpty} />
          <div className="mt-3 grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
            {shelves.data
              ?.filter((s) => showEmpty || s.item_count > 0)
              .map((s) => (
                <Link key={s.id} to={`/library/${s.slug}`} className="rounded-lg border border-slate-200 bg-white p-4 hover:border-brand-500 dark:border-slate-800 dark:bg-slate-900">
                  <div className="flex items-center justify-between">
                    <h2 className="font-medium">{s.title}</h2>
                    <Badge>{s.item_count}</Badge>
                  </div>
                  <p className="mt-1 text-sm text-slate-500">{s.description}</p>
                  {s.curated && <Badge tone="blue">curated</Badge>}
                </Link>
              ))}
          </div>
        </>
      )}
      {slug && (
        <Card title={shelf.data?.title ?? slug} actions={<Link to="/library" className="text-sm text-brand-600">All shelves</Link>}>
          {shelf.isLoading && <Spinner />}
          <ErrorNote error={shelf.error} />
          {shelf.data?.items?.length === 0 && <Empty icon={LibraryIcon} title="Nothing on this shelf yet" />}
          <ul className="divide-y divide-slate-100 dark:divide-slate-800">
            {shelf.data?.items?.map((i) => (
              <li key={i.type + i.id} className="py-2 text-sm">
                {i.type === 'doc_node' ? (
                  <Link to={`/docs/${i.id}`} className="font-medium text-brand-600">{i.title}</Link>
                ) : i.type === 'confluence_page' || i.type === 'jira_issue' ? (
                  <a href={i.path} target="_blank" rel="noreferrer" className="font-medium text-brand-600">{i.title}</a>
                ) : (
                  <Link to={`/palace/${i.id}`} className="font-medium text-brand-600">{i.title}</Link>
                )}
                {i.type === 'confluence_page' && <Badge>Confluence</Badge>}
                {i.type === 'jira_issue' && <Badge>Jira</Badge>}
                {i.pinned && <Badge tone="amber">pinned</Badge>}
                {i.type !== 'confluence_page' && i.type !== 'jira_issue' && <p className="font-mono text-xs text-slate-500">{i.path}</p>}
                {i.summary && <p className="text-slate-600 dark:text-slate-400">{i.summary}</p>}
                {i.note && <p className="text-xs italic text-slate-500">{i.note}</p>}
              </li>
            ))}
          </ul>
        </Card>
      )}
    </>
  );
}
