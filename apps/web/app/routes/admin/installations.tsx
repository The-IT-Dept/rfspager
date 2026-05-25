import { DataTable, type Column } from "~/components/data-table";
import { api } from "~/lib/api";

const columns: Column[] = [
  { key: "id", label: "ID", render: (v) => v ? String(v).slice(0, 8) + "…" : "—" },
  { key: "user_name", label: "User", sortable: true },
  { key: "push_type", label: "Push Type", sortable: true },
  { key: "vendor", label: "Vendor", sortable: true },
  { key: "model", label: "Model", sortable: true },
  { key: "version", label: "OS Ver", sortable: true },
  { key: "app_version", label: "App Ver", sortable: true },
  { key: "created_at", label: "Created", sortable: true },
];

export default function InstallationsPage() {
  return (
    <DataTable endpoint="/installations" columns={columns} title="Installations"
      onDelete={(r) => api.delete(`/installations/${r.id}`)} />
  );
}
