/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_ALLY_API_BASE?: string;
  readonly VITE_ALLY_LOCAL_TOKEN?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
