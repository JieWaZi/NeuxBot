import { startApplication } from "@neuxbot/frontend/start";

const { configureDesktopPlatform } = await import("./platform");
configureDesktopPlatform();
startApplication();
