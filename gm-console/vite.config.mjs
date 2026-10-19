import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";
import { readFileSync } from "node:fs";
import { Agent } from "node:https";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  function upstream(target) {
    if (!target) return undefined;
    const origin = new URL(target);
    if (!["127.0.0.1", "localhost", "[::1]"].includes(origin.hostname))
      throw new Error("Development proxy requires a local integration service");
    return {
      target: origin.origin,
      changeOrigin: true,
      secure: true,
      agent:
        origin.protocol === "https:"
          ? new Agent({ ca: readFileSync(env.PURPLEMAGIC_API_CA) })
          : undefined,
      configure(proxy) {
        proxy.on("proxyReq", (req) => {
          req.setHeader("Origin", origin.origin);
        });
        // Browser and service are both loopback in development; production retains secure cookies.
        proxy.on("proxyRes", (res) => {
          if (res.headers["set-cookie"])
            res.headers["set-cookie"] = res.headers["set-cookie"].map((v) =>
              v.replace(/;\s*Secure/gi, ""),
            );
        });
      },
    };
  }
  const api = upstream(env.PURPLEMAGIC_API_ORIGIN),
    hub = upstream(env.PURPLEMAGIC_HUB_ORIGIN || env.PURPLEMAGIC_API_ORIGIN);
  return {
    base: "./",
    optimizeDeps: {
      include: ["react", "react-dom/client"],
    },
    server: {
      host: "127.0.0.1",
      port: 8778,
      strictPort: true,
      proxy: {
        ...(api ? { "/api": api } : {}),
        ...(hub ? { "/admin/api": hub } : {}),
      },
      warmup: {
        clientFiles: ["./src/main.tsx"],
      },
    },
    plugins: [react()],
  };
});
