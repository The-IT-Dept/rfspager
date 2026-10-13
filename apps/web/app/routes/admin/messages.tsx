import { Link } from "react-router";
import { Badge } from "~/components/ui/badge";
import { DataTable, type Column } from "~/components/data-table";
import { api } from "~/lib/api";

function NotificationsCell({ row }: { row: Record<string, any> }) {
  const total = Number(row.notifications_count ?? 0);
  const pushed = Number(row.notifications_pushed_count ?? 0);
  if (total === 0) return <span className="text-muted-foreground">0</span>;
  if (pushed === total) return <Badge variant="secondary">{total}</Badge>;
  return (
    <span className="font-mono text-xs" title={`${pushed} pushed / ${total} total`}>
      {pushed}/{total}
    </span>
  );
}

const columns: Column[] = [
  { key: "id", label: "ID", sortable: true, render: (v) => <Link to={`/admin/messages/${v}`} className="font-mono hover:underline">{v}</Link> },
  { key: "system_name", label: "System", sortable: true },
  { key: "agency_name", label: "Agency" },
  { key: "capcode_alias", label: "Capcode" },
  { key: "address", label: "Address", sortable: true },
  { key: "message", label: "Message", render: (v, row) => v
      ? <Link to={`/admin/messages/${row.id}`} className="hover:underline">{String(v).length > 60 ? String(v).slice(0, 60) + "…" : String(v)}</Link>
      : "—" },
  { key: "notifications_count", label: "Notifs", render: (_v, row) => <NotificationsCell row={row} /> },
  { key: "created_at", label: "Created", sortable: true },
];

export default function MessagesPage() {
  return (
    <DataTable endpoint="/messages" columns={columns} title="Messages"
      editPath={(r) => `/admin/messages/${r.id}/edit`}
      onDelete={(r) => api.delete(`/messages/${r.id}`)} />
  );
}
