import { useState, useEffect } from "react";
import { useParams } from "react-router";
import { ResourceForm, type FieldDef } from "~/components/resource-form";
import { api } from "~/lib/api";

const fields: FieldDef[] = [
  { name: "name", label: "Name", required: true },
  { name: "email", label: "Email", type: "email", required: true },
  { name: "password", label: "Password (leave blank to keep)", type: "password" },
  { name: "subscription_product_id", label: "Subscription Plan", type: "select", options: [
    { value: "io.devlan.rfspager.monthly", label: "Monthly" },
    { value: "io.devlan.rfspager.yearly", label: "Yearly" },
    { value: "io.devlan.rfspager.gifted", label: "Gifted" },
  ]},
  { name: "subscription_expiry", label: "Subscription Expiry", type: "datetime" },
  { name: "subscription_valid", label: "Subscription Valid", type: "toggle" },
  { name: "will_renew", label: "Will Renew", type: "toggle" },
  { name: "superuser", label: "Superuser", type: "toggle" },
];

export default function EditUserPage() {
  const { id } = useParams();
  const [data, setData] = useState<Record<string, any> | null>(null);

  useEffect(() => { api.get(`/users/${id}`).then(setData); }, [id]);

  if (!data) return <p className="text-muted-foreground">Loading...</p>;

  return <ResourceForm title="Edit User" fields={fields} endpoint={`/users/${id}`} method="PUT" initialData={data} backPath="/admin/users" />;
}
