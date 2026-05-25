import { Badge } from "~/components/ui/badge";
import { DataTable, type Column } from "~/components/data-table";
import { api } from "~/lib/api";

const columns: Column[] = [
  { key: "id", label: "ID", sortable: true },
  { key: "email", label: "Email", sortable: true },
  { key: "name", label: "Name", sortable: true },
  { key: "role", label: "Role", render: (v) => <Badge variant={v === "superadmin" ? "destructive" : "secondary"}>{v}</Badge> },
  { key: "active", label: "Active", render: (v) => v ? <Badge>Active</Badge> : <Badge variant="outline">Disabled</Badge> },
  { key: "last_login_at", label: "Last Login", sortable: true },
];

export default function AdminUsersPage() {
  return (
    <DataTable endpoint="/admin-users" columns={columns} title="Admin Users"
      createPath="/admin/admin-users/new"
      editPath={(r) => `/admin/admin-users/${r.id}/edit`}
      onDelete={(r) => api.delete(`/admin-users/${r.id}`)} />
  );
}
