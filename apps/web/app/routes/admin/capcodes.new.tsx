import { ResourceForm, type FieldDef } from "~/components/resource-form";

const fields: FieldDef[] = [
  { name: "address", label: "Address", required: true, placeholder: "Use _ for single char wildcard" },
  { name: "agency_id", label: "Agency ID", type: "number", required: true },
  { name: "alias", label: "Alias", required: true },
  { name: "icon", label: "Icon (FontAwesome)", required: true, placeholder: "fas fa-broadcast-tower" },
  { name: "colour", label: "Colour", type: "color", required: true },
  { name: "ignore", label: "Ignore (hide from frontend)", type: "toggle" },
];

export default function NewCapcodePage() {
  return <ResourceForm title="Create Capcode" fields={fields} endpoint="/capcodes" backPath="/admin/capcodes" />;
}
