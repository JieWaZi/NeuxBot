import { render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { memo } from "react";
import { describe, expect, it, vi } from "vitest";
import { FileReferenceProvider, useFileReference } from "./file-reference-link";
import { emptyFileReferenceContext } from "./file-reference";

describe("file reference provider render isolation", () => {
  it("does not notify unchanged reference consumers on a parent rerender", () => {
    const renderConsumer = vi.fn();
    const Consumer = memo(function Consumer() {
      useFileReference(undefined);
      renderConsumer();
      return null;
    });
    const client = new QueryClient();
    const view = () => (
      <QueryClientProvider client={client}>
        <FileReferenceProvider context={emptyFileReferenceContext}>
          <Consumer />
        </FileReferenceProvider>
      </QueryClientProvider>
    );
    const { rerender, unmount } = render(view());
    renderConsumer.mockClear();
    rerender(view());
    expect(renderConsumer).not.toHaveBeenCalled();
    unmount();
    client.clear();
  });
});
