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
