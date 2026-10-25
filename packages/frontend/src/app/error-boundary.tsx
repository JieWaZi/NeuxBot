import { Component, type ReactNode } from "react";

import { Button } from "@/shared/ui/button";
import { logger } from "@/shared/diagnostics/logger";

type Props = { children: ReactNode };
type State = { error?: Error };

export class ErrorBoundary extends Component<Props, State> {
  state: State = {};

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error): void {
    logger.error("React 页面渲染失败", {
      errorName: error.name,
      message: error.message,
    });
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <main className="grid h-full place-items-center p-8">
        <section className="border-border-subtle bg-surface-raised max-w-md rounded-xl border p-8 text-center">
          <h1 className="type-page-title font-semibold">
            当前页面暂时无法显示
          </h1>
          <p className="text-muted-foreground type-body mt-3">
            {this.state.error.message}
          </p>
          <Button className="mt-6" onClick={() => window.location.reload()}>
            重新载入
          </Button>
        </section>
      </main>
    );
  }
}
