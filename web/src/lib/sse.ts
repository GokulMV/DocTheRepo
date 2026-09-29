// Parses a text/event-stream body into (event, data) pairs. Used by Ask, which POSTs (EventSource only
// supports GET) and reads the stream with fetch.

export interface SSEEvent {
  event: string;
  data: string;
}

/** parseSSE splits buffered stream text into complete events and returns the unconsumed remainder. */
export function parseSSE(buffer: string): { events: SSEEvent[]; rest: string } {
  const events: SSEEvent[] = [];
  const normalized = buffer.replace(/\r\n/g, '\n');
  const blocks = normalized.split('\n\n');
  const rest = blocks.pop() ?? '';
  for (const block of blocks) {
    let event = 'message';
    const data: string[] = [];
    for (const line of block.split('\n')) {
      if (line.startsWith(':')) continue;
      const i = line.indexOf(':');
      const field = i < 0 ? line : line.slice(0, i);
      const value = i < 0 ? '' : line.slice(i + 1).replace(/^ /, '');
      if (field === 'event') event = value;
      if (field === 'data') data.push(value);
    }
    if (data.length) events.push({ event, data: data.join('\n') });
  }
  return { events, rest };
}

/** readSSE reads a streaming Response and calls onEvent for each event. */
export async function readSSE(res: Response, onEvent: (e: SSEEvent) => void, signal?: AbortSignal): Promise<void> {
  const reader = res.body!.getReader();
  const decoder = new TextDecoder();
  let buf = '';
  for (;;) {
    if (signal?.aborted) {
      await reader.cancel();
      return;
    }
    const { value, done } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    const { events, rest } = parseSSE(buf);
    buf = rest;
    events.forEach(onEvent);
  }
  const { events } = parseSSE(buf + '\n\n');
  events.forEach(onEvent);
}
