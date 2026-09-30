import { useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useDocNode, useTree } from '@/api/hooks';
import type { TreeNode } from '@/api/types';
import { Markdown } from '@/components/Markdown';
import { Badge, Card, Empty, ErrorNote, PageHeader, Spinner, cx } from '@/components/ui';
import { relTime, shortSha } from '@/lib/format';

function Branch({ node, repoId, selected, onSelect, depth }: {
  node: TreeNode;
  repoId: string;
  selected?: string;
  onSelect: (n: TreeNode) => void;
  depth: number;
}) {
  const [open, setOpen] = useState(depth === 0);
  const children = useTree(open && node.has_children ? repoId : undefined, open && node.kind !== 'repo' ? node.id : undefined);
  const expandable = node.has_children && node.kind !== 'file';
  return (
    <li>
      <button
        className={cx(
          'flex w-full items-center gap-1 truncate rounded px-1.5 py-1 text-left text-sm hover:bg-slate-100 dark:hover:bg-slate-800',
          selected === node.id && 'bg-brand-50 font-medium text-brand-700 dark:bg-brand-700/20',
        )}
        style={{ paddingLeft: depth * 12 + 6 }}
        onClick={() => (expandable ? setOpen(!open) : onSelect(node))}
        aria-expanded={expandable ? open : undefined}
      >
        <span aria-hidden className="w-3 text-slate-400">{expandable ? (open ? '▾' : '▸') : ''}</span>
        <span className="truncate">{node.title}</span>
      </button>
      {open && expandable && (
        <ul>
          {children.isLoading && <li className="pl-8 text-xs text-slate-400">loading…</li>}
          {children.data?.map((c) => (
            <Branch key={c.id} node={c} repoId={repoId} selected={selected} onSelect={onSelect} depth={depth + 1} />
          ))}
        </ul>
      )}
    </li>
  );
}

function RepoTree({ root, selected, onSelect }: { root: TreeNode; selected?: string; onSelect: (n: TreeNode) => void }) {
  const [open, setOpen] = useState(false);
  const children = useTree(open ? root.repo_id : undefined);
  return (
    <li>
      <button className="flex w-full items-center gap-1 rounded px-1.5 py-1 text-left text-sm font-semibold hover:bg-slate-100 dark:hover:bg-slate-800" onClick={() => setOpen(!open)} aria-expanded={open}>
        <span aria-hidden className="w-3 text-slate-400">{open ? '▾' : '▸'}</span>
        {root.title}
      </button>
      {open && (
        <ul>
          {children.data?.map((c) => (
            <Branch key={c.id} node={c} repoId={root.repo_id!} selected={selected} onSelect={onSelect} depth={1} />
          ))}
        </ul>
      )}
    </li>
  );
}

export default function Docs() {
  const { nodeId } = useParams();
  const nav = useNavigate();
  const roots = useTree();
  const node = useDocNode(nodeId);
  return (
    <>
      <PageHeader title="Docs" description="Generated and imported documentation, per repository. Edit generated files only inside dth:human blocks — those survive regeneration." />
      <div className="grid gap-6 lg:grid-cols-[18rem_1fr]">
        <Card title="Tree">
          {roots.isLoading && <Spinner />}
          <ErrorNote error={roots.error} />
          {roots.data?.length === 0 && <Empty title="No docs yet">Track a repository and push, or import existing Markdown.</Empty>}
          <ul className="space-y-0.5">
            {roots.data?.map((r) => (
              <RepoTree key={r.id} root={r} selected={nodeId} onSelect={(n) => nav(`/docs/${n.id}`)} />
            ))}
          </ul>
        </Card>
        <div className="min-w-0">
          {!nodeId && <Empty title="Pick a document">Choose a file in the tree.</Empty>}
          {node.isLoading && <Spinner />}
          <ErrorNote error={node.error} />
          {node.data && (
            <Card
              title={
                <span className="font-mono text-xs">
                  {node.data.repo} / {node.data.path}
                </span>
              }
              actions={
                <span className="text-xs text-slate-500">
                  {node.data.commit_sha && <>at {shortSha(node.data.commit_sha)} · </>}updated {relTime(node.data.updated_at)}
                </span>
              }
            >
              {node.data.summary && <p className="mb-3 text-sm text-slate-600 dark:text-slate-400">{node.data.summary}</p>}
              {node.data.markdown ? <Markdown>{node.data.markdown}</Markdown> : <p className="text-sm text-slate-500">No content stored for this node.</p>}
              {node.data.chunks.length > 0 && (
                <div className="mt-4 flex flex-wrap gap-1 border-t border-slate-200 pt-3 dark:border-slate-800">
                  {node.data.chunks.map((c) => (
                    <Badge key={c.chunk_id} tone="blue">
                      {c.symbol}
                    </Badge>
                  ))}
                </div>
              )}
            </Card>
          )}
        </div>
      </div>
    </>
  );
}
