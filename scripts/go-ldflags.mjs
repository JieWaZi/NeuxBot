export const applicationVersionSymbol =
  "github.com/JieWaZi/NeuxBot/server/constants.ApplicationVersion";

const releaseVersionPattern = /^[0-9]+\.[0-9]+\.[0-9]+$/;

export function allyCoreLdflags(version = process.env.ALLY_VERSION ?? "") {
  if (version !== "" && !releaseVersionPattern.test(version)) {
    throw new Error(`ALLY_VERSION 必须是 X.Y.Z，当前为 ${version}`);
  }
  const flags = ["-s", "-w"];
  if (version !== "") {
    flags.push(`-X=${applicationVersionSymbol}=${version}`);
  }
  return flags.join(" ");
}
