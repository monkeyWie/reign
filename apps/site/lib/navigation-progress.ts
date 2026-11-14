import NProgress from "nprogress";

NProgress.configure({
  showSpinner: false,
  minimum: 0.12,
  trickleSpeed: 180,
});

let timeout: ReturnType<typeof setTimeout> | undefined;

export function finishNavigation() {
  clearTimeout(timeout);
  timeout = undefined;
  NProgress.done();
}

export function startNavigation(
  href: string,
  navigationType?: "push" | "replace" | "traverse",
) {
  const next = new URL(href, location.href);
  if (
    next.origin !== location.origin ||
    (navigationType !== "traverse" &&
      next.pathname === location.pathname &&
      next.search === location.search)
  )
    return;
  NProgress.start();
  clearTimeout(timeout);
  // A failed or cancelled navigation must not leave a permanent progress bar.
  timeout = setTimeout(finishNavigation, 30000);
}
