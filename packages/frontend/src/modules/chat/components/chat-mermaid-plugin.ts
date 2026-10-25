import type { DiagramPlugin } from "@streamdown/mermaid";

/** 保留 Streamdown 的图表协议，仅在实际绘图时加载 Mermaid 引擎。 */
export const mermaid: DiagramPlugin = {
  name: "mermaid",
  type: "diagram",
  language: "mermaid",
  getMermaid(config) {
    let currentConfig = config;
    return {
      initialize(nextConfig) {
        currentConfig = nextConfig;
      },
      async render(id, source) {
        const { mermaid } = await import("@streamdown/mermaid");
        return mermaid.getMermaid(currentConfig).render(id, source);
      },
    };
  },
};
