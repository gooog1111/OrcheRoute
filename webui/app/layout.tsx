import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "OrcheRoute",
  description: "Управление маршрутами, списками VPN-серверов и DNS",
  icons: { icon: "/favicon.svg" },
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="ru">
      <body>{children}</body>
    </html>
  );
}
