import { defineConfig } from "@hey-api/openapi-ts";

export default defineConfig({
  input: "../../server/openapi.json",
  output: {
    path: "src/shared/api/generated",
    clean: true,
  },
  client: "@hey-api/client-fetch",
  plugins: ["@hey-api/typescript", "@hey-api/sdk", "@tanstack/react-query"],
});
