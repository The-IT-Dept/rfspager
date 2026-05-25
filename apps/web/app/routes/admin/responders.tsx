import { DataTable, type Column } from "~/components/data-table";
import { api } from "~/lib/api";

const columns: Column[] = [
  { key: "id", label: "ID", sortable: true },
  { key: "user_name", label: "User", sortable: true },
  { key: "message_preview", label: "Message" },
  { key: "created_at", label: "Created", sortable: true },
];

export default function RespondersPage() {
  return (
    <DataTable endpoint="/responders" columns={columns} title="Responders"
      onDelete={(r) => api.delete(`/responders/${r.id}`)} />
  );
}
