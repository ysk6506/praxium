/**
 * The smallest way to read again after a write: a read names the topics it depends on (`"sources"`), a write says
 * which topics it changed, and the reads of those topics read again. There is no cache and no store of answers; this
 * only carries the news that something changed. A topic is a plain name and matches exactly: a read of `"sources"`
 * and a write that changes `"source:main"` do not meet unless the read lists both.
 */

type Listener = (topic: string) => void;

const listeners = new Set<Listener>();

/** Tell every read that depends on one of these topics that its answer may be old. */
export function invalidate(...topics: readonly string[]): void {
  for (const topic of topics) {
    for (const listener of [...listeners]) listener(topic);
  }
}

/** Hear the topics that are invalidated until the returned function is called. */
export function onInvalidate(listener: Listener): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}
