import { useMutation } from "@tanstack/react-query";
import { useState } from "react";
import { updateImBinding } from "@/shared/api/generated/sdk.gen";
import { withLocalErrorSurface } from "@/shared/api/local-error-surface";
import type { IMBinding } from "../types";

export function useBindingAuthorization({
  binding,
  refresh,
  onAuthorized,
}: {
  binding: IMBinding;
  refresh: () => Promise<unknown>;
  onAuthorized: () => void;
}) {
  const [confirmation, setConfirmation] = useState(false);
  if (confirmation && binding.state !== "approved") setConfirmation(false);
  const update = useMutation({
    mutationFn: (state: "approved" | "rejected") =>
      updateImBinding(
        withLocalErrorSurface({
          path: { bindingId: binding.id || "" },
          body: { state },
          throwOnError: true,
        }),
      ),
    onSuccess: async (_, state) => {
      setConfirmation(false);
      await refresh();
      if (state === "approved") onAuthorized();
    },
  });

  return {
    confirmingRevocation: confirmation,
    error: update.error,
    isPending: update.isPending,
    approve: () => update.mutate("approved"),
    startRevocation: () => {
      update.reset();
      setConfirmation(true);
    },
    cancelRevocation: () => {
      update.reset();
      setConfirmation(false);
    },
    confirmRevocation: () => {
      if (confirmation) update.mutate("rejected");
    },
  };
}
