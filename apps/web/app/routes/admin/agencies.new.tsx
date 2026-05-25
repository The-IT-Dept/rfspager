import { ResourceForm } from "~/components/resource-form";

export default function NewAgencyPage() {
  return <ResourceForm title="Create Agency" fields={[{ name: "agency", label: "Agency Name", required: true }]} endpoint="/agencies" backPath="/admin/agencies" />;
}
