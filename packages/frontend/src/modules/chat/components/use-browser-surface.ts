export { usePlatformBrowserSurface as useBrowserSurface } from "@/shared/platform/browser-surface";

export function browserErrorMessage(error: unknown, fallback: string) {
  if (error instanceof Error) return error.message;
  if (typeof error === "string") return error;
  return fallback;
}
