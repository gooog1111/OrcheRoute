import type { Metadata } from "next";
import { Dashboard } from "./ui/Dashboard";

export const metadata: Metadata = {
  title: "OrcheRoute",
  description: "Управление маршрутизацией и VPN на сервере",
};

export default function Home() {
  return <Dashboard />;
}
