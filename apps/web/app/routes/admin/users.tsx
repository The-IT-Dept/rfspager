import { Link } from "react-router";
import { Badge } from "~/components/ui/badge";
import { DataTable, type Column } from "~/components/data-table";
import { api } from "~/lib/api";

const platformBadge = (v: any) => {
  if (!v) return null;
  return <Badge variant="secondary">{String(v)}</Badge>;
};

const columns: Column[] = [
  { key: "id", label: "ID", sortable: true },
  {
    key: "name",
    label: "Name",
    sortable: true,
    render: (v, row) => (
      <Link to={`/admin/users/${row.id}`} className="text-primary hover:underline font-medium">
        {v || "(no name)"}
      </Link>
    ),
  },
  { key: "email", label: "Email", sortable: true },
  { key: "subscription_valid", label: "Subscribed", sortable: true, render: (v) => v ? <Badge>Pro</Badge> : <Badge variant="outline">Free</Badge> },
  { key: "subscription_platform", label: "Platform", render: platformBadge },
  { key: "subscription_expiry", label: "Expiry", sortable: true, render: (v) => v ? new Date(v).toLocaleDateString() : "—" },
  { key: "installations_count", label: "Devices", sortable: true },
  { key: "subscriptions_count", label: "Capcodes", sortable: true },
  { key: "superuser", label: "Admin", render: (v) => v ? <Badge variant="destructive">Super</Badge> : null },
];

export default function UsersPage() {
  return (
    <DataTable
      endpoint="/users"
      columns={columns}
      title="Users"
      createPath="/admin/users/new"
      editPath={(r) => `/admin/users/${r.id}`}
      onDelete={(r) => api.delete(`/users/${r.id}`)}
    />
  );
}
