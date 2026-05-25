import { ResourceForm } from "~/components/resource-form";

export default function NewHazardReductionPage() {
  return <ResourceForm title="Create Hazard Reduction" fields={[
    { name: "title", label: "Title", required: true },
    { name: "upstream_id", label: "Upstream ID" },
    { name: "leadAgency", label: "Lead Agency" },
    { name: "supportingAgencies", label: "Supporting Agencies" },
    { name: "lga", label: "LGA" },
    { name: "size", label: "Size (ha)", type: "number" },
    { name: "tenure", label: "Tenure" },
    { name: "start_at", label: "Start Date", type: "datetime" },
    { name: "end_at", label: "End Date", type: "datetime" },
  ]} endpoint="/hazard-reductions" backPath="/admin/hazard-reductions" />;
}
