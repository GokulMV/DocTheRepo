import { useEffect, useId, useRef, useState } from 'react';
import ReactMarkdown from 'react-markdown';
import rehypeSanitize from 'rehype-sanitize';
import remarkGfm from 'remark-gfm';

// Mermaid is large; load it only when a diagram is on screen. securityLevel strict disables click
// handlers and HTML labels inside diagrams.
function Mermaid({ code }: { code: string }) {
  const ref = useRef<HTMLDivElement>(null);
  const id = useId().replace(/:/g, '');
  const [err, setErr] = useState<string>();
  useEffect(() => {
    let cancelled = false;
    import('mermaid')
      .then(async ({ default: mermaid }) => {
        mermaid.initialize({ startOnLoad: false, securityLevel: 'strict', theme: 'neutral' });
        const { svg } = await mermaid.render(`m${id}`, code);
        if (!cancelled && ref.current) ref.current.innerHTML = svg;
      })
      .catch((e: unknown) => !cancelled && setErr(String(e)));
    return () => {
      cancelled = true;
    };
  }, [code, id]);
  if (err) return <pre className="text-xs text-red-600">{code}</pre>;
  return <div ref={ref} className="my-3 overflow-x-auto" aria-label="diagram" />;
}

/** Markdown renders generated docs and answers: GFM, sanitized HTML, Mermaid code blocks. */
export function Markdown({ children }: { children: string }) {
  return (
    <div className="prose-sm max-w-none space-y-3 text-sm leading-relaxed [&_a]:text-brand-600 [&_a]:underline [&_code]:rounded [&_code]:bg-slate-100 [&_code]:px-1 [&_code]:font-mono [&_code]:text-[0.85em] dark:[&_code]:bg-slate-800 [&_h1]:text-lg [&_h1]:font-semibold [&_h2]:mt-5 [&_h2]:text-base [&_h2]:font-semibold [&_h3]:font-semibold [&_li]:ml-5 [&_ol]:list-decimal [&_pre]:overflow-x-auto [&_pre]:rounded-md [&_pre]:bg-slate-900 [&_pre]:p-3 [&_pre]:text-slate-100 [&_pre_code]:bg-transparent [&_pre_code]:p-0 [&_table]:w-full [&_td]:border [&_td]:px-2 [&_th]:border [&_th]:px-2 [&_ul]:list-disc">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeSanitize]}
        components={{
          code({ className, children, ...props }) {
            const lang = /language-(\w+)/.exec(className ?? '')?.[1];
            if (lang === 'mermaid') return <Mermaid code={String(children).trim()} />;
            return (
              <code className={className} {...props}>
                {children}
              </code>
            );
          },
          a({ href, children }) {
            return (
              <a href={href} target="_blank" rel="noreferrer noopener">
                {children}
              </a>
            );
          },
        }}
      >
        {children}
      </ReactMarkdown>
    </div>
  );
}
