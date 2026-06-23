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
  return createSSEStore(readable(url), eventName);
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

// ── reactive variants ───────────────────────────────────────────────────────
//
// These take a Readable<string> URL instead of a fixed string. When the URL
// changes (e.g. the user switches accounts) the underlying EventSource is torn
// down and reopened against the new URL. An empty URL leaves the stream idle.

/** Like sseStore, but re-subscribes whenever the URL store changes. */
export function sseStoreReactive<T>(url: Readable<string>, eventName: string): SSEStore<T> {
  return createSSEStore(url, eventName);
}

/** Like sseLog, but re-subscribes (and resets the log) when the URL changes. */
export function sseLogReactive<T>(url: Readable<string>, eventName: string, maxItems = 100): Readable<T[]> {
  return readable<T[]>([], (set) => {
    let items: T[] = [];
    let es: EventSource | null = null;
    const unsub = url.subscribe((u) => {
      es?.close();
      es = null;
      items = [];
      set(items);
      if (!u) return;
      es = new EventSource(u);
      es.addEventListener(eventName, (e: MessageEvent) => {
        try {
          items = [JSON.parse(e.data) as T, ...items].slice(0, maxItems);
          set(items);
        } catch {
          // ignore
        }
      });
    });
    return () => {
      es?.close();
      unsub();
    };
  });
}

function createSSEStore<T>(url: Readable<string>, eventName: string): SSEStore<T> {
  let es: EventSource | null = null;
  let urlUnsub: (() => void) | null = null;
  let subscribers = 0;
  let dataValue: T | null = null;
  let statusValue: SSEStatus = 'closed';

  const dataSubs = new Set<(value: T | null) => void>();
  const statusSubs = new Set<(value: SSEStatus) => void>();

  const setData = (value: T | null) => {
    dataValue = value;
    for (const sub of dataSubs) sub(value);
  };
  const setStatus = (value: SSEStatus) => {
    statusValue = value;
    for (const sub of statusSubs) sub(value);
  };

  const close = () => {
    es?.close();
    es = null;
  };

  const connect = (u: string) => {
    close();
    setData(null);
    if (!u) {
      setStatus('closed');
      return;
    }
    setStatus('connecting');
    const current = new EventSource(u);
    es = current;

    current.addEventListener(eventName, (e: MessageEvent) => {
      if (es !== current) return;
      try {
        setData(JSON.parse(e.data) as T);
      } catch {
        // malformed JSON — ignore
      }
    });

    current.onopen = () => {
      if (es !== current) return;
      setStatus('open');
    };
    current.onerror = () => {
      if (es !== current) return;
      setStatus(current.readyState === EventSource.CLOSED ? 'closed' : 'error');
    };
  };

  const start = () => {
    subscribers += 1;
    if (subscribers !== 1) return;
    urlUnsub = url.subscribe(connect);
  };

  const stop = () => {
    if (subscribers === 0) return;
    subscribers -= 1;
    if (subscribers !== 0) return;
    close();
    urlUnsub?.();
    urlUnsub = null;
  };

  const subscribeData = (run: (value: T | null) => void) => {
    dataSubs.add(run);
    run(dataValue);
    start();
    return () => {
      dataSubs.delete(run);
      stop();
    };
  };

  const subscribeStatus = (run: (value: SSEStatus) => void) => {
    statusSubs.add(run);
    run(statusValue);
    start();
    return () => {
      statusSubs.delete(run);
      stop();
    };
  };

  return {
    data: { subscribe: subscribeData },
    status: { subscribe: subscribeStatus },
  };
}
