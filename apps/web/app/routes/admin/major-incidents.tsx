import { DataTable, type Column } from "~/components/data-table";
import { api } from "~/lib/api";

const columns: Column[] = [
  { key: "id", label: "ID", sortable: true },
  { key: "title", label: "Title", sortable: true },
  { key: "category", label: "Category", sortable: true },
  { key: "created_at", label: "Created", sortable: true },
  { key: "updated_at", label: "Updated", sortable: true },
];

export default function MajorIncidentsPage() {
  return (
    <DataTable endpoint="/major-incidents" columns={columns} title="Major Incidents"
      createPath="/admin/major-incidents/new" editPath={(r) => `/admin/major-incidents/${r.id}/edit`}
      onDelete={(r) => api.delete(`/major-incidents/${r.id}`)} />
  );
}
