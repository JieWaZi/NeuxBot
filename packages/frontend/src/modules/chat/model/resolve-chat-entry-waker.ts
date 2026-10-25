type ChatEntryWaker = {
  id?: string;
};

export function resolveChatEntryWakerId({
  requestedWakerId,
  lastWakerId,
  wakers,
}: {
  requestedWakerId?: string;
  lastWakerId?: string;
  wakers: readonly ChatEntryWaker[];
}): string {
  const available = new Set(
    wakers.flatMap((waker) => {
      const id = waker.id?.trim();
      return id ? [id] : [];
    }),
  );
  const requested = requestedWakerId?.trim();
  if (requested && (available.size === 0 || available.has(requested))) {
    return requested;
  }

  const last = lastWakerId?.trim();
  if (last && (available.size === 0 || available.has(last))) return last;

  return wakers[0]?.id?.trim() ?? "";
}
