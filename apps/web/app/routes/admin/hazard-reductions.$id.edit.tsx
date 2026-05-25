import { useState, useEffect } from "react";
import { useParams } from "react-router";
import { ResourceForm } from "~/components/resource-form";
import { api } from "~/lib/api";

export default function EditHazardReductionPage() {
  const { id } = useParams();
  const [data, setData] = useState<Record<string, any> | null>(null);
  useEffect(() => { api.get(`/hazard-reductions/${id}`).then(setData); }, [id]);
  if (!data) return <p className="text-muted-foreground">Loading...</p>;
  return <ResourceForm title="Edit Hazard Reduction" fields={[
    { name: "title", label: "Title", required: true },
    { name: "upstream_id", label: "Upstream ID" },
    { name: "leadAgency", label: "Lead Agency" },
    { name: "supportingAgencies", label: "Supporting Agencies" },
    { name: "lga", label: "LGA" },
    { name: "size", label: "Size (ha)", type: "number" },
    { name: "tenure", label: "Tenure" },
    { name: "start_at", label: "Start Date", type: "datetime" },
    { name: "end_at", label: "End Date", type: "datetime" },
  ]} endpoint={`/hazard-reductions/${id}`} method="PUT" initialData={data} backPath="/admin/hazard-reductions" />;
}
