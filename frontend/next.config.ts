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
  async rewrites() {
    // Same-origin proxy: /api/v1/* → Render backend
    // Keeps session cookies first-party (browser sees Vercel domain)
    const apiOrigin = process.env.API_ORIGIN;
    if (!apiOrigin) return [];
    return [{ source: "/api/v1/:path*", destination: `${apiOrigin}/api/v1/:path*` }];
  },
};

export default nextConfig;
