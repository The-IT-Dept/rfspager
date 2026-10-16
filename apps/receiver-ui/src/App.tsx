import { useEffect, type ComponentType } from "react";
import { Header } from "./components/header";
import { Toaster } from "./components/toast";
import { hub } from "./lib/events";
import { cn } from "./lib/format";
import { useHashRoute } from "./lib/hooks";
import { StatusProvider, useStatus } from "./lib/status";
import { ConnectionsTab } from "./tabs/connections";
import { LogsTab } from "./tabs/logs";
import { OverviewTab } from "./tabs/overview";
import { PagesTab } from "./tabs/pages";
import { SettingsTab } from "./tabs/settings";
import { StatsTab } from "./tabs/stats";

interface TabDef {
  path: string;
  label: string;
  Component: ComponentType;
}

const TABS: TabDef[] = [
  { path: "/", label: "Overview", Component: OverviewTab },
  { path: "/pages", label: "Pages", Component: PagesTab },
  { path: "/stats", label: "Stats", Component: StatsTab },
  { path: "/connections", label: "Connections", Component: ConnectionsTab },
  { path: "/logs", label: "Logs", Component: LogsTab },
  { path: "/settings", label: "Settings", Component: SettingsTab },
];

export function App() {
  useEffect(() => hub.start(), []);
  return (
    <StatusProvider>
      <Shell />
      <Toaster />
    </StatusProvider>
  );
}

function Shell() {
  const route = useHashRoute();
  const tab = TABS.find((t) => t.path === route) ?? TABS[0];
  const { status } = useStatus();

  useEffect(() => {
    const name = status?.system?.name;
    document.title = `${tab.label} · ${name ? `${name} receiver` : "RFSPager receiver"}`;
  }, [tab, status?.system?.name]);

  return (
    <div className="flex min-h-dvh flex-col">
      <Header />
      <nav className="sticky top-0 z-20 border-b border-border bg-white/95 backdrop-blur" aria-label="Sections">
        <div className="scrollbar-none mx-auto flex max-w-7xl gap-5 overflow-x-auto px-4">
          {TABS.map((t) => {
            const on = t === tab;
            return (
              <a
                key={t.path}
                href={`#${t.path}`}
                aria-current={on ? "page" : undefined}
                className={cn(
                  "-mb-px shrink-0 border-b-2 py-2.5 text-[12.5px] transition-colors",
                  on
                    ? "border-brand font-semibold text-foreground"
                    : "border-transparent text-muted-foreground hover:text-foreground",
                )}
              >
                {t.label}
              </a>
            );
          })}
        </div>
      </nav>
      <main className="mx-auto w-full max-w-7xl flex-1 px-4 pt-3 pb-10">
        <tab.Component key={tab.path} />
      </main>
    </div>
  );
}
