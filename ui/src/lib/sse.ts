import { readable, type Readable } from 'svelte/store';

export type SSEStatus = 'connecting' | 'open' | 'closed' | 'error';

export interface SSEStore<T> {
  data: Readable<T | null>;
  status: Readable<SSEStatus>;
}

/**
 * Create a Svelte readable store backed by an SSE endpoint.
 * The EventSource auto-reconnects on drop; the store reflects the latest
 * value of the named event. Closes cleanly when the last subscriber leaves.
 */
export function sseStore<T>(url: string, eventName: string): SSEStore<T> {
  let es: EventSource | null = null;

  const data = readable<T | null>(null, (set) => {
    es = new EventSource(url);

    es.addEventListener(eventName, (e: MessageEvent) => {
      try {
        set(JSON.parse(e.data) as T);
      } catch {
        // malformed JSON — ignore
      }
    });

    return () => {
      es?.close();
      es = null;
    };
  });

  const status = readable<SSEStatus>('connecting', (set) => {
    // Re-open a separate EventSource just to track readyState.
    // Both share the same HTTP connection via the browser's SSE layer.
    const tracker = new EventSource(url);
    tracker.onopen = () => set('open');
    tracker.onerror = () =>
      set(tracker.readyState === EventSource.CLOSED ? 'closed' : 'error');
    return () => tracker.close();
  });

  return { data, status };
}

/**
 * Subscribe to an SSE endpoint and collect every matching event into an
 * append-only array (useful for an event log).
 */
export function sseLog<T>(url: string, eventName: string, maxItems = 100): Readable<T[]> {
  return readable<T[]>([], (set) => {
    let items: T[] = [];
    const es = new EventSource(url);

    es.addEventListener(eventName, (e: MessageEvent) => {
      try {
        const item = JSON.parse(e.data) as T;
        items = [item, ...items].slice(0, maxItems);
        set(items);
      } catch {
        // ignore
      }
    });

    return () => es.close();
  });
}
