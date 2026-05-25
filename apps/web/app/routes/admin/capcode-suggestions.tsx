import { Button } from "~/components/ui/button";
import { DataTable, type Column } from "~/components/data-table";
import { api } from "~/lib/api";

const columns: Column[] = [
  { key: "id", label: "ID", sortable: true },
  { key: "user_name", label: "Submitted By", sortable: true },
  { key: "address", label: "Address", sortable: true },
  { key: "agency", label: "Agency", sortable: true },
  { key: "alias", label: "Alias", sortable: true },
  { key: "icon", label: "Icon" },
  { key: "colour", label: "Colour", render: (v) => v ? <span className="w-4 h-4 rounded-full inline-block border" style={{ backgroundColor: v }} /> : "—" },
  { key: "created_at", label: "Created", sortable: true },
];

export default function CapcodeSuggestionsPage() {
  return (
    <DataTable endpoint="/capcode-suggestions" columns={columns} title="Capcode Suggestions"
      onDelete={(r) => api.delete(`/capcode-suggestions/${r.id}`)}
      actions={(r) => (
        <Button variant="ghost" size="sm" className="text-green-600" onClick={async () => {
          if (!confirm("Approve this suggestion and create a capcode?")) return;
          await api.post(`/capcode-suggestions/${r.id}/approve`);
          window.location.reload();
        }}>Approve</Button>
      )}
    />
  );
}
