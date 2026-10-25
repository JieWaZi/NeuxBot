import { useState } from "react";
import { useTranslation } from "react-i18next";

import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogFooter } from "@/shared/ui/dialog";
import { Input } from "@/shared/ui/input";
import { SegmentedControl } from "@/shared/ui/segmented-control";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/shared/ui/select";

export type SessionTransitionChoice = {
  turns: number;
  mode: "recent" | "summary";
  summaryModel: string;
  summaryReasoning: string;
};

type SummaryOption = {
  id: string;
  name: string;
  reasoningOptions?: Array<{ id: string; name: string }>;
};

export function ChatSessionTransitionDialog({
  open,
  reason,
  models,
  reasoningOptions,
  defaultModel,
  defaultReasoning,
  onCancel,
  onConfirm,
}: {
  open: boolean;
  reason: "switch" | "recovery";
  models: SummaryOption[];
  reasoningOptions: Array<{ id: string; name: string }>;
  defaultModel: string;
  defaultReasoning: string;
  onCancel: () => void;
  onConfirm: (choice: SessionTransitionChoice) => void;
}) {
  const { t } = useTranslation("chat");
  const [turns, setTurns] = useState(5);
  const [mode, setMode] = useState<SessionTransitionChoice["mode"]>("recent");
  const [summaryModel, setSummaryModel] = useState("");
  const [summaryReasoning, setSummaryReasoning] = useState("");
  const model =
    models.find((item) => item.id === summaryModel)?.id || defaultModel;
  const modelReasoningOptions = models.find((item) => item.id === model)
    ?.reasoningOptions?.length
    ? (models.find((item) => item.id === model)?.reasoningOptions ?? [])
    : reasoningOptions;
  const selectedReasoning =
    modelReasoningOptions.find((item) => item.id === summaryReasoning)?.id ??
    modelReasoningOptions.find((item) => item.id === defaultReasoning)?.id ??
    modelReasoningOptions[0]?.id ??
    "";
  const valid =
    Number.isInteger(turns) &&
    turns >= 1 &&
    turns <= 20 &&
    (mode === "recent" || Boolean(model));

  const cancel = () => {
    setTurns(5);
    setMode("recent");
    setSummaryModel("");
    setSummaryReasoning("");
    onCancel();
  };

  return (
    <Dialog open={open} onOpenChange={(next) => !next && cancel()}>
      <DialogContent
        title={t("sessionTransitionTitle")}
        description={t(
          reason === "recovery"
            ? "sessionRecoveryDescription"
            : "sessionTransitionDescription",
        )}
        size="sm"
      >
        <div className="grid gap-2 px-6 pb-5">
          <span className="type-body font-medium">
            {t("sessionTransitionMode")}
          </span>
          <SegmentedControl
            ariaLabel={t("sessionTransitionMode")}
            distribution="fill"
            size="sm"
            value={mode}
            options={[
              { value: "recent", label: t("sessionTransitionRecent") },
              { value: "summary", label: t("sessionTransitionSummary") },
            ]}
            onValueChange={setMode}
          />
          {mode === "summary" ? (
            <>
              <p className="text-muted-foreground type-caption">
                {t("sessionTransitionSummaryHint")}
              </p>
              <label
                className="type-body font-medium"
                htmlFor="chat-summary-model"
              >
                {t("sessionTransitionSummaryModel")}
              </label>
              <Select value={model} onValueChange={setSummaryModel}>
                <SelectTrigger
                  id="chat-summary-model"
                  size="sm"
                  aria-label={t("sessionTransitionSummaryModel")}
                >
                  <SelectValue placeholder={t("sessionTransitionNoModel")} />
                </SelectTrigger>
                <SelectContent>
                  {models.map((option) => (
                    <SelectItem key={option.id} value={option.id}>
                      {option.name || option.id}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {modelReasoningOptions.length ? (
                <>
                  <label
                    className="type-body font-medium"
                    htmlFor="chat-summary-reasoning"
                  >
                    {t("sessionTransitionSummaryReasoning")}
                  </label>
                  <Select
                    value={selectedReasoning}
                    onValueChange={setSummaryReasoning}
                  >
                    <SelectTrigger
                      id="chat-summary-reasoning"
                      size="sm"
                      aria-label={t("sessionTransitionSummaryReasoning")}
                    >
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {modelReasoningOptions.map((option) => (
                        <SelectItem key={option.id} value={option.id}>
                          {option.name || option.id}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </>
              ) : null}
            </>
          ) : null}
          <label className="type-body font-medium" htmlFor="chat-history-turns">
            {t(
              mode === "summary"
                ? "sessionTransitionFallbackTurns"
                : "sessionTransitionTurns",
            )}
          </label>
          <Input
            id="chat-history-turns"
            size="sm"
            type="number"
            min={1}
            max={20}
            value={Number.isNaN(turns) ? "" : turns}
            onChange={(event) => setTurns(event.target.valueAsNumber)}
          />
          <p className="text-muted-foreground type-caption">
            {t("sessionTransitionHint")}
          </p>
        </div>
        <DialogFooter>
          <Button type="button" variant="secondary" onClick={cancel}>
            {t("cancelAction")}
          </Button>
          <Button
            type="button"
            variant="primary"
            disabled={!valid}
            onClick={() => {
              onConfirm({
                turns,
                mode,
                summaryModel: mode === "summary" ? model : "",
                summaryReasoning: mode === "summary" ? selectedReasoning : "",
              });
              setTurns(5);
              setMode("recent");
              setSummaryModel("");
              setSummaryReasoning("");
            }}
          >
            {t("sessionTransitionConfirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
