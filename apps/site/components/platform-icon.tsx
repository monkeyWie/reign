import { Globe } from "lucide-react";
import paths from "../../../internal/brandicons/paths.json";

export type PlatformIconName =
  "macos" | "windows" | "linux" | "android" | "ios" | "web";

export function PlatformIcon({
  platform,
  size = 24,
  className,
}: {
  platform: PlatformIconName;
  size?: number;
  className?: string;
}) {
  if (platform === "web")
    return <Globe size={size} className={className} aria-hidden="true" />;
  const key = platform === "macos" || platform === "ios" ? "apple" : platform;
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="currentColor"
      className={className}
      aria-hidden="true"
      focusable="false"
      data-platform-icon={platform}
    >
      <path d={paths[key]} />
    </svg>
  );
}
