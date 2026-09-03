/// <reference types="vite/client" />

export {};

declare global {
  interface Window {
    // Injected at container startup by server.js (see generateConfig()),
    // not available at build time — never bundle this as a Vite env var.
    API_BASE_URL: string;
  }
}
