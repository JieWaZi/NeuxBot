import { describe, expect, it } from "vitest";

import styles from "./index.css?raw";

describe("共享控件密度合同", () => {
  it("使用桌面紧凑尺寸阶梯", () => {
    expect(styles).toContain("--spacing-button-sm: 1.75rem;");
    expect(styles).toContain("--spacing-button-md: 2rem;");
    expect(styles).toContain("--spacing-button-lg: 2.25rem;");
    expect(styles).toContain("--spacing-control-sm: 1.75rem;");
    expect(styles).toContain("--spacing-control-md: 2.25rem;");
    expect(styles).toContain("--spacing-control-lg: 2.75rem;");
    expect(styles).toContain("--spacing-control-search-sm: 1.875rem;");
    expect(styles).toContain("--spacing-control-hit-target: 2.25rem;");
  });

  it("文字按钮和导航控件使用克制的语义圆角", () => {
    expect(styles).toContain("--radius-control: 0.75rem;");
    expect(styles).toContain("--radius-control-inner: 0.5rem;");
  });

  it("为列表工具栏提供稳定的语义宽度", () => {
    expect(styles).toContain("--spacing-field-filter: 8rem;");
    expect(styles).toContain("--spacing-field-filter-min: 6rem;");
    expect(styles).toContain("--spacing-field-date-range: 15rem;");
    expect(styles).toContain("--spacing-field-search: 18rem;");
    expect(styles).toContain("--spacing-field-search-min: 10rem;");
    expect(styles).toContain("--spacing-field-form-compact: 20rem;");
  });

  it("根容器按实际窗口尺寸参与响应式布局", () => {
    const rootRule = styles.match(/html,\s*body,\s*#root\s*\{([^}]*)\}/)?.[1];

    expect(rootRule).toBeDefined();
    expect(rootRule).toContain("width: 100%;");
    expect(rootRule).toContain("min-width: 0;");
    expect(rootRule).toContain("min-height: 0;");
  });
});
