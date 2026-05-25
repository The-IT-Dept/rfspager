import { ResourceForm } from "~/components/resource-form";

export default function NewMajorIncidentPage() {
  return <ResourceForm title="Create Major Incident" fields={[
    { name: "title", label: "Title", required: true },
    { name: "upstream_id", label: "Upstream ID" },
    { name: "category", label: "Category" },
  ]} endpoint="/major-incidents" backPath="/admin/major-incidents" />;
}
