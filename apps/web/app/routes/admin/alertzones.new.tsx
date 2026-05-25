import { ResourceForm } from "~/components/resource-form";

export default function NewAlertzonePage() {
  return <ResourceForm title="Create Alertzone" fields={[
    { name: "user_id", label: "User ID", type: "number", required: true },
    { name: "name", label: "Name", required: true },
    { name: "lat", label: "Latitude", type: "number", required: true },
    { name: "lng", label: "Longitude", type: "number", required: true },
    { name: "radius", label: "Radius (km)", type: "number", required: true },
  ]} endpoint="/alertzones" backPath="/admin/alertzones" />;
}
