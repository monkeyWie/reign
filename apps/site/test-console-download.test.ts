import assert from "node:assert/strict";
import { test } from "node:test";
import { downloadWasm } from "../../clients/nsgo/web/download.mjs";

test("workspace download reports bytes and resumes an interrupted body", async () => {
  let requests = 0;
  const progress: number[] = [];
  const retries: number[] = [];
  const blob = await downloadWasm("https://example.com/app.wasm.gz", {
    size: 6,
    delay: async () => {},
    onProgress: (loaded: number) => progress.push(loaded),
    onRetry: (n: number) => retries.push(n),
    fetcher: async (_: unknown, init: RequestInit) => {
      if (requests++ === 0) {
        let n = 0;
        return new Response(
          new ReadableStream({
            pull(c) {
              if (n++ === 0) c.enqueue(new Uint8Array([1, 2, 3]));
              else c.error(new TypeError("Network error"));
            },
          }),
        );
      }
      assert.deepEqual(init.headers, { Range: "bytes=3-" });
      return new Response(new Uint8Array([4, 5, 6]), {
        status: 206,
        headers: { "Content-Range": "bytes 3-5/6" },
      });
    },
  });
  assert.deepEqual(
    [...new Uint8Array(await blob.arrayBuffer())],
    [1, 2, 3, 4, 5, 6],
  );
  assert.deepEqual(progress, [0, 3, 6]);
  assert.deepEqual(retries, [1]);
});

test("a server ignoring Range restarts cleanly instead of duplicating bytes", async () => {
  let n = 0;
  const blob = await downloadWasm("https://example.com/app.wasm.gz", {
    size: 3,
    delay: async () => {},
    fetcher: async () =>
      new Response(new Uint8Array(++n === 1 ? [1] : [1, 2, 3])),
  });
  assert.equal(n, 2);
  assert.deepEqual([...new Uint8Array(await blob.arrayBuffer())], [1, 2, 3]);
});

test("download failure stops after a bounded number of retries", async () => {
  let attempts = 0;
  await assert.rejects(
    downloadWasm("https://example.com/app.wasm.gz", {
      delay: async () => {},
      fetcher: async () => {
        attempts++;
        throw new TypeError("Network error");
      },
    }),
    /Network error/,
  );
  assert.equal(attempts, 3);
});

test("workspace parts download concurrently with a bound and assemble in manifest order", async () => {
  let active = 0,
    peak = 0;
  const pending: (() => void)[] = [];
  const finished: number[] = [];
  const progress: number[] = [];
  const download = downloadWasm("https://example.com/assets/app.wasm.gz", {
    size: 6,
    concurrency: 2,
    parts: [0, 1, 2].map((i) => ({ file: `app.part-${i}.bin`, size: 2 })),
    onProgress: (n: number) => progress.push(n),
    fetcher: async (url: URL) => {
      const i = Number(url.pathname.match(/part-(\d+)/)![1]);
      peak = Math.max(peak, ++active);
      await new Promise<void>((resolve) => {
        pending[i] = resolve;
      });
      active--;
      finished.push(i);
      return new Response(new Uint8Array([i * 2 + 1, i * 2 + 2]));
    },
  });
  const tick = () => new Promise((resolve) => setTimeout(resolve, 0));
  await tick();
  assert.equal(active, 2);
  pending[1]();
  await tick();
  assert.equal(active, 2);
  pending[2]();
  await tick();
  pending[0]();
  const blob = await download;
  assert.equal(peak, 2);
  assert.deepEqual(finished, [1, 2, 0]);
  assert.deepEqual(
    [...new Uint8Array(await blob.arrayBuffer())],
    [1, 2, 3, 4, 5, 6],
  );
  assert.equal(progress.at(-1), 6);
});

test("an interrupted part retries independently without redownloading completed parts", async () => {
  const requests: Record<string, number> = {};
  const blob = await downloadWasm("https://example.com/app.wasm.gz", {
    size: 4,
    parts: [
      { file: "a.bin", size: 2 },
      { file: "b.bin", size: 2 },
    ],
    delay: async () => {},
    fetcher: async (url: URL, init: RequestInit) => {
      const key = url.pathname;
      requests[key] = (requests[key] || 0) + 1;
      if (key === "/b.bin" && requests[key] === 1) {
        let n = 0;
        return new Response(
          new ReadableStream({
            pull(c) {
              if (!n++) c.enqueue(new Uint8Array([3]));
              else c.error(new TypeError("Network error"));
            },
          }),
        );
      }
      if (key === "/b.bin") {
        assert.deepEqual(init.headers, { Range: "bytes=1-" });
        return new Response(new Uint8Array([4]), {
          status: 206,
          headers: { "Content-Range": "bytes 1-1/2" },
        });
      }
      return new Response(new Uint8Array([1, 2]));
    },
  });
  assert.deepEqual(requests, { "/a.bin": 1, "/b.bin": 2 });
  assert.deepEqual([...new Uint8Array(await blob.arrayBuffer())], [1, 2, 3, 4]);
});

test("invalid part manifests fail before fetching", async () => {
  await assert.rejects(
    downloadWasm("https://example.com/app.wasm.gz", {
      size: 4,
      parts: [{ file: "a.bin", size: 2 }],
      fetcher: async () => {
        throw new Error("must not fetch");
      },
    }),
    /Invalid workspace manifest/,
  );
});

test("a failed part cancels remaining downloads", async () => {
  let aborted = false;
  await assert.rejects(
    downloadWasm("https://example.com/app.wasm.gz", {
      retries: 0,
      parts: [
        { file: "bad.bin", size: 1 },
        { file: "slow.bin", size: 1 },
      ],
      fetcher: async (url: URL, init: RequestInit) => {
        if (url.pathname === "/bad.bin") throw new Error("unavailable");
        return new Promise((_, reject) =>
          init.signal!.addEventListener("abort", () => {
            aborted = true;
            reject(init.signal!.reason);
          }),
        );
      },
    }),
    /unavailable/,
  );
  assert.ok(aborted);
});
