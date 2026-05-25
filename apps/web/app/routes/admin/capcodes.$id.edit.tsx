import { useState, useEffect } from "react";
import { useParams } from "react-router";
import { ResourceForm, type FieldDef } from "~/components/resource-form";
import { api } from "~/lib/api";

const fields: FieldDef[] = [
  { name: "address", label: "Address", required: true },
  { name: "agency_id", label: "Agency ID", type: "number", required: true },
  { name: "alias", label: "Alias", required: true },
  { name: "icon", label: "Icon (FontAwesome)", required: true },
  { name: "colour", label: "Colour", type: "color", required: true },
  { name: "ignore", label: "Ignore", type: "toggle" },
];

export default function EditCapcodePage() {
  const { id } = useParams();
  const [data, setData] = useState<Record<string, any> | null>(null);
  useEffect(() => { api.get(`/capcodes/${id}`).then(setData); }, [id]);
  if (!data) return <p className="text-muted-foreground">Loading...</p>;
  return <ResourceForm title="Edit Capcode" fields={fields} endpoint={`/capcodes/${id}`} method="PUT" initialData={data} backPath="/admin/capcodes" />;
}
