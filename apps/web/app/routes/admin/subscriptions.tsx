import { DataTable, type Column } from "~/components/data-table";
import { api } from "~/lib/api";

const columns: Column[] = [
  { key: "id", label: "ID", sortable: true },
  { key: "user_name", label: "User", sortable: true },
  { key: "capcode_address", label: "Capcode", sortable: true },
  { key: "agency_name", label: "Agency" },
  { key: "capcode_alias", label: "Alias" },
  { key: "created_at", label: "Created", sortable: true },
];

export default function SubscriptionsPage() {
  return (
    <DataTable endpoint="/subscriptions" columns={columns} title="Subscriptions"
      createPath="/admin/subscriptions/new"
      onDelete={(r) => api.delete(`/subscriptions/${r.id}`)} />
  );
}
