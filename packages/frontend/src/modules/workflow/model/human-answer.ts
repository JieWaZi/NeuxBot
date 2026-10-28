import { record, type HumanRequest } from "./types";

/** 冻结候选只用于阅读标签；原运行答案与自定义值保持原样。 */
export function humanAnswerLabel(
  value: unknown,
  human?: HumanRequest,
): unknown {
  const kind = human?.options?.kind;
  const choices = human?.options?.choices;
  if ((kind !== "select" && kind !== "multiselect") || !Array.isArray(choices))
    return value;
  const labels = new Map(
    choices.flatMap((item) => {
      const choice = record(item);
      return typeof choice?.value === "string" &&
        typeof choice.label === "string"
        ? [[choice.value, choice.label] as const]
        : [];
    }),
  );
  const display = (answer: unknown) =>
    typeof answer === "string" ? (labels.get(answer) ?? answer) : answer;
  return kind === "multiselect" && Array.isArray(value)
    ? value.map(display)
    : display(value);
}
