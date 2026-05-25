import { Badge } from "~/components/ui/badge";
import { DataTable, type Column } from "~/components/data-table";
import { api } from "~/lib/api";

const columns: Column[] = [
  { key: "id", label: "ID", sortable: true },
  { key: "name", label: "Name", sortable: true },
  { key: "email", label: "Email", sortable: true },
  { key: "subscription_valid", label: "Subscribed", sortable: true, render: (v) => v ? <Badge>Pro</Badge> : <Badge variant="outline">Free</Badge> },
  { key: "subscription_expiry", label: "Expiry", sortable: true },
  { key: "installations_count", label: "Devices", sortable: true },
  { key: "subscriptions_count", label: "Capcodes", sortable: true },
  { key: "alertzones_count", label: "Alertzones", sortable: true },
  { key: "superuser", label: "Admin", render: (v) => v ? <Badge variant="destructive">Super</Badge> : null },
];

export default function UsersPage() {
  return (
    <DataTable
      endpoint="/users"
      columns={columns}
      title="Users"
      createPath="/admin/users/new"
      editPath={(r) => `/admin/users/${r.id}/edit`}
      onDelete={(r) => api.delete(`/users/${r.id}`)}
    />
  );
}
