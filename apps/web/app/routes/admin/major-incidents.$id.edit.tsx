import { useState, useEffect } from "react";
import { useParams } from "react-router";
import { ResourceForm } from "~/components/resource-form";
import { api } from "~/lib/api";

export default function EditMajorIncidentPage() {
  const { id } = useParams();
  const [data, setData] = useState<Record<string, any> | null>(null);
  useEffect(() => { api.get(`/major-incidents/${id}`).then(setData); }, [id]);
  if (!data) return <p className="text-muted-foreground">Loading...</p>;
  return <ResourceForm title="Edit Major Incident" fields={[
    { name: "title", label: "Title", required: true },
    { name: "upstream_id", label: "Upstream ID" },
    { name: "category", label: "Category" },
  ]} endpoint={`/major-incidents/${id}`} method="PUT" initialData={data} backPath="/admin/major-incidents" />;
}
