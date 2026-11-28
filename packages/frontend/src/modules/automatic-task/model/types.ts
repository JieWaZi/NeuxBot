import type { GithubComJieWaZiNeuxBotServerRepositoriesModelsAutomaticTaskConfiguration as Configuration } from "@/shared/api/generated/types.gen";

export type {
  GithubComJieWaZiNeuxBotServerRepositoriesModelsAutomaticTaskConfiguration as TaskConfiguration,
  GithubComJieWaZiNeuxBotServerServicesScheduleTaskView as TaskView,
  GithubComJieWaZiNeuxBotServerServicesScheduleRunView as RunView,
  GithubComJieWaZiNeuxBotServerServicesScheduleSavedTask as SavedTask,
} from "@/shared/api/generated/types.gen";

export type TaskSchedule = NonNullable<Configuration["schedule"]>;
