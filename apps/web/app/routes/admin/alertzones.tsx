import { DataTable, type Column } from "~/components/data-table";
import { api } from "~/lib/api";

const columns: Column[] = [
  { key: "id", label: "ID", sortable: true },
  { key: "user_name", label: "User", sortable: true },
  { key: "name", label: "Name", sortable: true },
  { key: "lat", label: "Latitude", sortable: true },
  { key: "lng", label: "Longitude", sortable: true },
  { key: "radius", label: "Radius (km)", sortable: true },
  { key: "created_at", label: "Created", sortable: true },
];

export default function AlertzonesPage() {
  return (
    <DataTable endpoint="/alertzones" columns={columns} title="Alertzones"
      createPath="/admin/alertzones/new" editPath={(r) => `/admin/alertzones/${r.id}/edit`}
      onDelete={(r) => api.delete(`/alertzones/${r.id}`)} />
  );
}
