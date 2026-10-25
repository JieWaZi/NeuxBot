import {
  parseReference,
  resolveFileReference,
} from "@/shared/workspace/file-reference";
import { useFileReference } from "@/shared/workspace/file-reference-link";
import { useAui, useAuiState } from "@assistant-ui/react";
import type { Editor } from "@tiptap/core";
import Mention from "@tiptap/extension-mention";
import Placeholder from "@tiptap/extension-placeholder";
import { EditorContent, useEditor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import { type SuggestionOptions } from "@tiptap/suggestion";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useReducer,
  useRef,
  useState,
} from "react";
import { useTranslation } from "react-i18next";
import {
  hasPlatformService,
  searchWorkspaceFiles,
  searchWorkspaceResourceFiles,
  subscribeFileDrop,
} from "@/shared/platform";
import { cn } from "@/shared/lib/cn";
import {
  ChatComposerAttachments,
  type ChatFileOpenRequest,
} from "./chat-attachment";
import type { ChatSkillOption } from "./chat-skill-reference";
import type { ChatConnectorOption } from "./chat-connector-reference";
import type { AllyPathFile } from "../runtime/chat-attachment-adapter";
import type { ChatCodeReference } from "../runtime/chat-queue-store";
import {
  readWorkspaceFileDrag,
  workspaceFileDragType,
} from "../model/workspace-file-drag";
import { toast } from "@/shared/ui/sonner";

type MentionItem = {
  id: string;
  label: string;
  description?: string;
  referenceType: "file" | "skill" | "connector" | "plugin" | "code" | "member";
};
const fileReferenceLimit = 20;

export type ChatMemberMentionOption = {
  description?: string;
  id: string;
  name: string;
};

type ChatComposerInputProps = {
  autoFocus?: boolean;
  placeholder: string;
  ariaLabel: string;
  onValueChange?: () => void;
  className?: string;
  skills?: ChatSkillOption[];
  connectors?: ChatConnectorOption[];
  plugins?: ChatSkillOption[];
  onPluginSelectionChange?: (pluginIds: string[]) => void;
  selectedPluginIds?: string[];
  pluginsError?: boolean;
  fileRoots?: string[];
  fileScopePath?: string;
  fileScopeChatId?: string;
  members?: ChatMemberMentionOption[];
  onSkillSelectionChange?: (skillIds: string[]) => void;
  onConnectorSelectionChange?: (connectorIds: string[]) => void;
  onMemberMentionChange?: (wakerId?: string) => void;
  selectedSkillIds?: string[];
  selectedConnectorIds?: string[];
  skillsError?: boolean;
  connectorsError?: boolean;
  onAttachmentAdd?: (path: string) => Promise<void> | void;
  onAttachmentsRemove?: (paths: string[]) => Promise<void> | void;
  onAttachmentsChange?: () => void;
  onOpenFile?: (file: ChatFileOpenRequest) => void;
  onSend?: () => void;
  codeReferenceRequest?: ChatCodeReference;
  onCodeReferencesChange?: (references: ChatCodeReference[]) => void;
  draft?: string;
  clearRevision?: number;
  restoreRevision?: number;
  disabled?: boolean;
};

function extractText(editor: ReturnType<typeof useEditor>) {
  if (!editor) return "";
  const blocks: string[] = [];
  editor.state.doc.forEach((block) => {
    const parts: string[] = [];
    block.descendants((node) => {
      if (node.type.name === "text") parts.push(node.text ?? "");
      if (node.type.name === "hardBreak") parts.push("\n");
    });
    blocks.push(parts.join(""));
  });
  return blocks.join("\n");
}

async function searchFiles(
  roots: readonly string[],
  query: string,
  scopePath?: string,
  chatId?: string,
): Promise<MentionItem[]> {
  const results = await Promise.all(
    roots.map((root, index) =>
      chatId && hasPlatformService("workspace")
        ? searchWorkspaceResourceFiles(
            chatId,
            query,
            fileReferenceLimit,
            index === 0 ? (scopePath ?? "") : "",
            root,
          )
        : searchWorkspaceFiles(
            root,
            query,
            fileReferenceLimit,
            index === 0 ? scopePath : "",
          ),
    ),
  );
  const candidates = [
    ...new Map(
      results.flatMap((entries, index) =>
        entries.map(
          (entry) =>
            [entry.path, { ...entry, root: roots[index] ?? "" }] as const,
        ),
      ),
    ).values(),
  ]
    .sort((left, right) => left.relativePath.localeCompare(right.relativePath))
    .slice(0, fileReferenceLimit);
  const references = await Promise.all(
    candidates.map(async (entry) => {
      const candidate = parseReference(
        chatId && hasPlatformService("workspace")
          ? entry.relativePath
          : entry.path,
        "path",
      );
      if (candidate?.kind !== "file") return undefined;
      const result = await resolveFileReference(candidate, {
        chatId,
        baseDirectory: entry.root,
        workspaceRoot: roots[0] ?? "",
        sourceRoots: roots.slice(1),
      });
      if (result.status !== "resolved") return undefined;
      return {
        id: result.file.canonicalPath,
        label: result.file.displayPath,
        description: result.file.canonicalPath,
        referenceType: "file" as const,
      };
    }),
  );
  return [
    ...new Map(
      references
        .filter((item) => item !== undefined)
        .map((item) => [item.id, item]),
    ).values(),
  ];
}

function renderSuggestion(
  title: string,
  emptyMessage: () => string,
  loadingMessage?: () => string,
): NonNullable<SuggestionOptions<MentionItem>["render"]> {
  return () => {
    let popup: HTMLDivElement | undefined;
    let selectedIndex = 0;
    let currentItems: MentionItem[] = [];
    let execute: ((item: MentionItem) => void) | undefined;
    let latestProps:
      | {
          editor: Editor;
          items: MentionItem[];
          clientRect?: (() => DOMRect | null) | null;
          command: (item: MentionItem) => void;
          loading?: boolean;
        }
      | undefined;

    const positionPopup = () => {
      if (!popup || !latestProps) return;
      const inputRect = latestProps.editor.view.dom.getBoundingClientRect();
      const caretRect = latestProps.clientRect?.();
      const left = inputRect.left || caretRect?.left || 0;
      popup.style.left = `${Math.max(8, left)}px`;
      popup.style.top = "auto";
      popup.style.bottom = `${Math.max(8, window.innerHeight - inputRect.top + 8)}px`;
      popup.style.width = `${Math.min(400, Math.max(240, inputRect.width || 400))}px`;
    };

    const scrollSelectedIntoView = () => {
      const selectedButton = popup?.querySelectorAll<HTMLButtonElement>(
        'button[role="option"]',
      )[selectedIndex];
      if (
        selectedButton &&
        typeof selectedButton.scrollIntoView === "function"
      ) {
        selectedButton.scrollIntoView({ block: "nearest" });
      }
    };

    const render = (props: {
      editor: Editor;
      items: MentionItem[];
      clientRect?: (() => DOMRect | null) | null;
      command: (item: MentionItem) => void;
      loading?: boolean;
    }) => {
      latestProps = props;
      currentItems = props.items;
      execute = props.command;
      selectedIndex = Math.min(
        selectedIndex,
        Math.max(0, currentItems.length - 1),
      );
      if (!popup) {
        popup = document.createElement("div");
        popup.className =
          "border-border bg-surface-elevated fixed z-50 max-h-80 overflow-y-auto overflow-x-hidden rounded-lg border p-1 shadow-sm";
        popup.setAttribute("role", "listbox");
      }
      popup.replaceChildren();
      popup.setAttribute("aria-busy", String(Boolean(props.loading)));
      const heading = document.createElement("div");
      heading.className =
        "text-muted-foreground type-caption px-2 py-1.5 font-medium";
      heading.textContent = title;
      popup.append(heading);
      if (props.loading && loadingMessage) {
        const loading = document.createElement("div");
        loading.className =
          "text-muted-foreground type-body px-2 py-3 text-center";
        loading.setAttribute("role", "status");
        loading.textContent = loadingMessage();
        popup.append(loading);
      } else if (currentItems.length === 0) {
        const empty = document.createElement("div");
        empty.className =
          "text-muted-foreground type-body px-2 py-3 text-center";
        empty.textContent = emptyMessage();
        popup.append(empty);
      } else {
        currentItems.forEach((item, index) => {
          const button = document.createElement("button");
          button.type = "button";
          button.className =
            "type-body flex min-h-row-rich w-full items-center gap-2 rounded-md px-2 py-1 text-left outline-none";
          if (index === selectedIndex) button.classList.add("bg-muted");
          button.setAttribute("role", "option");
          button.setAttribute("aria-selected", String(index === selectedIndex));
          const text = document.createElement("span");
          text.className = "min-w-0";
          const label = document.createElement("span");
          label.className = "block truncate font-medium";
          label.textContent = item.label;
          text.append(label);
          if (item.description) {
            const description = document.createElement("span");
            description.className =
              "text-muted-foreground type-caption block truncate";
            description.textContent = item.description;
            text.append(description);
          }
          button.append(text);
          button.addEventListener("mousedown", (event) =>
            event.preventDefault(),
          );
          button.addEventListener("click", () => execute?.(item));
          popup?.append(button);
        });
      }
      positionPopup();
      scrollSelectedIntoView();
    };

    const show = (props: NonNullable<typeof latestProps>) => {
      render(props);
      if (popup && !popup.isConnected) {
        document.body.append(popup);
        positionPopup();
      }
    };
    return {
      onBeforeStart: loadingMessage ? show : undefined,
      onBeforeUpdate: loadingMessage ? show : undefined,
      onStart: show,
      onUpdate: render,
      onKeyDown: ({ event }) => {
        if (event.key === "ArrowDown") {
          selectedIndex = Math.min(selectedIndex + 1, currentItems.length - 1);
          if (latestProps) render(latestProps);
          return true;
        }
        if (event.key === "ArrowUp") {
          selectedIndex = Math.max(selectedIndex - 1, 0);
          if (latestProps) render(latestProps);
          return true;
        }
        if (event.key === "Enter" && currentItems[selectedIndex]) {
          const item = currentItems[selectedIndex];
          if (!item) return false;
          execute?.(item);
          return true;
        }
        return event.key === "Escape";
      },
      onExit: () => {
        popup?.remove();
        popup = undefined;
      },
    };
  };
}

/**
 * 使用 TipTap Mention 节点承载文件、Skill 和 MCP 连接器引用。
 * Mention 是原子节点，因此正文与引用共享同一条编辑器文档，不会因 React 列表重排。
 */
export function ChatComposerInput({
  autoFocus = false,
  placeholder,
  ariaLabel,
  onValueChange,
  className,
  skills = [],
  connectors = [],
  plugins = [],
  selectedPluginIds = [],
  onPluginSelectionChange,
  pluginsError = false,
  fileRoots = [],
  fileScopePath,
  fileScopeChatId,
  members = [],
  onSkillSelectionChange,
  onConnectorSelectionChange,
  onMemberMentionChange,
  selectedSkillIds = [],
  selectedConnectorIds = [],
  skillsError = false,
  connectorsError = false,
  onAttachmentAdd,
  onAttachmentsRemove,
  onAttachmentsChange,
  onOpenFile,
  onSend,
  codeReferenceRequest,
  onCodeReferencesChange,
  draft,
  clearRevision = 0,
  restoreRevision = 0,
  disabled = false,
}: ChatComposerInputProps) {
  const { t } = useTranslation("chat");
  const aui = useAui();
  const threadDisabled = useAuiState((state) => state.thread.isDisabled);
  const callbacksRef = useRef({
    aui,
    onAttachmentAdd,
    onAttachmentsRemove,
    onAttachmentsChange,
    onOpenFile,
    onSkillSelectionChange,
    onPluginSelectionChange,
    onConnectorSelectionChange,
    onMemberMentionChange,
    onValueChange,
    onSend,
    onCodeReferencesChange,
  });
  const selectedSkillIdsRef = useRef(selectedSkillIds);
  const selectedConnectorIdsRef = useRef(selectedConnectorIds);
  const skillsRef = useRef(skills);
  const pluginsRef = useRef(plugins);
  const selectedPluginIdsRef = useRef(selectedPluginIds);
  const pluginsErrorRef = useRef(pluginsError);
  const connectorsRef = useRef(connectors);
  const fileRootsRef = useRef(fileRoots);
  const fileScopePathRef = useRef(fileScopePath);
  const fileScopeChatIdRef = useRef(fileScopeChatId);
  const fileSearchFailedRef = useRef(false);
  const membersRef = useRef(members);
  const selectedMemberIdRef = useRef("");
  const isComposingRef = useRef(false);
  const dropTargetRef = useRef<HTMLDivElement>(null);
  const [isDraggingFile, setIsDraggingFile] = useState(false);
  const [, refreshAttachments] = useReducer(
    (revision: number) => revision + 1,
    0,
  );
  const compositionGuardRef = useRef(false);
  const skillsErrorRef = useRef(skillsError);
  const connectorsErrorRef = useRef(connectorsError);
  const disabledRef = useRef(disabled);
  const insertedCodeReferenceIDsRef = useRef(new Set<string>());
  const sentDocumentRef = useRef<ReturnType<Editor["getJSON"]> | undefined>(
    undefined,
  );
  const handledDraftRef = useRef<string | undefined>(draft ? undefined : draft);
  const handledClearRevisionRef = useRef(clearRevision);
  const handledRestoreRevisionRef = useRef(restoreRevision);

  useEffect(() => {
    callbacksRef.current = {
      aui,
      onAttachmentAdd,
      onAttachmentsRemove,
      onAttachmentsChange,
      onOpenFile,
      onSkillSelectionChange,
      onPluginSelectionChange,
      onConnectorSelectionChange,
      onMemberMentionChange,
      onValueChange,
      onSend,
      onCodeReferencesChange,
    };
    selectedSkillIdsRef.current = selectedSkillIds;
    selectedConnectorIdsRef.current = selectedConnectorIds;
    skillsRef.current = skills;
    pluginsRef.current = plugins;
    selectedPluginIdsRef.current = selectedPluginIds;
    pluginsErrorRef.current = pluginsError;
    connectorsRef.current = connectors;
    fileRootsRef.current = fileRoots;
    fileScopePathRef.current = fileScopePath;
    fileScopeChatIdRef.current = fileScopeChatId;
    membersRef.current = members;
    skillsErrorRef.current = skillsError;
    connectorsErrorRef.current = connectorsError;
    disabledRef.current = disabled;
  }, [
    aui,
    plugins,
    selectedPluginIds,
    pluginsError,
    connectors,
    connectorsError,
    disabled,
    fileRoots,
    fileScopePath,
    fileScopeChatId,
    members,
    onAttachmentAdd,
    onAttachmentsRemove,
    onAttachmentsChange,
    onPluginSelectionChange,
    onConnectorSelectionChange,
    onMemberMentionChange,
    onOpenFile,
    onSend,
    onSkillSelectionChange,
    onValueChange,
    onCodeReferencesChange,
    selectedConnectorIds,
    selectedSkillIds,
    skills,
    skillsError,
  ]);

  const notifyAttachmentsChanged = useCallback(() => {
    refreshAttachments();
    callbacksRef.current.onAttachmentsChange?.();
  }, []);

  useEffect(() => {
    return subscribeFileDrop((event) => {
      if (disabled) return;
      const target =
        dropTargetRef.current?.closest<HTMLElement>(
          "[data-chat-composer-surface]",
        ) ?? dropTargetRef.current;
      if (event.type === "leave") {
        setIsDraggingFile(false);
        return;
      }
      if (!target) return;
      const rect = target.getBoundingClientRect();
      const x = event.position.x;
      const y = event.position.y;
      const isInside =
        x >= rect.left && x <= rect.right && y >= rect.top && y <= rect.bottom;
      setIsDraggingFile(isInside && event.type !== "drop");
      if (event.type === "drop" && isInside) {
        // 等页面的 drop 处理结束后再更新附件，避免原生捕获阶段抢先提交状态。
        window.setTimeout(() => {
          void (async () => {
            for (const path of event.paths) {
              await callbacksRef.current.onAttachmentAdd?.(path);
              notifyAttachmentsChanged();
            }
          })();
        }, 0);
      }
    });
  }, [disabled, notifyAttachmentsChanged]);

  const removeComposerAttachment = useCallback(
    async (path: string) => {
      if (!path) return;
      if (callbacksRef.current.onAttachmentsRemove) {
        await callbacksRef.current.onAttachmentsRemove([path]);
      } else {
        const attachments = aui.composer.getState().attachments;
        for (let index = attachments.length - 1; index >= 0; index--) {
          const attachment = attachments[index];
          const sourcePath = (attachment?.file as AllyPathFile | undefined)
            ?.allySourcePath;
          if (sourcePath !== path) continue;
          await aui.composer.attachment({ index }).remove();
        }
      }
    },
    [aui],
  );

  const markCompositionStart = useCallback(() => {
    isComposingRef.current = true;
    compositionGuardRef.current = false;
  }, []);
  const markCompositionEnd = useCallback(() => {
    isComposingRef.current = false;
    compositionGuardRef.current = true;
    window.setTimeout(() => {
      compositionGuardRef.current = false;
    }, 120);
  }, []);

  const selectSkill = useCallback((item: MentionItem) => {
    if (!selectedSkillIdsRef.current.includes(item.id)) {
      callbacksRef.current.onSkillSelectionChange?.([
        ...selectedSkillIdsRef.current,
        item.id,
      ]);
    }
  }, []);

  const selectConnector = useCallback((item: MentionItem) => {
    if (!selectedConnectorIdsRef.current.includes(item.id)) {
      callbacksRef.current.onConnectorSelectionChange?.([
        ...selectedConnectorIdsRef.current,
        item.id,
      ]);
    }
  }, []);
  const selectPlugin = useCallback((item: MentionItem) => {
    if (!selectedPluginIdsRef.current.includes(item.id)) {
      callbacksRef.current.onPluginSelectionChange?.([
        ...selectedPluginIdsRef.current,
        item.id,
      ]);
    }
  }, []);

  const hasMemberSuggestions = members.length > 0;
  const fileSuggestion = useMemo(
    () =>
      ({
        char: "@",
        allowedPrefixes: [" "],
        items: async ({ query }) => {
          const normalizedQuery = query.toLowerCase();
          const memberItems = selectedMemberIdRef.current
            ? []
            : membersRef.current
                .filter((member) =>
                  [member.name, member.description ?? ""].some((value) =>
                    value.toLowerCase().includes(normalizedQuery),
                  ),
                )
                .map((member) => ({
                  id: member.id,
                  label: member.name,
                  description: member.description ?? t("team.member"),
                  referenceType: "member" as const,
                }));
          fileSearchFailedRef.current = false;
          let files: MentionItem[] = [];
          try {
            files = await searchFiles(
              fileRootsRef.current,
              query,
              fileScopePathRef.current,
              fileScopeChatIdRef.current,
            );
          } catch {
            fileSearchFailedRef.current = true;
          }
          return [...memberItems, ...files];
        },
        command: ({ editor, range, props }) => {
          editor
            .chain()
            .focus()
            .insertContentAt(range, {
              type: "mention",
              attrs: {
                id: props.id,
                label: props.label,
                path: props.referenceType === "file" ? props.id : "",
                referenceType: props.referenceType,
                mentionSuggestionChar: "@",
              },
            })
            .run();
        },
        render: renderSuggestion(
          hasMemberSuggestions ? t("team.mentionMenu") : t("fileReferenceMenu"),
          // 菜单渲染器在 React 渲染后才读取最新搜索状态。
          // eslint-disable-next-line react-hooks/refs
          () =>
            hasMemberSuggestions
              ? t("team.mentionEmpty")
              : fileSearchFailedRef.current
                ? t("workspace.filesLoadError")
                : fileRootsRef.current.length === 0
                  ? t("noWorkspaceFiles")
                  : t("noMatchingFiles"),
          () => t("fileReferenceLoading"),
        ),
      }) as SuggestionOptions<MentionItem>,
    [hasMemberSuggestions, t],
  );
  const skillSuggestion = useMemo(
    () =>
      ({
        char: "/",
        allowedPrefixes: [" "],
        items: ({ query }) =>
          [
            ...pluginsRef.current.map((plugin) => ({
              ...plugin,
              plugin: true,
            })),
            ...skillsRef.current.map((skill) => ({ ...skill, plugin: false })),
          ]
            .filter((skill) =>
              [skill.id, skill.name, skill.description].some((value) =>
                value.toLowerCase().includes(query.toLowerCase()),
              ),
            )
            .map((skill) => ({
              id: skill.id,
              label: skill.plugin
                ? `${t("plugin:plugins")} · ${skill.name}`
                : skill.name,
              description: skill.description,
              referenceType: skill.plugin
                ? ("plugin" as const)
                : ("skill" as const),
            })),
        command: ({ editor, range, props }) => {
          editor
            .chain()
            .focus()
            .insertContentAt(range, {
              type: "mention",
              attrs: {
                id: props.id,
                label: props.label,
                referenceType: props.referenceType,
                mentionSuggestionChar: "/",
              },
            })
            .run();
          if (props.referenceType === "plugin") selectPlugin(props);
          else selectSkill(props);
        },
        // The suggestion renderer outlives React renders, so read the latest query state from the ref.
        // eslint-disable-next-line react-hooks/refs
        render: renderSuggestion(t("plugin:title"), () =>
          skillsErrorRef.current ? t("skillsLoadError") : t("noMatchingSkills"),
        ),
      }) as SuggestionOptions<MentionItem>,
    [selectSkill, selectPlugin, t],
  );
  const connectorSuggestion = useMemo(
    () =>
      ({
        char: "#",
        allowedPrefixes: [" "],
        items: ({ query }) =>
          connectorsRef.current
            .filter((connector) =>
              [connector.id, connector.name, connector.description].some(
                (value) => value.toLowerCase().includes(query.toLowerCase()),
              ),
            )
            .map((connector) => ({
              id: connector.id,
              label: connector.name,
              description: connector.description,
              referenceType: "connector" as const,
            })),
        command: ({ editor, range, props }) => {
          editor
            .chain()
            .focus()
            .insertContentAt(range, {
              type: "mention",
              attrs: {
                id: props.id,
                label: props.label,
                referenceType: "connector",
                mentionSuggestionChar: "#",
              },
            })
            .run();
          selectConnector(props);
        },
        // The suggestion renderer outlives React renders, so read the latest query state from the ref.
        // eslint-disable-next-line react-hooks/refs
        render: renderSuggestion(t("connectors"), () =>
          connectorsErrorRef.current
            ? t("connectorsLoadError")
            : t("noMatchingConnectors"),
        ),
      }) as SuggestionOptions<MentionItem>,
    [selectConnector, t],
  );

  const editor = useEditor({
    autofocus: autoFocus ? "end" : false,
    extensions: [
      StarterKit.configure({
        heading: false,
        blockquote: false,
        codeBlock: false,
        horizontalRule: false,
        dropcursor: false,
      }),
      Placeholder.configure({ placeholder }),
      Mention.extend({
        addAttributes() {
          return {
            ...this.parent?.(),
            referenceType: { default: "file" },
            mentionSuggestionChar: { default: "@" },
            path: { default: "" },
            startLine: { default: 0 },
            endLine: { default: 0 },
            content: { default: "" },
          };
        },
        // TipTap keeps these callbacks for the editor lifetime; their refs are refreshed in the effect above.
        // eslint-disable-next-line react-hooks/refs
      }).configure({
        HTMLAttributes: { class: "chat-composer-mention" },
        deleteTriggerWithBackspace: true,
        renderText: ({ node }) => `${node.attrs.label ?? node.attrs.id}`,
        renderHTML: ({ node }) => [
          "span",
          {
            class: cn(
              "bg-action-quiet-hover text-content-primary type-body inline-flex max-w-full items-center rounded-full px-2 py-0.5 font-medium",
              node.attrs.referenceType === "skill" &&
                "bg-primary/12 text-primary",
              node.attrs.referenceType === "connector" &&
                "bg-success/12 text-success",
              node.attrs.referenceType === "code" &&
                "bg-success/12 text-success",
              node.attrs.referenceType === "member" &&
                "bg-primary/12 text-primary",
            ),
            "data-chat-composer-reference": node.attrs.id,
            title:
              node.attrs.referenceType === "file" ||
              node.attrs.referenceType === "code"
                ? node.attrs.path || node.attrs.id
                : undefined,
            "data-chat-composer-reference-type": node.attrs.referenceType,
            "data-chat-composer-skill":
              node.attrs.referenceType === "skill" ? node.attrs.id : undefined,
            "data-chat-composer-plugin":
              node.attrs.referenceType === "plugin" ? node.attrs.id : undefined,
            "data-chat-composer-connector":
              node.attrs.referenceType === "connector"
                ? node.attrs.id
                : undefined,
            "data-chat-composer-code-reference":
              node.attrs.referenceType === "file" ? node.attrs.id : undefined,
            "data-chat-composer-member":
              node.attrs.referenceType === "member" ? node.attrs.id : undefined,
          },
          `${node.attrs.referenceType === "member" ? "@" : ""}${node.attrs.label ?? node.attrs.id}`,
        ],
        suggestions: [fileSuggestion, skillSuggestion, connectorSuggestion],
      }),
    ],
    content: "",
    onCreate: ({ editor: createdEditor }) => {
      Object.defineProperty(createdEditor.view.dom, "value", {
        configurable: true,
        get: () => extractText(createdEditor),
        set: (value: string) => {
          createdEditor.commands.setContent(value);
        },
      });
    },
    editorProps: {
      attributes: {
        role: "textbox",
        "aria-label": ariaLabel,
        "aria-multiline": "true",
        "data-chat-composer-input": "true",
        class: cn(
          "type-label block max-h-40 min-h-16 w-full overflow-x-hidden overflow-y-auto overscroll-contain px-1 py-0 outline-none [overflow-wrap:anywhere]",
          className,
        ),
      },
      handleKeyDown: (view, event) => {
        if (disabledRef.current) return true;
        if (
          event.key !== "Enter" ||
          event.isComposing ||
          isComposingRef.current
        ) {
          return false;
        }
        if (compositionGuardRef.current) {
          event.preventDefault();
          return false;
        }
        if (event.shiftKey) {
          event.preventDefault();
          editor?.commands.splitBlock();
          return true;
        }
        if (view.dom.querySelector("[data-decoration-id]")) return false;
        event.preventDefault();
        if (callbacksRef.current.onSend) {
          callbacksRef.current.onSend();
        } else {
          callbacksRef.current.aui.composer.send();
        }
        return true;
      },
      handleDOMEvents: {
        compositionstart: () => {
          markCompositionStart();
          return false;
        },
        compositionend: () => {
          markCompositionEnd();
          return false;
        },
      },
    },
    onUpdate: ({ editor: nextEditor }) => {
      const text = extractText(nextEditor);
      const codeReferences: ChatCodeReference[] = [];
      const skillIDs = new Set<string>();
      const pluginIDs = new Set<string>();
      const connectorIDs = new Set<string>();
      let memberID = "";
      nextEditor.state.doc.descendants((node) => {
        if (node.type.name !== "mention") return;
        const referenceID = String(node.attrs.id ?? "");
        switch (node.attrs.referenceType) {
          case "file":
            if (referenceID) {
              codeReferences.push({
                id: referenceID,
                path: referenceID,
              });
            }
            break;
          case "skill":
            if (referenceID) skillIDs.add(referenceID);
            break;
          case "plugin":
            if (referenceID) pluginIDs.add(referenceID);
            break;
          case "connector":
            if (referenceID) connectorIDs.add(referenceID);
            break;
          case "code":
            codeReferences.push({
              id: referenceID || crypto.randomUUID(),
              path: String(node.attrs.path),
              startLine: Number(node.attrs.startLine),
              endLine: Number(node.attrs.endLine),
              content: String(node.attrs.content),
            });
            break;
          case "member":
            if (!memberID && referenceID) memberID = referenceID;
            break;
        }
      });
      selectedMemberIdRef.current = memberID;
      nextEditor.view.dom.setAttribute("value", text);
      callbacksRef.current.aui.composer.setText(
        text || (codeReferences.length ? t("codeReferenceOnlyMessage") : ""),
      );
      callbacksRef.current.onPluginSelectionChange?.(Array.from(pluginIDs));
      callbacksRef.current.onSkillSelectionChange?.(Array.from(skillIDs));
      callbacksRef.current.onConnectorSelectionChange?.(
        Array.from(connectorIDs),
      );
      callbacksRef.current.onCodeReferencesChange?.(codeReferences);
      callbacksRef.current.onMemberMentionChange?.(memberID || undefined);
      callbacksRef.current.onValueChange?.();
    },
  });

  useLayoutEffect(() => {
    if (
      !editor ||
      editor.isDestroyed ||
      draft === undefined ||
      handledDraftRef.current === draft
    )
      return;
    const applyDraft = () => {
      if (editor.isDestroyed) return;
      handledDraftRef.current = draft;
      // 外部建议也是编辑器文档，由 onUpdate 同步普通 Chat 的发送状态。
      editor.commands.setContent({
        type: "doc",
        content: [
          {
            type: "paragraph",
            content: draft ? [{ type: "text", text: draft }] : [],
          },
        ],
      });
    };
    if (editor.isInitialized) applyDraft();
    else editor.on("create", applyDraft);
    return () => {
      editor.off("create", applyDraft);
    };
  }, [draft, editor]);

  useLayoutEffect(() => {
    if (!editor || handledClearRevisionRef.current === clearRevision) return;
    handledClearRevisionRef.current = clearRevision;
    sentDocumentRef.current = editor.getJSON();
    editor.commands.clearContent();
  }, [clearRevision, editor]);

  useLayoutEffect(() => {
    if (!editor || handledRestoreRevisionRef.current === restoreRevision)
      return;
    handledRestoreRevisionRef.current = restoreRevision;
    const sentDocument = sentDocumentRef.current;
    sentDocumentRef.current = undefined;
    if (sentDocument) editor.commands.setContent(sentDocument);
  }, [editor, restoreRevision]);

  const codeReferenceCandidate = parseReference(
    codeReferenceRequest?.path ?? "",
    "path",
  );
  const codeReferenceQuery = useFileReference(
    codeReferenceCandidate?.kind === "file"
      ? codeReferenceCandidate
      : undefined,
    {
      chatId: fileScopeChatId,
      baseDirectory: fileRoots[0] ?? "",
      workspaceRoot: fileRoots[0] ?? "",
      sourceRoots: fileRoots.slice(1),
    },
  );
  const codeReferenceFile =
    codeReferenceQuery.data?.status === "resolved"
      ? codeReferenceQuery.data.file
      : undefined;
  useEffect(() => {
    if (!editor || !codeReferenceRequest || codeReferenceQuery.isFetching)
      return;
    const referenceID = codeReferenceRequest.id ?? "";
    if (referenceID && insertedCodeReferenceIDsRef.current.has(referenceID)) {
      return;
    }
    if (referenceID) insertedCodeReferenceIDsRef.current.add(referenceID);
    const label = `${codeReferenceFile?.displayPath ?? codeReferenceRequest.path} (${codeReferenceRequest.startLine}-${codeReferenceRequest.endLine})`;
    editor
      .chain()
      .focus()
      .insertContent({
        type: "mention",
        attrs: {
          ...codeReferenceRequest,
          label,
          referenceType: "code",
          mentionSuggestionChar: "",
        },
      })
      .run();
  }, [
    codeReferenceRequest,
    codeReferenceFile,
    codeReferenceQuery.isFetching,
    editor,
  ]);

  useEffect(() => {
    if (!editor) return;
    editor.setEditable(!disabled && !threadDisabled);
  }, [disabled, editor, threadDisabled]);

  if (!editor) {
    return (
      <div
        aria-busy="true"
        aria-label={ariaLabel}
        aria-readonly="true"
        className={cn(
          "text-muted-foreground type-body h-control-lg w-full cursor-text px-1 py-2",
          className,
        )}
        role="textbox"
      >
        {placeholder}
      </div>
    );
  }
  return (
    <div
      ref={dropTargetRef}
      data-chat-composer-dropzone
      data-file-dragging={isDraggingFile}
      className="w-full"
      onPasteCapture={(event) => {
        const files = Array.from(event.clipboardData.files);
        if (!files.length) return;
        event.preventDefault();
        event.stopPropagation();
        if (disabled || aui.thread.getState().isDisabled) return;
        const composer = aui.composer;
        void (async () => {
          for (const file of files) {
            try {
              await composer.addAttachment(file);
              notifyAttachmentsChanged();
            } catch {
              toast.error(t("attachmentFailed"));
            }
          }
        })();
      }}
      onDragOverCapture={(event) => {
        if (event.dataTransfer.types.includes(workspaceFileDragType)) {
          event.preventDefault();
          event.stopPropagation();
          event.dataTransfer.dropEffect = "copy";
          return;
        }
        if (!event.dataTransfer.types.includes("Files")) return;
        event.preventDefault();
        event.stopPropagation();
      }}
      onDropCapture={(event) => {
        if (event.dataTransfer.types.includes(workspaceFileDragType)) {
          event.preventDefault();
          event.stopPropagation();
          const dropped = readWorkspaceFileDrag(
            event.dataTransfer.getData(workspaceFileDragType),
          );
          if (
            disabled ||
            !dropped ||
            dropped.chatId !== fileScopeChatIdRef.current ||
            !fileRootsRef.current.includes(dropped.rootPath)
          )
            return;
          const candidate = parseReference(dropped.path, "path");
          if (candidate?.kind !== "file") return;
          void resolveFileReference(candidate, {
            chatId: fileScopeChatIdRef.current,
            baseDirectory: dropped.rootPath,
            workspaceRoot: fileRootsRef.current[0] ?? "",
            sourceRoots: fileRootsRef.current.slice(1),
          })
            .then((result) => {
              if (result.status !== "resolved") {
                toast.error(t("fileReference.unavailable"));
                return;
              }
              editor
                .chain()
                .focus()
                .insertContent({
                  type: "mention",
                  attrs: {
                    id: result.file.canonicalPath,
                    label: result.file.displayPath,
                    path: result.file.canonicalPath,
                    referenceType: "file",
                    mentionSuggestionChar: "@",
                  },
                })
                .run();
            })
            .catch(() => toast.error(t("fileReference.unavailable")));
          return;
        }
        if (!event.dataTransfer.types.includes("Files")) return;
        event.preventDefault();
        event.stopPropagation();
      }}
    >
      <div className="flex flex-wrap gap-1 pb-2">
        <ChatComposerAttachments
          onOpenFile={onOpenFile}
          onRemove={async (path) => {
            await removeComposerAttachment(path);
            // 附件运行时的订阅状态会在删除 Promise 结束后的事件轮次更新。
            window.setTimeout(notifyAttachmentsChanged, 0);
          }}
        />
      </div>
      <EditorContent
        editor={editor}
        className="w-full overflow-hidden"
        onCompositionStart={markCompositionStart}
        onCompositionEnd={markCompositionEnd}
        onKeyDown={(event) => {
          if (event.key !== "Enter") return;
          if (
            event.nativeEvent.isComposing ||
            isComposingRef.current ||
            compositionGuardRef.current
          ) {
            event.preventDefault();
          }
        }}
        onChange={(event) => {
          const target = event.target as HTMLDivElement & { value?: string };
          const value = target.value;
          if (typeof value === "string" && value !== extractText(editor)) {
            editor.commands.setContent(value);
          }
        }}
      />
    </div>
  );
}
