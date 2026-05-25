import { useState, useEffect } from "react";
import { useParams } from "react-router";
import { ResourceForm, type FieldDef } from "~/components/resource-form";
import { ApiKeyField } from "~/components/api-key-field";
import { api } from "~/lib/api";

const fields: FieldDef[] = [
  { name: "user_id", label: "Owner User ID", type: "number", required: true },
  { name: "system_id", label: "UUID", required: true },
  { name: "name", label: "Name", required: true },
  {
    name: "api_key",
    label: "Ingress API Key",
    render: ({ value, onChange, disabled }) => (
      <ApiKeyField value={value} onChange={onChange} disabled={disabled} />
    ),
  },
  { name: "sync", label: "Sync Enabled", type: "toggle" },
  { name: "steal_capcodes", label: "Steal Capcodes", type: "toggle" },
  { name: "frequency", label: "Frequency", type: "number" },
  { name: "base_url", label: "Base URL" },
];

export default function EditSystemPage() {
  const { id } = useParams();
  const [data, setData] = useState<Record<string, any> | null>(null);
  useEffect(() => { api.get(`/systems/${id}`).then(setData); }, [id]);
  if (!data) return <p className="text-muted-foreground">Loading...</p>;
  return <ResourceForm title="Edit System" fields={fields} endpoint={`/systems/${id}`} method="PUT" initialData={data} backPath="/admin/systems" />;
}
