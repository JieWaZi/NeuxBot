/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_NEUXBOT_API_BASE?: string;
  readonly VITE_NEUXBOT_LOCAL_TOKEN?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
