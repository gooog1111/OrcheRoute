import type { Metadata } from "next";
import "./globals.css";
import { releaseBranding } from "./platform/release";

export const metadata: Metadata = {
  title: "OrcheRoute",
  description: "Управление маршрутами, списками VPN-серверов и DNS",
  icons: { icon: releaseBranding.prerelease ? "/favicon-beta.svg" : "/favicon.svg" },
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="ru" data-release-channel={releaseBranding.channel} data-theme="matrix">
      <body>{children}</body>
    </html>
  );
}
