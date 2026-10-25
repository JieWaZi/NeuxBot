import { type Dispatch, type ReactNode, type SetStateAction } from "react";
import {
  Navigate,
  Outlet,
  Route,
  Routes,
  useParams,
  useLocation,
} from "react-router-dom";

import { ResourcesPage } from "@/app/pages/resources-page";
import { KnowledgeScene } from "./knowledge-scene";
import {
  PluginDetailPage,
  PluginCatalogDetailPage,
  PluginSkillDetailPage,
  PluginConnectorDetailPage,
} from "@/modules/plugin";
import { WorkBoardPage } from "@/modules/work-board";

import type { ChatRightPanel } from "@/modules/chat";
import {
  AutomaticTaskDetailPage,
  AutomaticTaskListPage,
} from "@/modules/automatic-task";
import { WorkflowDetailPage, WorkflowListPage } from "@/modules/workflow";

import {
  ChatTaskListPage,
  ConnectorDetailPage,
  ProjectDetailPage,
  ProjectListPage,
  SkillDetailPage,
  WakerConnectorsPage,
  WakerDetailPage,
  WakerMemoryPage,
  WakerSkillsPage,
} from "./waker-partner-routes";

import { ChatEntryPage } from "@/modules/chat/pages/chat-entry-page";
import { PartnerManagementPage as WakerListPage } from "@/app/pages/partner-management-page";
import { TeamThreadPage } from "@/app/pages/team-thread-page";
import { TeamThreadListPage } from "@/modules/team/pages/team-thread-list-page";

export type AppRouteOutletContext = {
  chatRightPanel: ChatRightPanel | undefined;
  onTeamThreadDeleted: (channelId: string, threadId: string) => void;
  setChatRightPanel: Dispatch<SetStateAction<ChatRightPanel | undefined>>;
  renderTeamMembers?: (onMemberOpened: () => void) => ReactNode;
};

export type AppRouteSceneProps = {
  location: string;
  outletContext: AppRouteOutletContext;
};

function RouteSceneOutlet({ context }: { context: AppRouteOutletContext }) {
  return <Outlet context={context} />;
}

function LegacyAutomaticTaskRoute() {
  const { wakerId, taskId } = useParams();
  const location = useLocation();
  const params = new URLSearchParams(location.search);
  if (wakerId && !taskId) params.set("wakerId", wakerId);
  return (
    <Navigate
      replace
      to={
        taskId
          ? `/automatic-tasks/${taskId}${location.search}`
          : `/automatic-tasks?${params}`
      }
    />
  );
}

/** 页面随工作台初始化就绪，重型编辑器、文件预览与弹窗在内容区按需加载。 */
export function AppRouteScene({ location, outletContext }: AppRouteSceneProps) {
  return (
    <Routes location={location}>
      <Route element={<RouteSceneOutlet context={outletContext} />}>
        <Route element={<ChatEntryPage />}>
          <Route index element={<></>} />
          <Route path="chat" element={<></>} />
          <Route path="chats/:chatId" element={<></>} />
        </Route>
        <Route path="resources" element={<ResourcesPage />} />
        <Route path="knowledge" element={<KnowledgeScene />} />
        <Route path="knowledge/:libraryId" element={<KnowledgeScene />} />
        <Route
          path="resources/plugins/catalog/:catalogKey"
          element={<PluginCatalogDetailPage />}
        />
        <Route
          path="resources/plugins/:pluginId"
          element={<PluginDetailPage />}
        />
        <Route
          path="resources/plugins/:pluginId/connectors/:connectorKey"
          element={<PluginConnectorDetailPage />}
        />
        <Route
          path="wakers/:wakerId/plugins/:pluginId/connectors/:connectorKey"
          element={<PluginConnectorDetailPage />}
        />
        <Route path="resources/skills/:skillId" element={<SkillDetailPage />} />
        <Route
          path="resources/plugins/:pluginId/skills/:skillKey"
          element={<PluginSkillDetailPage />}
        />
        <Route
          path="resources/connectors/:connectorId"
          element={<ConnectorDetailPage />}
        />
        <Route path="wakers/:wakerId/resources" element={<ResourcesPage />} />
        <Route
          path="wakers/:wakerId/plugins/:pluginId"
          element={<PluginDetailPage />}
        />
        <Route
          path="wakers/:wakerId/plugins/:pluginId/skills/:skillKey"
          element={<PluginSkillDetailPage />}
        />
        <Route path="work-board" element={<WorkBoardPage />} />
        <Route path="wakers" element={<WakerListPage />} />
        <Route path="wakers/:wakerId" element={<WakerDetailPage />} />
        <Route path="workflows" element={<WorkflowListPage />} />
        <Route path="workflows/:workflowId" element={<WorkflowDetailPage />}>
          <Route index element={<></>} />
          <Route path="runs/:runId" element={<></>} />
        </Route>
        <Route path="automatic-tasks" element={<AutomaticTaskListPage />} />
        <Route
          path="automatic-tasks/:taskId"
          element={<AutomaticTaskDetailPage />}
        />
        <Route
          path="wakers/:wakerId/automatic-tasks"
          element={<LegacyAutomaticTaskRoute />}
        />
        <Route
          path="wakers/:wakerId/automatic-tasks/:taskId"
          element={<LegacyAutomaticTaskRoute />}
        />
        <Route path="wakers/:wakerId/projects" element={<ProjectListPage />} />
        <Route
          path="wakers/:wakerId/projects/:projectId"
          element={<ProjectDetailPage />}
        />
        <Route path="wakers/:wakerId/chats" element={<ChatTaskListPage />} />
        <Route path="wakers/:wakerId/memory" element={<WakerMemoryPage />} />
        <Route path="wakers/:wakerId/skills" element={<WakerSkillsPage />} />
        <Route
          path="wakers/:wakerId/skills/:skillId"
          element={<SkillDetailPage />}
        />
        <Route
          path="wakers/:wakerId/connectors"
          element={<WakerConnectorsPage />}
        />
        <Route
          path="wakers/:wakerId/connectors/:connectorId"
          element={<ConnectorDetailPage />}
        />
        <Route
          path="teams"
          element={<Navigate to="/wakers?tab=teams" replace />}
        />
        <Route path="teams/:channelId" element={<TeamThreadPage />}>
          <Route index element={<></>} />
          <Route path="threads/:threadId" element={<></>} />
        </Route>
        <Route path="teams/:channelId/chats" element={<TeamThreadListPage />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  );
}
