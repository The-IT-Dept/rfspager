import { Badge } from "~/components/ui/badge";
import { DataTable, type Column } from "~/components/data-table";
import { api } from "~/lib/api";

const columns: Column[] = [
  { key: "id", label: "ID", sortable: true },
  { key: "address", label: "Address", sortable: true },
  { key: "agency_name", label: "Agency", sortable: true },
  { key: "alias", label: "Alias", sortable: true },
  { key: "icon", label: "Icon" },
  { key: "colour", label: "Colour", render: (v) => v ? <div className="flex items-center gap-2"><span className="w-4 h-4 rounded-full border" style={{ backgroundColor: v }} />{v}</div> : "—" },
  { key: "ignore", label: "Ignored", render: (v) => v ? <Badge variant="destructive">Yes</Badge> : null },
  { key: "messages_count", label: "Messages", sortable: true },
  { key: "subscriptions_count", label: "Subs", sortable: true },
];

export default function CapcodesPage() {
  return (
    <DataTable endpoint="/capcodes" columns={columns} title="Capcodes"
      createPath="/admin/capcodes/new" editPath={(r) => `/admin/capcodes/${r.id}/edit`}
      onDelete={(r) => api.delete(`/capcodes/${r.id}`)} />
  );
}
