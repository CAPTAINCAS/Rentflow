import type { NextConfig } from "next";

// The API base URL is baked into the client bundle at build time, so it must be
// inlined here as well as being available to `next build`.
const apiBase = (process.env.NEXT_PUBLIC_API_BASE_URL ?? "").replace(/\/+$/, "");

const nextConfig: NextConfig = {
  poweredByHeader: false,
  output: "standalone",
  env: {
    NEXT_PUBLIC_API_BASE_URL: apiBase,
    NEXT_PUBLIC_SITE_URL: process.env.NEXT_PUBLIC_SITE_URL ?? "",
  },
};

export default nextConfig;
