import { createHashRouter } from "react-router-dom";

import { AppShell } from "./layout/app-shell";

export const router = createHashRouter([
  {
    path: "*",
    element: <AppShell />,
  },
]);
