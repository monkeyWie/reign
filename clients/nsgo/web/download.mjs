// Download fully before compilation, so an interrupted HTTP body can be
// resumed without aborting WebAssembly.compileStreaming halfway through.
export async function downloadWasm(
  url,
  {
    size = 0,
    onProgress = () => {},
    onRetry = () => {},
    fetcher = fetch,
    retries = 2,
    delay = (n) => new Promise((resolve) => setTimeout(resolve, n)),
  } = {},
) {
  let chunks = [],
    loaded = 0;
  onProgress(loaded, size);
  for (let attempt = 0; ; attempt++) {
    try {
      const response = await fetcher(
        url,
        loaded ? { headers: { Range: `bytes=${loaded}-` } } : {},
      );
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
      if (attempt >= retries || error?.name === "AbortError") throw error;
      onRetry(attempt + 1, loaded, size);
      await delay(500 * (attempt + 1));
    }
  }
}
