import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  base: "./",
  server: {
    port: 15173,
    strictPort: false,
    // Bind every interface so a phone or tablet on the LAN can load the theme.
    host: true,
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    assetsDir: "assets",
    assetsInlineLimit: 100000,
    cssCodeSplit: false,
    rollupOptions: {
      output: {
        manualChunks: undefined,
        inlineDynamicImports: true,
      },
    },
  },
});
