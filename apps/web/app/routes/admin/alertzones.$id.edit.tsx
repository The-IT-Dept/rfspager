import { useState, useEffect } from "react";
import { useParams } from "react-router";
import { ResourceForm } from "~/components/resource-form";
import { api } from "~/lib/api";

export default function EditAlertzonePage() {
  const { id } = useParams();
  const [data, setData] = useState<Record<string, any> | null>(null);
  useEffect(() => { api.get(`/alertzones/${id}`).then(setData); }, [id]);
  if (!data) return <p className="text-muted-foreground">Loading...</p>;
  return <ResourceForm title="Edit Alertzone" fields={[
    { name: "user_id", label: "User ID", type: "number", required: true },
    { name: "name", label: "Name", required: true },
    { name: "lat", label: "Latitude", type: "number", required: true },
    { name: "lng", label: "Longitude", type: "number", required: true },
    { name: "radius", label: "Radius (km)", type: "number", required: true },
  ]} endpoint={`/alertzones/${id}`} method="PUT" initialData={data} backPath="/admin/alertzones" />;
}
