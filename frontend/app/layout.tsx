import type { Metadata, Viewport } from "next";
import { Providers } from "@/components/providers";
import { AppShell } from "@/components/app-shell";
import "./globals.css";

export const metadata: Metadata = { title: { default: "RentFlow · Property operations", template: "%s · RentFlow" }, description: "Clear, steady rent and property operations.", manifest: "/manifest.webmanifest", icons: [{ rel: "icon", url: "/icon.svg", type: "image/svg+xml" }, { rel: "icon", url: "/icon-192.png", sizes: "192x192", type: "image/png" }, { rel: "icon", url: "/icon-512.png", sizes: "512x512", type: "image/png" }] };
export const viewport: Viewport = { themeColor: "#174d3d", width: "device-width", initialScale: 1 };
function ServiceWorker(){return <script dangerouslySetInnerHTML={{__html:"if ('serviceWorker' in navigator) { window.addEventListener('load', () => navigator.serviceWorker.register('/sw.js').catch(() => {})); }"}}/>;}
export default function RootLayout({children}:{children:React.ReactNode}){return <html lang="en"><body><Providers><AppShell>{children}</AppShell></Providers><ServiceWorker/></body></html>;}
