import { useState, useEffect } from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "~/components/ui/card";
import { Badge } from "~/components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import { api } from "~/lib/api";
import {
  Users,
  CreditCard,
  UserX,
  UserPlus,
  MessageSquare,
  MessageSquarePlus,
  Tag,
  Server,
  Smartphone,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

interface Stats {
  total_users: number;
  paid_users: number;
  free_users: number;
  total_capcodes: number;
  total_messages: number;
  total_systems: number;
  messages_today: number;
  new_users_today: number;
  total_installations: number;
}

interface AppVersionRow {
  version: string;
  ios: number;
  android: number;
  devices: number;
  accounts: number;
}

interface AppVersionStats {
  window_days: number;
  new_app_major: number;
  active_devices: number;
  new_app_devices: number;
  active_accounts: number;
  new_app_accounts: number;
  registered_devices: number;
  versions: AppVersionRow[];
}

interface StatCard {
  label: string;
  key: keyof Stats;
  icon: LucideIcon;
  accent?: string;
}

const cards: StatCard[] = [
  { label: "Total Users", key: "total_users", icon: Users },
  { label: "Paid Users", key: "paid_users", icon: CreditCard, accent: "text-green-600" },
  { label: "Free Users", key: "free_users", icon: UserX, accent: "text-muted-foreground" },
  { label: "New Users Today", key: "new_users_today", icon: UserPlus, accent: "text-blue-600" },
  { label: "Total Messages", key: "total_messages", icon: MessageSquare },
  { label: "Messages Today", key: "messages_today", icon: MessageSquarePlus, accent: "text-orange-600" },
  { label: "Total Capcodes", key: "total_capcodes", icon: Tag },
  { label: "Active Systems", key: "total_systems", icon: Server },
  { label: "Installations", key: "total_installations", icon: Smartphone },
];

function percent(part: number, whole: number) {
  if (whole === 0) return "0%";
  const p = (part / whole) * 100;
  return `${p < 10 && p > 0 ? p.toFixed(1) : Math.round(p)}%`;
}

function isNewApp(version: string, major: number) {
  return Number.parseInt(version.split(".")[0], 10) >= major;
}

function AppVersionsCard({ data }: { data: AppVersionStats }) {
  const share = data.active_devices ? data.new_app_devices / data.active_devices : 0;
  const n = (v: number) => v.toLocaleString();

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between space-y-0 pb-2">
        <div className="space-y-1">
          <CardTitle className="text-sm font-medium text-muted-foreground">App versions</CardTitle>
          <CardDescription>Devices active in the last {data.window_days} days</CardDescription>
        </div>
        <Smartphone className="size-4 text-muted-foreground" />
      </CardHeader>
      <CardContent className="space-y-6">
        <div className="space-y-3">
          <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
            <p className="text-3xl font-bold tabular-nums">{percent(data.new_app_devices, data.active_devices)}</p>
            <p className="text-sm text-muted-foreground">on the new app ({data.new_app_major}.x)</p>
          </div>
          <div
            role="meter"
            aria-label={`Active devices on the new app`}
            aria-valuemin={0}
            aria-valuemax={data.active_devices}
            aria-valuenow={data.new_app_devices}
            title={`${n(data.new_app_devices)} of ${n(data.active_devices)} active devices`}
            className="h-2 w-full overflow-hidden rounded-full bg-muted"
          >
            <div
              className="h-full rounded-full bg-primary"
              style={{
                width: `${share * 100}%`,
                minWidth: data.new_app_devices > 0 ? 4 : 0,
              }}
            />
          </div>
          <p className="text-sm text-muted-foreground tabular-nums">
            {n(data.new_app_devices)} of {n(data.active_devices)} devices · {n(data.new_app_accounts)} of{" "}
            {n(data.active_accounts)} accounts · {n(data.registered_devices)} devices registered in total
          </p>
        </div>

        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Version</TableHead>
              <TableHead className="text-right">iOS</TableHead>
              <TableHead className="text-right">Android</TableHead>
              <TableHead className="text-right">Devices</TableHead>
              <TableHead className="text-right">Share</TableHead>
              <TableHead className="text-right">Accounts</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {data.versions.length === 0 ? (
              <TableRow>
                <TableCell colSpan={6} className="text-center text-muted-foreground">
                  No active devices
                </TableCell>
              </TableRow>
            ) : (
              data.versions.map((v) => (
                <TableRow key={v.version}>
                  <TableCell className="font-medium">
                    <span className="inline-flex items-center gap-2">
                      {v.version}
                      {isNewApp(v.version, data.new_app_major) && <Badge variant="secondary">New app</Badge>}
                    </span>
                  </TableCell>
                  <TableCell className="text-right tabular-nums">{n(v.ios)}</TableCell>
                  <TableCell className="text-right tabular-nums">{n(v.android)}</TableCell>
                  <TableCell className="text-right tabular-nums">{n(v.devices)}</TableCell>
                  <TableCell className="text-right tabular-nums text-muted-foreground">
                    {percent(v.devices, data.active_devices)}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">{n(v.accounts)}</TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  );
}

export default function DashboardPage() {
  const [stats, setStats] = useState<Stats | null>(null);
  const [versions, setVersions] = useState<AppVersionStats | null>(null);

  useEffect(() => {
    api.get<Stats>("/dashboard").then(setStats).catch(console.error);
    api.get<AppVersionStats>("/dashboard/app-versions").then(setVersions).catch(console.error);
  }, []);

  if (!stats) {
    return (
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        {Array.from({ length: 9 }).map((_, i) => (
          <Card key={i} className="animate-pulse">
            <CardHeader className="pb-2"><div className="h-4 w-24 bg-muted rounded" /></CardHeader>
            <CardContent><div className="h-8 w-16 bg-muted rounded" /></CardContent>
          </Card>
        ))}
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        {cards.map((c) => (
          <Card key={c.key}>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium text-muted-foreground">{c.label}</CardTitle>
              <c.icon className={`size-4 ${c.accent || "text-muted-foreground"}`} />
            </CardHeader>
            <CardContent>
              <p className="text-3xl font-bold tabular-nums">{stats[c.key].toLocaleString()}</p>
            </CardContent>
          </Card>
        ))}
      </div>
      {versions && <AppVersionsCard data={versions} />}
    </div>
  );
}
