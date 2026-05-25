import { DataTable, type Column } from "~/components/data-table";
import { api } from "~/lib/api";

const columns: Column[] = [
  { key: "id", label: "ID", sortable: true },
  { key: "title", label: "Title", sortable: true },
  { key: "leadAgency", label: "Lead Agency", sortable: true },
  { key: "lga", label: "LGA", sortable: true },
  { key: "size", label: "Size", sortable: true },
  { key: "start_at", label: "Start", sortable: true },
  { key: "end_at", label: "End", sortable: true },
];

export default function HazardReductionsPage() {
  return (
    <DataTable endpoint="/hazard-reductions" columns={columns} title="Hazard Reductions"
      createPath="/admin/hazard-reductions/new" editPath={(r) => `/admin/hazard-reductions/${r.id}/edit`}
      onDelete={(r) => api.delete(`/hazard-reductions/${r.id}`)} />
  );
}
