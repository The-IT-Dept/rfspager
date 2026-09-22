import { useEffect, useState } from "react";
import { Link } from "react-router";
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card";
import { Badge } from "~/components/ui/badge";
import { DataTable, type Column } from "~/components/data-table";
import { api } from "~/lib/api";
import {
  Apple,
  Globe,
  Gift,
  CreditCard,
  TrendingUp,
  CalendarClock,
  XCircle,
  RefreshCcw,
  Users as UsersIcon,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

interface BillingStats {
  active_total: number;
  active_apple: number;
  active_google: number;
  active_stripe: number;
  active_gifted: number;
  new_subs_7d: number;
  new_subs_30d: number;
  cancellations_30d: number;
  refunds_30d: number;
  expiring_soon: number;
  will_not_renew: number;
  mrr_estimate_cents: number;
}

interface StatCard {
  label: string;
  key: keyof BillingStats;
  icon: LucideIcon;
  accent?: string;
  format?: "number" | "currency";
  sub?: string;
}

const headlineCards: StatCard[] = [
  { label: "Active Subscriptions", key: "active_total", icon: UsersIcon, accent: "text-green-600" },
  { label: "MRR (est.)", key: "mrr_estimate_cents", icon: TrendingUp, accent: "text-green-600", format: "currency" },
  { label: "New (30d)", key: "new_subs_30d", icon: TrendingUp, accent: "text-blue-600", sub: "new_subs_7d" },
  { label: "Cancellations (30d)", key: "cancellations_30d", icon: XCircle, accent: "text-orange-600" }, // gitleaks:allow — metric field name
  { label: "Refunds (30d)", key: "refunds_30d", icon: RefreshCcw, accent: "text-red-600" },
  { label: "Expiring in 7d", key: "expiring_soon", icon: CalendarClock, accent: "text-amber-600" },
];

const platformCards: StatCard[] = [
  { label: "Apple", key: "active_apple", icon: Apple },
  { label: "Google", key: "active_google", icon: Globe },
  { label: "Stripe (web)", key: "active_stripe", icon: CreditCard },
  { label: "Gifted", key: "active_gifted", icon: Gift },
];

function formatCents(cents: number): string {
  return new Intl.NumberFormat("en-AU", {
    style: "currency",
    currency: "AUD",
    maximumFractionDigits: 0,
  }).format(cents / 100);
}

const platformBadge = (v: string) => {
  if (v === "apple") return <Badge variant="secondary">Apple</Badge>;
  if (v === "google") return <Badge variant="secondary">Google</Badge>;
  if (v === "stripe") return <Badge variant="secondary">Stripe</Badge>;
  return <Badge variant="outline">{v}</Badge>;
};

const eventBadge = (v: string) => {
  const valid = (v || "").toLowerCase();
  if (valid.includes("refund") || valid.includes("revoke") || valid.includes("expire")) {
    return <Badge variant="destructive">{v}</Badge>;
  }
  if (valid.includes("purchase") || valid.includes("renew") || valid === "restore") {
    return <Badge>{v}</Badge>;
  }
  return <Badge variant="outline">{v}</Badge>;
};

const txnColumns: Column[] = [
  { key: "created_at", label: "When", sortable: true, render: (v) => v ? new Date(v).toLocaleString() : "—" },
  { key: "user_email", label: "User", render: (_v, row) => row.user_id ? (
    <Link to={`/admin/users/${row.user_id}`} className="text-primary hover:underline">
      {row.user_email || row.user_name || `#${row.user_id}`}
    </Link>
  ) : "—" },
  { key: "platform", label: "Platform", render: platformBadge },
  { key: "event_type", label: "Event", render: eventBadge },
  { key: "product_id", label: "Product", sortable: true },
  { key: "is_valid", label: "Valid", render: (v) => v ? <Badge>Yes</Badge> : <Badge variant="outline">No</Badge> },
  { key: "expires_at", label: "Expires", render: (v) => v ? new Date(v).toLocaleDateString() : "—" },
];

export default function BillingPage() {
  const [stats, setStats] = useState<BillingStats | null>(null);

  useEffect(() => {
    api.get<BillingStats>("/billing/stats").then(setStats).catch(console.error);
  }, []);

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Billing &amp; Subscriptions</h1>
        <p className="text-sm text-muted-foreground">
          IAP and Stripe activity across all users. MRR is an estimate based on product SKUs.
        </p>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        {headlineCards.map((c) => {
          const v = stats?.[c.key];
          const display = stats == null
            ? "…"
            : c.format === "currency"
              ? formatCents(Number(v ?? 0))
              : Number(v ?? 0).toLocaleString();
          const sub = c.sub && stats ? `${Number(stats[c.sub as keyof BillingStats] ?? 0).toLocaleString()} in last 7d` : null;
          return (
            <Card key={c.key}>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium text-muted-foreground">{c.label}</CardTitle>
                <c.icon className={`size-4 ${c.accent || "text-muted-foreground"}`} />
              </CardHeader>
              <CardContent>
                <p className="text-3xl font-bold tabular-nums">{display}</p>
                {sub && <p className="text-xs text-muted-foreground mt-1">{sub}</p>}
              </CardContent>
            </Card>
          );
        })}
      </div>

      <div>
        <h2 className="text-lg font-semibold mb-3">Active by platform</h2>
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
          {platformCards.map((c) => (
            <Card key={c.key}>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium text-muted-foreground">{c.label}</CardTitle>
                <c.icon className="size-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <p className="text-2xl font-bold tabular-nums">
                  {stats == null ? "…" : Number(stats[c.key] ?? 0).toLocaleString()}
                </p>
              </CardContent>
            </Card>
          ))}
        </div>
      </div>

      <DataTable
        endpoint="/billing/transactions"
        columns={txnColumns}
        title="Recent transactions"
        editPath={(r) => `/admin/billing/transactions/${r.id}`}
      />
    </div>
  );
}
