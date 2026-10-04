import { StrictMode, useSyncExternalStore } from "react";
import { createRoot } from "react-dom/client";
import { SWRConfig } from "swr";
import { NextIntlClientProvider } from "./intl";
import en from "../messages/en.json";
import de from "../messages/de.json";
import { nativeMessages } from "./messages";
import { sessionVersion, hasSession } from "./api";
import { usePathname } from "./navigation";
import { getThemeBootstrapScript } from "../src/lib/theme-script";
import { DashboardClientLayout } from "../src/components/dashboard-client-layout";
import Login from "../src/app/login/page";
import Overview from "../src/app/dashboard/page";
import Providers from "../src/app/dashboard/providers/page";
import Keys from "../src/app/dashboard/api-keys/page";
import Quota from "../src/app/dashboard/quota/page";
import Usage from "../src/app/dashboard/usage/page";
import Monitoring from "../src/app/dashboard/monitoring/page";
import Logs from "../src/app/dashboard/logs/page";
import { Operations, NativeSettings } from "./operations";
import "../src/app/globals.css";
import "./fonts.css";
import icon from "../src/app/icon.png?inline";

const favicon = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
if (favicon) favicon.href = icon;

const bootstrap = document.createElement("script");
bootstrap.textContent = getThemeBootstrapScript(); document.head.appendChild(bootstrap); bootstrap.remove();
const subscribe = (listener: () => void) => {
  window.addEventListener("dashboard:session", listener);
  window.addEventListener("dashboard:refresh", listener);
  return () => { window.removeEventListener("dashboard:session", listener); window.removeEventListener("dashboard:refresh", listener); };
};
const snapshot = () => `${sessionVersion()}:${localStorage.getItem("dashboard.locale") === "de" ? "de" : "en"}`;
function App() {
  const version = useSyncExternalStore(subscribe, snapshot);
  const path = usePathname();
  const locale = version.endsWith(":de") ? "de" : "en";
  const original = locale === "de" ? de : en;
  const messages = { ...original, native: nativeMessages[locale], nav: { ...original.nav, nativeOperations: nativeMessages[locale].operations }, auth: { ...original.auth, passwordLabel: locale === "de" ? "Admin-Zugangsschlüssel" : "Admin credential" } };
  const page = path === "/dashboard/providers" ? <Providers /> : path === "/dashboard/api-keys" ? <Keys /> : path === "/dashboard/quota" ? <Quota /> : path === "/dashboard/usage" ? <Usage /> : path === "/dashboard/monitoring" ? <Monitoring /> : path === "/dashboard/logs" ? <Logs /> : path === "/dashboard/settings" ? <NativeSettings /> : path === "/dashboard/operations" ? <Operations /> : <Overview />;
  return <NextIntlClientProvider locale={locale} messages={messages} timeZone="UTC">
    <SWRConfig key={version} value={{ provider: () => new Map(), revalidateOnFocus: false }}>
      {hasSession() ? <DashboardClientLayout><div key={path}>{page}</div></DashboardClientLayout> : <Login />}
    </SWRConfig>
  </NextIntlClientProvider>;
}
const root = document.getElementById("root");
if (!root) throw new Error("Dashboard root missing");
createRoot(root).render(<StrictMode><App /></StrictMode>);
