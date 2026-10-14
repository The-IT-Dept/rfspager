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
  { name: "frequency", label: "Frequency", type: "number" },
  // Suburb-level only (2 decimals): the mobile systems endpoint exposes these.
  { name: "lat", label: "Latitude", type: "number", step: "any", placeholder: "-33.23" },
  { name: "lng", label: "Longitude", type: "number", step: "any", placeholder: "151.56" },
  // Receptions before this aren't attributed to the current location on the
  // coverage map (e.g. a feed that moved). Blank = always been here.
  { name: "located_since", label: "Located Since", type: "datetime-local" },
  {
    name: "hardware",
    label: "Hardware",
    type: "select",
    options: [
      { value: "own", label: "Own gear" },
      { value: "needs", label: "Needs gear (we help with hardware)" },
    ],
  },
];

export default function EditSystemPage() {
  const { id } = useParams();
  const [data, setData] = useState<Record<string, any> | null>(null);
  useEffect(() => { api.get(`/systems/${id}`).then(setData); }, [id]);
  if (!data) return <p className="text-muted-foreground">Loading...</p>;
  return (
    <ResourceForm
      title="Edit System"
      fields={fields}
      endpoint={`/systems/${id}`}
      method="PUT"
      initialData={data}
      backPath={`/admin/systems/${id}`}
    />
  );
}
