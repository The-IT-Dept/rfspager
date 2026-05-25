import { DataTable, type Column } from "~/components/data-table";
import { api } from "~/lib/api";

const columns: Column[] = [
  { key: "id", label: "ID", sortable: true },
  { key: "agency", label: "Agency Name", sortable: true },
  { key: "capcodes_count", label: "Capcodes", sortable: true },
  { key: "created_at", label: "Created", sortable: true },
];

export default function AgenciesPage() {
  return (
    <DataTable endpoint="/agencies" columns={columns} title="Agencies"
      createPath="/admin/agencies/new" editPath={(r) => `/admin/agencies/${r.id}/edit`}
      onDelete={(r) => api.delete(`/agencies/${r.id}`)} />
  );
}
