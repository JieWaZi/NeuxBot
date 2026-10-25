import js from "@eslint/js";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

const rendererImportPaths = [
  {
    name: "electron",
    message: "渲染进程只能通过 shared/platform 使用 Electron 能力。",
  },
];

const nodeImportPattern = {
  group: ["node:*"],
  message: "渲染进程不得直接使用 Node.js 能力。",
};

const testFiles = ["src/**/*.test.{ts,tsx}", "src/test/**/*.{ts,tsx}"];

export default tseslint.config(
  {
    ignores: [
      "dist",
      "dist-native",
      "release",
      "coverage",
      "src/shared/api/generated",
      "dist-electron",
      "openapi-ts.config.ts",
    ],
  },
  {
    files: ["electron/**/*.mjs", "electron/**/*.{ts,tsx}", "scripts/**/*.mjs"],
    languageOptions: { globals: { ...globals.node, ...globals.es2023 } },
    rules: { "no-restricted-imports": "off" },
  },
  js.configs.recommended,
  ...tseslint.configs.strict,
  reactHooks.configs.flat["recommended-latest"],
  {
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      ecmaVersion: "latest",
      globals: { ...globals.es2023 },
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
    rules: {
      "no-console": "error",
      "@typescript-eslint/no-explicit-any": "error",
      "@typescript-eslint/consistent-type-imports": "error",
    },
  },
  {
    files: ["vite.config.ts", "openapi-ts.config.ts"],
    languageOptions: { globals: { ...globals.node, ...globals.es2023 } },
    rules: { "no-restricted-imports": "off" },
  },
  {
    files: ["electron/shared/contracts.ts"],
    rules: { "@typescript-eslint/no-invalid-void-type": "off" },
  },
  {
    files: ["src/**/*.{ts,tsx}"],
    ignores: testFiles,
    languageOptions: {
      globals: { ...globals.browser, ...globals.es2023 },
    },
    rules: {
      "no-restricted-imports": [
        "error",
        {
          paths: rendererImportPaths,
          patterns: [nodeImportPattern],
        },
      ],
    },
  },
  {
    files: testFiles,
    languageOptions: {
      globals: { ...globals.browser, ...globals.node, ...globals.es2023 },
    },
  },
  {
    files: ["src/shared/**/*.{ts,tsx}"],
    ignores: testFiles,
    rules: {
      "no-restricted-imports": [
        "error",
        {
          paths: rendererImportPaths,
          patterns: [
            nodeImportPattern,
            {
              group: ["@/app/*", "@/modules/*"],
              message: "shared 不得依赖 app 或 modules。",
            },
          ],
        },
      ],
    },
  },
  {
    files: ["src/modules/**/*.{ts,tsx}"],
    ignores: testFiles,
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            nodeImportPattern,
            {
              group: ["@/app/*"],
              message: "modules 不得依赖 app。",
            },
            {
              group: ["@/modules/*/*"],
              message: "模块不得深层导入其他模块。",
            },
          ],
          paths: rendererImportPaths,
        },
      ],
    },
  },
);
