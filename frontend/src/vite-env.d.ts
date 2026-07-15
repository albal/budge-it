/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_CURRENCY?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
