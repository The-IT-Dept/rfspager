import { useState } from "react";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { Label } from "~/components/ui/label";
import { Switch } from "~/components/ui/switch";
import { DataTable, type Column } from "~/components/data-table";
import { api } from "~/lib/api";

function ApiKeyCell({ value }: { value: unknown }) {
  if (!value || typeof value !== "string") return <span className="text-muted-foreground">—</span>;
  const masked = value.slice(0, 4) + "…" + value.slice(-4);
  return (
    <span className="inline-flex items-center gap-2 font-mono text-xs">
      {masked}
      <Button
        type="button"
        variant="ghost"
        size="sm"
        className="h-5 px-1"
        onClick={(e) => { e.stopPropagation(); navigator.clipboard.writeText(value); }}
        title="Copy full key"
      >
        copy
      </Button>
    </span>
  );
}

function DeletedBadge({ value }: { value: unknown }) {
  if (!value) return null;
  return <Badge variant="destructive">deleted</Badge>;
}

function StatusBadge({ row }: { row: Record<string, any> }) {
  if (row.deleted_at) return <Badge variant="destructive">deleted</Badge>;
  if (!row.approved_at) {
    return (
      <Badge variant="outline" className="border-amber-500 text-amber-700 bg-amber-50">
        pending
      </Badge>
    );
  }
  return (
    <Badge variant="outline" className="border-emerald-500 text-emerald-700 bg-emerald-50">
      approved
    </Badge>
  );
}

function ApproveCell({
  row,
  onApproved,
}: {
  row: Record<string, any>;
  onApproved: () => void;
}) {
  const [busy, setBusy] = useState(false);
  if (row.approved_at) return null;
  return (
    <Button
      size="sm"
      onClick={async (e) => {
        e.stopPropagation();
        if (!confirm(`Approve "${row.name}" and issue an API key?`)) return;
        setBusy(true);
        try {
          await api.post(`/systems/${row.id}/approve`);
          onApproved();
        } catch (err: any) {
          alert(err?.message || "Approval failed");
        } finally {
          setBusy(false);
        }
      }}
      disabled={busy}
    >
      {busy ? "Approving…" : "Approve"}
    </Button>
  );
}

export default function SystemsPage() {
  const [showDeleted, setShowDeleted] = useState(false);
  const [pendingOnly, setPendingOnly] = useState(false);
  // Bump on approval to force DataTable refetch.
  const [refreshNonce, setRefreshNonce] = useState(0);

  const params = new URLSearchParams();
  if (showDeleted) params.set("show_deleted", "1");
  if (pendingOnly) params.set("approved_at__null", "1"); // backend may ignore — we also filter client-side
  const endpoint = `/systems${params.toString() ? `?${params}` : ""}`;

  const columns: Column[] = [
    { key: "id", label: "ID", sortable: true },
    {
      key: "_status",
      label: "Status",
      render: (_, row) => <StatusBadge row={row as Record<string, any>} />,
    },
    { key: "user_name", label: "Owner", sortable: true },
    { key: "name", label: "Name", sortable: true },
    { key: "suburb", label: "Suburb", sortable: true },
    { key: "system_id", label: "UUID" },
    { key: "api_key", label: "Ingress Key", render: (v) => <ApiKeyCell value={v} /> },
    { key: "last_message_at", label: "Last Message", sortable: true },
    { key: "created_at", label: "Submitted", sortable: true },
    {
      key: "_approve",
      label: "",
      render: (_, row) => (
        <ApproveCell
          row={row as Record<string, any>}
          onApproved={() => setRefreshNonce((n) => n + 1)}
        />
      ),
    },
    { key: "deleted_at", label: "", render: (v) => <DeletedBadge value={v} /> },
  ];

  return (
    <DataTable
      key={`${showDeleted}-${pendingOnly}-${refreshNonce}`}
      endpoint={endpoint}
      columns={columns}
      title="Systems"
      createPath="/admin/systems/new"
      editPath={(r) => `/admin/systems/${r.id}/edit`}
      onDelete={(r) => api.delete(`/systems/${r.id}`)}
      headerActions={
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2">
            <Switch
              id="pending-only"
              checked={pendingOnly}
              onCheckedChange={setPendingOnly}
            />
            <Label htmlFor="pending-only" className="text-sm whitespace-nowrap">
              Pending only
            </Label>
          </div>
          <div className="flex items-center gap-2">
            <Switch id="show-deleted" checked={showDeleted} onCheckedChange={setShowDeleted} />
            <Label htmlFor="show-deleted" className="text-sm whitespace-nowrap">
              Show deleted
            </Label>
          </div>
        </div>
      }
    />
  );
}
