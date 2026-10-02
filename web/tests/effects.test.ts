import fs from 'node:fs';
import path from 'node:path';
import ts from 'typescript';
import { describe, expect, it } from 'vitest';

// An effect may return only a cleanup function. Returning anything else (for example the Promise newer browsers
// return from scrollIntoView) makes React call it on unmount and crash the next page. Effects therefore use a
// block body, and every `return` in one returns a function.
function sources(dir: string, out: string[] = []): string[] {
  for (const f of fs.readdirSync(dir)) {
    const p = path.join(dir, f);
    if (fs.statSync(p).isDirectory()) sources(p, out);
    else if (/\.tsx?$/.test(f)) out.push(p);
  }
  return out;
}

describe('effects', () => {
  it('return nothing or a cleanup function', () => {
    const bad: string[] = [];
    for (const file of sources(path.join(import.meta.dirname, '../src'))) {
      const sf = ts.createSourceFile(file, fs.readFileSync(file, 'utf8'), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
      const where = (n: ts.Node) => `${path.relative(process.cwd(), file)}:${sf.getLineAndCharacterOfPosition(n.getStart(sf)).line + 1}`;
      const isFn = (e: ts.Expression) => ts.isArrowFunction(e) || ts.isFunctionExpression(e);
      const visit = (n: ts.Node) => {
        if (ts.isCallExpression(n) && /^(React\.)?use(Layout|Insertion)?Effect$/.test(n.expression.getText(sf))) {
          const fn = n.arguments[0];
          if (fn && (ts.isArrowFunction(fn) || ts.isFunctionExpression(fn))) {
            if (fn.modifiers?.some((m) => m.kind === ts.SyntaxKind.AsyncKeyword)) bad.push(`${where(n)} async effect`);
            if (!ts.isBlock(fn.body)) {
              if (!isFn(fn.body as ts.Expression)) bad.push(`${where(n)} expression body`);
            } else {
              const rets = (b: ts.Node) => ts.forEachChild(b, (c) => {
                if (ts.isFunctionLike(c)) return;
                if (ts.isReturnStatement(c) && c.expression && !isFn(c.expression)) bad.push(`${where(c)} returns a non-function`);
                rets(c);
              });
              rets(fn.body);
            }
          }
        }
        ts.forEachChild(n, visit);
      };
      visit(sf);
    }
    expect(bad).toEqual([]);
  });
});
