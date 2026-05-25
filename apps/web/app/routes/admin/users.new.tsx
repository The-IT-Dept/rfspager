import { ResourceForm, type FieldDef } from "~/components/resource-form";

const fields: FieldDef[] = [
  { name: "name", label: "Name", required: true },
  { name: "email", label: "Email", type: "email", required: true },
  { name: "password", label: "Password", type: "password", required: true },
  { name: "subscription_product_id", label: "Subscription Plan", type: "select", options: [
    { value: "io.devlan.rfspager.monthly", label: "Monthly" },
    { value: "io.devlan.rfspager.yearly", label: "Yearly" },
    { value: "io.devlan.rfspager.gifted", label: "Gifted" },
  ]},
  { name: "subscription_expiry", label: "Subscription Expiry", type: "datetime" },
  { name: "subscription_valid", label: "Subscription Valid", type: "toggle" },
  { name: "superuser", label: "Superuser", type: "toggle" },
];

export default function NewUserPage() {
  return <ResourceForm title="Create User" fields={fields} endpoint="/users" backPath="/admin/users" />;
}
