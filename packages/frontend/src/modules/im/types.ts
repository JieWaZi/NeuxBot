import type { GithubComJieWaZiNeuxBotServerConstantsImPlatform } from "@/shared/api/generated/types.gen";

export type {
  GithubComJieWaZiNeuxBotServerRepositoriesModelsImConnection as IMConnection,
  GithubComJieWaZiNeuxBotServerRepositoriesModelsImBinding as IMBinding,
  GithubComJieWaZiNeuxBotServerServicesImLoginProgress as IMLogin,
  GithubComJieWaZiNeuxBotServerServicesImConnectionConfiguration as ConnectionConfiguration,
} from "@/shared/api/generated/types.gen";

export type IMPlatform = Exclude<
  GithubComJieWaZiNeuxBotServerConstantsImPlatform,
  ""
>;
