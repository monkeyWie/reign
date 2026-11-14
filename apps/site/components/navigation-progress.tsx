"use client";

import { Suspense, useEffect } from "react";
import { usePathname, useSearchParams } from "next/navigation";
import { finishNavigation } from "../lib/navigation-progress";

function NavigationCompletion() {
  const pathname = usePathname();
  const search = useSearchParams();
  useEffect(() => {
    finishNavigation();
  }, [pathname, search]);
  return null;
}

export function NavigationProgress() {
  return (
    <Suspense fallback={null}>
      <NavigationCompletion />
    </Suspense>
  );
}
