import { useState, useEffect } from "react";
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card";
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

export default function DashboardPage() {
  const [stats, setStats] = useState<Stats | null>(null);

  useEffect(() => {
    api.get<Stats>("/dashboard").then(setStats).catch(console.error);
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
  );
}
