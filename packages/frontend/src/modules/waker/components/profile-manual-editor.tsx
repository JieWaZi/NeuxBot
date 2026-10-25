import { ArrowDown, ArrowRight, ArrowUp, Plus, Trash2 } from "lucide-react";

import type { Expertise, WorkMethod } from "../model/types";
import { Button } from "@/shared/ui/button";
import { Input } from "@/shared/ui/input";
import { Textarea } from "@/shared/ui/textarea";

type Props = {
  expertise: Expertise[];
  onExpertiseChange: (items: Expertise[]) => void;
  onWorkMethodsChange: (methods: WorkMethod[]) => void;
  onWorkStyleChange: (items: string[]) => void;
  workMethods: WorkMethod[];
  workStyle: string[];
};

export function ProfileManualEditor({
  expertise,
  onExpertiseChange,
  onWorkMethodsChange,
  onWorkStyleChange,
  workMethods,
  workStyle,
}: Props) {
  return (
    <>
      <ExpertiseEditor items={expertise} onChange={onExpertiseChange} />
      <ListEditor
        title="工作风格"
        description="描述角色稳定的工作偏好，每条填写一种行为特点。"
        placeholder="例如：先澄清目标，再小步迭代"
        items={workStyle}
        onChange={onWorkStyleChange}
      />
      <WorkMethodsEditor methods={workMethods} onChange={onWorkMethodsChange} />
    </>
  );
}

export function cleanManualProfile(
  expertise: Expertise[],
  workStyle: string[],
  workMethods: WorkMethod[],
) {
  return {
    expertise: expertise
      .map((item) => ({
        title: item.title?.trim(),
        description: item.description?.trim(),
      }))
      .filter((item) => item.title),
    workStyle: cleanItems(workStyle),
    workMethods: workMethods
      .map((method) => ({
        name: method.name?.trim(),
        steps: cleanItems(method.steps ?? []),
      }))
      .filter((method) => method.name),
  };
}

function ExpertiseEditor({
  items,
  onChange,
}: {
  items: Expertise[];
  onChange: (items: Expertise[]) => void;
}) {
  const updateItem = (index: number, item: Expertise) => {
    onChange(
      items.map((current, itemIndex) => (itemIndex === index ? item : current)),
    );
  };

  return (
    <section className="border-border rounded-lg border p-4">
      <header className="flex items-center justify-between gap-4">
        <div>
          <h3 className="type-label font-semibold">擅长领域</h3>
          <p className="text-muted-foreground type-caption mt-1">
            每项分别填写能力标题和具体说明，用于概览中的核心职能展示。
          </p>
        </div>
        <Button
          size="sm"
          variant="secondary"
          type="button"
          onClick={() => onChange([...items, { title: "", description: "" }])}
        >
          <Plus />
          添加
        </Button>
      </header>
      <div className="mt-3 grid gap-3">
        {items.length > 0 ? (
          items.map((item, index) => (
            <div key={`expertise-${index}`} className="bg-muted rounded-md p-3">
              <div className="flex items-end gap-2">
                <label className="type-label grid min-w-0 flex-1 gap-2 font-medium">
                  标题
                  <Input
                    placeholder="例如：项目计划"
                    value={item.title ?? ""}
                    onChange={(event) =>
                      updateItem(index, {
                        ...item,
                        title: event.target.value,
                      })
                    }
                  />
                </label>
                <MoveButtons
                  index={index}
                  length={items.length}
                  label="擅长领域"
                  onMove={(nextIndex) =>
                    onChange(moveItem(items, index, nextIndex))
                  }
                />
                <Button
                  size="icon"
                  variant="ghost"
                  type="button"
                  aria-label="删除擅长领域"
                  onClick={() =>
                    onChange(
                      items.filter((_, itemIndex) => itemIndex !== index),
                    )
                  }
                >
                  <Trash2 />
                </Button>
              </div>
              <label className="type-label mt-3 grid gap-2 font-medium">
                描述
                <Textarea
                  size="sm"
                  placeholder="例如：将目标和规格转化为现实可执行的里程碑、任务包、依赖、负责人和验收标准。"
                  value={item.description ?? ""}
                  onChange={(event) =>
                    updateItem(index, {
                      ...item,
                      description: event.target.value,
                    })
                  }
                />
              </label>
            </div>
          ))
        ) : (
          <p className="text-muted-foreground type-body">尚未添加内容。</p>
        )}
      </div>
    </section>
  );
}

function ListEditor({
  description,
  items,
  onChange,
  placeholder,
  title,
}: {
  description: string;
  items: string[];
  onChange: (items: string[]) => void;
  placeholder: string;
  title: string;
}) {
  return (
    <section className="border-border rounded-lg border p-4">
      <header className="flex items-center justify-between gap-4">
        <div>
          <h3 className="type-label font-semibold">{title}</h3>
          <p className="text-muted-foreground type-caption mt-1">
            {description}
          </p>
        </div>
        <Button
          size="sm"
          variant="secondary"
          type="button"
          onClick={() => onChange([...items, ""])}
        >
          <Plus />
          添加
        </Button>
      </header>
      <div className="mt-3 grid gap-2">
        {items.length > 0 ? (
          items.map((item, index) => (
            <div key={`${title}-${index}`} className="flex items-center gap-2">
              <Input
                placeholder={placeholder}
                value={item}
                onChange={(event) =>
                  onChange(
                    items.map((current, itemIndex) =>
                      itemIndex === index ? event.target.value : current,
                    ),
                  )
                }
              />
              <MoveButtons
                index={index}
                length={items.length}
                label={title}
                onMove={(nextIndex) =>
                  onChange(moveItem(items, index, nextIndex))
                }
              />
              <Button
                size="icon"
                variant="ghost"
                type="button"
                aria-label={`删除${title}条目`}
                onClick={() =>
                  onChange(items.filter((_, itemIndex) => itemIndex !== index))
                }
              >
                <Trash2 />
              </Button>
            </div>
          ))
        ) : (
          <p className="text-muted-foreground type-body">尚未添加内容。</p>
        )}
      </div>
    </section>
  );
}

function WorkMethodsEditor({
  methods,
  onChange,
}: {
  methods: WorkMethod[];
  onChange: (methods: WorkMethod[]) => void;
}) {
  const updateMethod = (index: number, method: WorkMethod) => {
    onChange(
      methods.map((current, methodIndex) =>
        methodIndex === index ? method : current,
      ),
    );
  };

  return (
    <section className="border-border rounded-lg border p-4">
      <header className="flex items-center justify-between gap-4">
        <div>
          <h3 className="type-label font-semibold">工作方式</h3>
          <p className="text-muted-foreground type-caption mt-1">
            为常见工作场景定义具名流程，并按执行顺序填写步骤。
          </p>
        </div>
        <Button
          size="sm"
          variant="secondary"
          type="button"
          onClick={() => onChange([...methods, { name: "", steps: [""] }])}
        >
          <Plus />
          添加
        </Button>
      </header>
      <div className="mt-3 grid gap-3">
        {methods.length > 0 ? (
          methods.map((method, index) => (
            <div key={`method-${index}`} className="bg-muted rounded-md p-3">
              <div className="flex items-center gap-2">
                <Input
                  placeholder="流程名称，例如：交付一个新功能"
                  value={method.name ?? ""}
                  onChange={(event) =>
                    updateMethod(index, {
                      ...method,
                      name: event.target.value,
                    })
                  }
                />
                <MoveButtons
                  index={index}
                  length={methods.length}
                  label="工作流程"
                  onMove={(nextIndex) =>
                    onChange(moveItem(methods, index, nextIndex))
                  }
                />
                <Button
                  size="icon"
                  variant="ghost"
                  type="button"
                  aria-label="删除工作流程"
                  onClick={() =>
                    onChange(
                      methods.filter((_, methodIndex) => methodIndex !== index),
                    )
                  }
                >
                  <Trash2 />
                </Button>
              </div>
              <p className="text-muted-foreground type-caption mt-2">
                填写流程名称后，用下方步骤描述完整执行顺序。
              </p>
              <div className="mt-3 flex flex-wrap items-center gap-x-1 gap-y-2">
                {(method.steps ?? []).map((step, stepIndex) => (
                  <div
                    key={`method-${index}-step-${stepIndex}`}
                    className="flex items-center gap-1"
                  >
                    <Input
                      className="w-36"
                      placeholder={`步骤 ${stepIndex + 1}，例如：${stepIndex === 0 ? "理解目标" : "完成产出"}`}
                      value={step}
                      onChange={(event) =>
                        updateMethod(index, {
                          ...method,
                          steps: (method.steps ?? []).map(
                            (current, currentIndex) =>
                              currentIndex === stepIndex
                                ? event.target.value
                                : current,
                          ),
                        })
                      }
                    />
                    <Button
                      size="sm"
                      variant="ghost"
                      type="button"
                      aria-label="删除工作步骤"
                      onClick={() =>
                        updateMethod(index, {
                          ...method,
                          steps: (method.steps ?? []).filter(
                            (_, currentIndex) => currentIndex !== stepIndex,
                          ),
                        })
                      }
                    >
                      删除
                    </Button>
                    <ArrowRight
                      aria-hidden="true"
                      className="text-muted-foreground size-icon-sm"
                      strokeWidth={1.5}
                    />
                  </div>
                ))}
                <Button
                  size="sm"
                  variant="ghost"
                  type="button"
                  onClick={() =>
                    updateMethod(index, {
                      ...method,
                      steps: [...(method.steps ?? []), ""],
                    })
                  }
                >
                  <Plus />
                  添加步骤
                </Button>
              </div>
            </div>
          ))
        ) : (
          <p className="text-muted-foreground type-body">尚未添加工作流程。</p>
        )}
      </div>
    </section>
  );
}

function cleanItems(items: string[]): string[] {
  return items.map((item) => item.trim()).filter(Boolean);
}

function MoveButtons({
  index,
  label,
  length,
  onMove,
}: {
  index: number;
  label: string;
  length: number;
  onMove: (index: number) => void;
}) {
  return (
    <div className="flex">
      <Button
        size="icon"
        variant="ghost"
        type="button"
        aria-label={`上移${label}`}
        disabled={index === 0}
        onClick={() => onMove(index - 1)}
      >
        <ArrowUp />
      </Button>
      <Button
        size="icon"
        variant="ghost"
        type="button"
        aria-label={`下移${label}`}
        disabled={index === length - 1}
        onClick={() => onMove(index + 1)}
      >
        <ArrowDown />
      </Button>
    </div>
  );
}

function moveItem<T>(items: T[], fromIndex: number, toIndex: number): T[] {
  if (toIndex < 0 || toIndex >= items.length) return items;
  const next = [...items];
  const [item] = next.splice(fromIndex, 1);
  if (item === undefined) return items;
  next.splice(toIndex, 0, item);
  return next;
}
