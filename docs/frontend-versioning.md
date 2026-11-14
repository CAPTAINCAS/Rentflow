# Frontend framework versioning

The frontend is pinned to the patched Next.js 16.4 line rather than the initially staged 15.3.3. Vercel's November 2026 security release lists Next.js 16.4.5 as Active LTS and Next.js 15.5.29 as Maintenance LTS, and recommends patching to those versions. The source reports cache cross-tenant disclosure findings in Cache Components and other medium-severity issues; this application should not enable experimental/shared cache behavior for account-specific data. [1]

The official Next.js 16.4 release notes identify React 19.3 as the included React version. Package pins and lockfile must use a compatible React/React DOM 19.3 release, Node 22, and pnpm 10.18.0. [2]

## References

[1]: https://nextjs.org/blog/november-2026-security-release "Next.js November 2026 Security Release"
[2]: https://nextjs.org/blog/next-16-4 "Next.js 16.4 release notes"
