// Download fully before compilation, so an interrupted HTTP body can be
// resumed without aborting WebAssembly.compileStreaming halfway through.
export async function downloadWasm(
  url,
  { parts = [], concurrency = 4, ...options } = {},
) {
  if (!parts.length) return downloadFile(url, options);
  const total = parts.reduce((sum, part) => sum + part.size, 0);
  if (
    !parts.every((part) => Number.isSafeInteger(part.size) && part.size > 0) ||
    (options.size && total !== options.size)
  )
    throw new Error("Invalid workspace manifest");
  const controller = new AbortController();
  const blobs = new Array(parts.length);
  const loaded = parts.map(() => 0);
  const progress = () => loaded.reduce((a, b) => a + b, 0);
  let next = 0;
  options.onProgress?.(0, total);
  const worker = async () => {
    while (next < parts.length && !controller.signal.aborted) {
      const i = next++;
      const part = parts[i];
      blobs[i] = await downloadFile(
        new URL(part.file, new URL(url, globalThis.location?.href)),
        {
          ...options,
          size: part.size,
          signal: controller.signal,
          onProgress(bytes) {
            loaded[i] = bytes;
            options.onProgress?.(progress(), total);
          },
          onRetry(attempt) {
            options.onRetry?.(attempt, progress(), total);
          },
        },
      );
    }
  };
  // Parts are immutable CDN objects, avoiding servers that ignore Range.
  // Limit concurrency so mobile connections are not flooded with requests.
  const workers = Array.from(
    {
      length: Math.min(
        parts.length,
        Math.max(1, Math.min(6, Math.floor(concurrency) || 4)),
      ),
    },
    () =>
      worker().catch((error) => {
        controller.abort();
        throw error;
      }),
  );
  await Promise.all(workers);
  return new Blob(blobs);
}

async function downloadFile(
  url,
  {
    size = 0,
    onProgress = () => {},
    onRetry = () => {},
    fetcher = fetch,
    retries = 2,
    delay = (n) => new Promise((resolve) => setTimeout(resolve, n)),
    signal,
  } = {},
) {
  let chunks = [],
    loaded = 0;
  onProgress(loaded, size);
  for (let attempt = 0; ; attempt++) {
    try {
      signal?.throwIfAborted();
      const response = await fetcher(url, {
        ...(loaded ? { headers: { Range: `bytes=${loaded}-` } } : {}),
        ...(signal ? { signal } : {}),
      });
      if (!response.ok || !response.body)
        throw new Error(`HTTP ${response.status}`);
      if (response.status === 206) {
        const range = /^bytes (\d+)-\d+\/(\d+)$/.exec(
          response.headers.get("Content-Range") || "",
        );
        if (
          !range ||
          Number(range[1]) !== loaded ||
          (size && Number(range[2]) !== size)
        ) {
          chunks = [];
          loaded = 0;
          throw new Error("Invalid download range");
        }
        size ||= Number(range[2]);
      } else {
        chunks = [];
        loaded = 0;
      }
      const reader = response.body.getReader();
      try {
        for (;;) {
          const { done, value } = await reader.read();
          if (done) break;
          chunks.push(value);
          loaded += value.byteLength;
          onProgress(loaded, size);
          if (size && loaded > size) {
            chunks = [];
            loaded = 0;
            throw new Error("Invalid download size");
          }
        }
      } finally {
        await reader.cancel().catch(() => {});
        reader.releaseLock();
      }
      if (size && loaded !== size) throw new Error("Incomplete download");
      return new Blob(chunks);
    } catch (error) {
      if (attempt >= retries || signal?.aborted || error?.name === "AbortError")
        throw error;
      onRetry(attempt + 1, loaded, size);
      await delay(500 * (attempt + 1));
    }
  }
}
