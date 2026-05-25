import { useState, useEffect } from "react";
import { useParams } from "react-router";
import { ResourceForm } from "~/components/resource-form";
import { api } from "~/lib/api";

export default function EditAdminUserPage() {
  const { id } = useParams();
  const [data, setData] = useState<Record<string, any> | null>(null);
  useEffect(() => { api.get(`/admin-users/${id}`).then((r) => setData(r.data?.[0] || r)); }, [id]);
  if (!data) return <p className="text-muted-foreground">Loading...</p>;
  return <ResourceForm title="Edit Admin User" fields={[
    { name: "email", label: "Email", type: "email", required: true },
    { name: "name", label: "Name", required: true },
    { name: "password", label: "Password (leave blank to keep)", type: "password" },
    { name: "role", label: "Role", type: "select", required: true, options: [
      { value: "admin", label: "Admin" },
      { value: "superadmin", label: "Super Admin" },
    ]},
    { name: "active", label: "Active", type: "toggle" },
  ]} endpoint={`/admin-users/${id}`} method="PUT" initialData={data} backPath="/admin/admin-users" />;
}
