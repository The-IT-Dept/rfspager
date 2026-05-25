import { useState, useEffect } from "react";
import { useParams } from "react-router";
import { ResourceForm } from "~/components/resource-form";
import { api } from "~/lib/api";

export default function EditAgencyPage() {
  const { id } = useParams();
  const [data, setData] = useState<Record<string, any> | null>(null);
  useEffect(() => { api.get(`/agencies/${id}`).then(setData); }, [id]);
  if (!data) return <p className="text-muted-foreground">Loading...</p>;
  return <ResourceForm title="Edit Agency" fields={[{ name: "agency", label: "Agency Name", required: true }]} endpoint={`/agencies/${id}`} method="PUT" initialData={data} backPath="/admin/agencies" />;
}
