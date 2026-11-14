import { finishNavigation, startNavigation } from "./lib/navigation-progress";

// Next's hook covers Links, programmatic push/replace and browser back/forward.
export const onRouterTransitionStart = startNavigation;

// Native links (e.g. language changes between root layouts) load a new document.
document.addEventListener("click", (event) => {
  if (
    event.defaultPrevented ||
    event.button !== 0 ||
    event.metaKey ||
    event.ctrlKey ||
    event.shiftKey ||
    event.altKey
  )
    return;
  const link = event
    .composedPath()
    .find(
      (node): node is HTMLAnchorElement => node instanceof HTMLAnchorElement,
    );
  if (
    !link ||
    !link.hasAttribute("href") ||
    link.hasAttribute("download") ||
    (link.target && link.target !== "_self")
  )
    return;
  startNavigation(link.href);
});

// Also clear progress when restoring a document from the browser's back cache.
window.addEventListener("pageshow", finishNavigation);
