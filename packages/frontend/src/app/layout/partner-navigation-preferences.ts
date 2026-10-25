const partnerNavigationStorageKey = "ally.partner-navigation";

export type PartnerNavigationPreferences = {
  collapsedGroups: Record<string, string[]>;
  contextSidebarWidth: number;
  currentPartnerId: string;
  currentRailKey: string;
  entityMode: "team" | "waker";
  globalSidebarCollapsed: boolean;
  lastTeamThreads: Record<string, string>;
  railExpanded: boolean;
  railOrder: string[];
};

const defaultPreferences: PartnerNavigationPreferences = {
  collapsedGroups: {},
  contextSidebarWidth: 240,
  currentPartnerId: "",
  currentRailKey: "",
  entityMode: "waker",
  globalSidebarCollapsed: false,
  lastTeamThreads: {},
  railExpanded: false,
  railOrder: [],
};

export const contextSidebarWidthRange = {
  max: 360,
  min: 216,
} as const;

export function clampContextSidebarWidth(value: number): number {
  if (!Number.isFinite(value)) return defaultPreferences.contextSidebarWidth;
  return Math.min(
    contextSidebarWidthRange.max,
    Math.max(contextSidebarWidthRange.min, Math.round(value)),
  );
}

function stringArray(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return Array.from(
    new Set(
      value.filter(
        (item): item is string => typeof item === "string" && Boolean(item),
      ),
    ),
  );
}

function stringArrayRecord(value: unknown): Record<string, string[]> {
  if (!value || typeof value !== "object") return {};
  return Object.fromEntries(
    Object.entries(value).flatMap(([key, items]) => {
      const normalized = stringArray(items);
      return key && normalized.length > 0 ? [[key, normalized]] : [];
    }),
  );
}

function stringRecord(value: unknown): Record<string, string> {
  if (!value || typeof value !== "object") return {};
  return Object.fromEntries(
    Object.entries(value).flatMap(([key, item]) =>
      key && typeof item === "string" && item ? [[key, item]] : [],
    ),
  );
}

export function readPartnerNavigationPreferences(): PartnerNavigationPreferences {
  if (typeof window === "undefined") return defaultPreferences;
  try {
    const raw = window.localStorage.getItem(partnerNavigationStorageKey);
    if (!raw) return defaultPreferences;
    const value: unknown = JSON.parse(raw);
    if (!value || typeof value !== "object") return defaultPreferences;
    const currentPartnerId =
      "currentPartnerId" in value && typeof value.currentPartnerId === "string"
        ? value.currentPartnerId
        : "";
    return {
      collapsedGroups:
        "collapsedGroups" in value
          ? stringArrayRecord(value.collapsedGroups)
          : {},
      contextSidebarWidth:
        "contextSidebarWidth" in value &&
        typeof value.contextSidebarWidth === "number"
          ? clampContextSidebarWidth(value.contextSidebarWidth)
          : defaultPreferences.contextSidebarWidth,
      currentPartnerId,
      currentRailKey:
        "currentRailKey" in value && typeof value.currentRailKey === "string"
          ? value.currentRailKey
          : currentPartnerId
            ? `waker:${currentPartnerId}`
            : "",
      entityMode:
        "entityMode" in value && value.entityMode === "team" ? "team" : "waker",
      globalSidebarCollapsed:
        "globalSidebarCollapsed" in value
          ? value.globalSidebarCollapsed === true
          : "sidebarCollapsed" in value && value.sidebarCollapsed === true,
      lastTeamThreads:
        "lastTeamThreads" in value ? stringRecord(value.lastTeamThreads) : {},
      railExpanded: "railExpanded" in value && value.railExpanded === true,
      railOrder: "railOrder" in value ? stringArray(value.railOrder) : [],
    };
  } catch {
    return defaultPreferences;
  }
}

export function rememberTeamThread(
  preferences: PartnerNavigationPreferences,
  channelId: string,
  threadId: string,
): PartnerNavigationPreferences {
  if (!channelId || !threadId) return preferences;
  if (preferences.lastTeamThreads[channelId] === threadId) return preferences;
  return {
    ...preferences,
    lastTeamThreads: {
      ...preferences.lastTeamThreads,
      [channelId]: threadId,
    },
  };
}

export function forgetTeamThread(
  preferences: PartnerNavigationPreferences,
  channelId: string,
  threadId: string,
): PartnerNavigationPreferences {
  if (preferences.lastTeamThreads[channelId] !== threadId) return preferences;
  const next = Object.fromEntries(
    Object.entries(preferences.lastTeamThreads).filter(
      ([storedChannelId]) => storedChannelId !== channelId,
    ),
  );
  return { ...preferences, lastTeamThreads: next };
}

export function writePartnerNavigationPreferences(
  preferences: PartnerNavigationPreferences,
) {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(
      partnerNavigationStorageKey,
      JSON.stringify(preferences),
    );
  } catch {
    // 本地偏好不可用时不阻断伙伴导航。
  }
}

export function reconcilePartnerOrder(
  availablePartnerIds: readonly string[],
  preferredPartnerIds: readonly string[],
): string[] {
  const available = new Set(availablePartnerIds);
  const ordered = preferredPartnerIds.filter((id) => available.has(id));
  const known = new Set(ordered);
  for (const id of availablePartnerIds) {
    if (!known.has(id)) ordered.push(id);
  }
  return ordered;
}

export function collapsedPartnerIds(
  orderedPartnerIds: readonly string[],
  currentPartnerId: string,
  limit = 5,
): string[] {
  if (orderedPartnerIds.length <= limit) return [...orderedPartnerIds];
  const visible = orderedPartnerIds.slice(0, limit);
  if (!currentPartnerId || visible.includes(currentPartnerId)) return visible;
  return [...visible.slice(0, limit - 1), currentPartnerId];
}

export function moveItem(
  items: readonly string[],
  sourceId: string,
  targetId: string,
): string[] {
  const sourceIndex = items.indexOf(sourceId);
  const targetIndex = items.indexOf(targetId);
  if (sourceIndex < 0 || targetIndex < 0 || sourceIndex === targetIndex) {
    return [...items];
  }
  const next = [...items];
  next.splice(sourceIndex, 1);
  next.splice(targetIndex, 0, sourceId);
  return next;
}
