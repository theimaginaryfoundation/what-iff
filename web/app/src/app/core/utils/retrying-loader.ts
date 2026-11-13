/**
 * A loader that runs `load` once and shares its promise, unless it fails: a failed load (a chunk
 * that did not download) is forgotten, so the next call tries again instead of failing until reload.
 */
export function retryingLoader<T>(load: () => Promise<T>): () => Promise<T> {
  let loading: Promise<T> | null = null;
  return () => {
    loading ??= load().catch(error => {
      loading = null;
      throw error;
    });
    return loading;
  };
}
