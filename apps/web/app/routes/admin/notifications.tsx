import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { DataTable, type Column } from "~/components/data-table";
import { api } from "~/lib/api";

const columns: Column[] = [
  { key: "notifiable_id", label: "User ID", sortable: true },
  { key: "type", label: "Type", render: (v) => v ? String(v).split("\\").pop() : "—" },
  { key: "read_at", label: "Read", render: (v) => v ? <Badge>Read</Badge> : <Badge variant="outline">Unread</Badge> },
  { key: "created_at", label: "Created", sortable: true },
];

export default function NotificationsPage() {
  return (
    <DataTable endpoint="/notifications" columns={columns} title="Notifications"
      onDelete={(r) => api.delete(`/notifications/${r.id}`)}
      actions={(r) => !r.read_at ? (
        <Button variant="ghost" size="sm" onClick={async () => { await api.post(`/notifications/${r.id}/read`); window.location.reload(); }}>
          Mark Read
        </Button>
      ) : null}
    />
  );
}
