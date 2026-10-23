import { Button } from "~/components/ui/button";
import { DataTable, type Column } from "~/components/data-table";
import { api } from "~/lib/api";

// An amendment carries capcode_id; approving it updates that capcode with
// whichever of alias/agency the user filled in. Without it, approval creates
// a new capcode for the address.
const isAmendment = (row: Record<string, any>) => row.capcode_id != null;

const columns: Column[] = [
  { key: "id", label: "ID", sortable: true },
  { key: "capcode_id", label: "Type", render: (_v, row) => isAmendment(row) ? "Edit" : "New" },
  { key: "user_name", label: "Submitted By", sortable: true },
  { key: "address", label: "Address", sortable: true },
  { key: "current_alias", label: "Current", render: (_v, row) => isAmendment(row)
    ? <span className="text-muted-foreground">{[row.current_alias, row.current_agency].filter(Boolean).join(" · ") || "—"}</span>
    : "—" },
  { key: "agency", label: "Agency", sortable: true },
  { key: "alias", label: "Alias", sortable: true },
  { key: "note", label: "Note", render: (v) => v ? <span className="whitespace-pre-wrap">{v}</span> : "—" },
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
          const prompt = isAmendment(r)
            ? `Approve this suggestion and update capcode ${r.address}?`
            : "Approve this suggestion and create a capcode?";
          if (!confirm(prompt)) return;
          await api.post(`/capcode-suggestions/${r.id}/approve`);
          window.location.reload();
        }}>Approve</Button>
      )}
    />
  );
}
