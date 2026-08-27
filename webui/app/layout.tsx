import type { Metadata } from "next";
import "./globals.css";
import { releaseBranding } from "./platform/release";
import { themeIDs, themeStorageKey } from "./ui/theme";

const themeBootstrap = `(()=>{try{const value=localStorage.getItem(${JSON.stringify(themeStorageKey)});if(${JSON.stringify(themeIDs)}.includes(value))document.documentElement.dataset.theme=value}catch{}})()`;

export const metadata: Metadata = {
  title: "OrcheRoute",
  description: "Управление маршрутами, списками VPN-серверов и DNS",
  icons: { icon: releaseBranding.prerelease ? "/favicon-beta.svg" : "/favicon.svg" },
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="ru" data-release-channel={releaseBranding.channel} data-theme="matrix" suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: themeBootstrap }} />
      </head>
      <body>{children}</body>
    </html>
  );
}
