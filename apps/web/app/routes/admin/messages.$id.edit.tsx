import { useState, useEffect } from "react";
import { useParams } from "react-router";
import { ResourceForm, type FieldDef } from "~/components/resource-form";
import { api } from "~/lib/api";

const fields: FieldDef[] = [
  { name: "system_id", label: "System ID", type: "number", disabled: true },
  { name: "capcode_id", label: "Capcode ID", type: "number" },
  { name: "address", label: "Address", required: true },
  { name: "message", label: "Message", type: "textarea", required: true },
  { name: "lat", label: "Latitude" },
  { name: "lng", label: "Longitude" },
];

export default function EditMessagePage() {
  const { id } = useParams();
  const [data, setData] = useState<Record<string, any> | null>(null);
  useEffect(() => { api.get(`/messages/${id}`).then(setData); }, [id]);
  if (!data) return <p className="text-muted-foreground">Loading...</p>;
  return <ResourceForm title="Edit Message" fields={fields} endpoint={`/messages/${id}`} method="PUT" initialData={data} backPath="/admin/messages" />;
}
