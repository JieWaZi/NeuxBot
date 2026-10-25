import { useMutation } from "@tanstack/react-query";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { SettingsRow } from "@/modules/settings";
import { updateImBinding } from "@/shared/api/generated/sdk.gen";
import {
  localApiErrorMessage,
  withLocalErrorSurface,
} from "@/shared/api/local-error-surface";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { QueryState } from "@/shared/ui/query-state";
import { Skeleton } from "@/shared/ui/skeleton";
import {
  bindingAuthorizationConfiguration,
  canAuthorizeWithoutConfiguration,
} from "../model/binding-authorization";
import { useIMBindings } from "../queries";
import { BindingForm } from "./binding-form";
import { BindingOverview } from "./binding-overview";
import { ConnectionChatPicker } from "./connection-chat-picker";
import { useDialogFocusRestore } from "./use-dialog-focus-restore";

export function ConnectionAuthorizationDialog({
  connectionId,
  onClose,
  onSaved,
}: {
  connectionId: string;
  onClose: () => void;
  onSaved: () => void;
}) {
  const { t } = useTranslation("im");
  const dialogFocus = useDialogFocusRestore();
  const query = useIMBindings(connectionId);
  const [bindingId, setBindingId] = useState<string>();
  const [confirmRevoke, setConfirmRevoke] = useState(false);
  const bindings = (query.data || []).filter((item) => item.id);
  const binding =
    bindings.find((item) => item.id === bindingId) ||
    bindings.find((item) => item.state !== "approved") ||
    bindings[0];
  const update = useMutation({
    mutationFn: (state: "approved" | "rejected") => {
      if (!binding?.id) throw new Error(t("operationFailed"));
      return updateImBinding(
        withLocalErrorSurface({
          path: { bindingId: binding.id },
          body: bindingAuthorizationConfiguration(binding, state),
          throwOnError: true,
        }),
      );
    },
    onSuccess: () => {
      onSaved();
      onClose();
    },
  });
  const picker = binding ? (
    <ConnectionChatPicker
      bindings={bindings}
      binding={binding}
      disabled={update.isPending}
      onSelect={(id) => {
        setBindingId(id);
        setConfirmRevoke(false);
        update.reset();
      }}
    />
  ) : null;
  const needsConfiguration =
    binding &&
    (binding.state === "approved"
      ? !confirmRevoke
      : !canAuthorizeWithoutConfiguration(binding));
  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !update.isPending) onClose();
      }}
    >
      <DialogContent {...dialogFocus} size="lg" title={t("authorize")}>
        {query.isError ? (
          <div className="px-6 pb-6">
            <QueryState
              error={query.error}
              onRetry={() => void query.refetch()}
            />
          </div>
        ) : query.isPending ? (
          <div
            role="status"
            aria-busy="true"
            aria-label={t("loading")}
            className="grid gap-3 px-6 pb-6"
          >
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-24 w-full" />
          </div>
        ) : !binding ? (
          <p className="type-body text-content-secondary px-6 pb-6">
            {t("waitingForChat")}
          </p>
        ) : needsConfiguration ? (
          <BindingForm
            key={binding.id}
            purpose={binding.state === "approved" ? "edit" : "authorization"}
            binding={binding}
            beforeFields={
              <>
                {picker}
                {binding.state === "approved" ? (
                  <SettingsRow
                    title={t("revoke")}
                    description={t("revokeHint", {
                      name: binding.name || binding.externalChatId,
                    })}
                  >
                    <Button
                      type="button"
                      size="sm"
                      variant="danger-quiet"
                      onClick={() => setConfirmRevoke(true)}
                    >
                      {t("revoke")}
                    </Button>
                  </SettingsRow>
                ) : null}
              </>
            }
            onClose={onClose}
            onSaved={onSaved}
          />
        ) : (
          <>
            <div className="grid min-h-0 gap-6 overflow-y-auto px-6 pb-6">
              {picker}
              <BindingOverview binding={binding} />
              <p className="type-body text-content-secondary">
                {t(
                  binding.state === "approved"
                    ? "revokeHint"
                    : binding.kind === "group"
                      ? "groupAuthorization"
                      : "directAuthorization",
                  { name: binding.name || binding.externalChatId },
                )}
              </p>
              {update.error ? (
                <p role="alert" className="type-body text-destructive">
                  {localApiErrorMessage(update.error, t("operationFailed"))}
                </p>
              ) : null}
            </div>
            <DialogFooter>
              <Button
                variant="secondary"
                disabled={update.isPending}
                onClick={() =>
                  binding.state === "approved"
                    ? setConfirmRevoke(false)
                    : onClose()
                }
              >
                {t("cancel")}
              </Button>
              {binding.state === "approved" ? (
                <Button
                  variant="danger"
                  disabled={update.isPending}
                  onClick={() => update.mutate("rejected")}
                >
                  {t("revoke")}
                </Button>
              ) : (
                <Button
                  disabled={update.isPending}
                  onClick={() => update.mutate("approved")}
                >
                  {t("authorize")}
                </Button>
              )}
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
