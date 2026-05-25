import { ResourceForm } from "~/components/resource-form";

export default function NewAdminUserPage() {
  return <ResourceForm title="Create Admin User" fields={[
    { name: "email", label: "Email", type: "email", required: true },
    { name: "name", label: "Name", required: true },
    { name: "password", label: "Password", type: "password", required: true },
    { name: "role", label: "Role", type: "select", required: true, options: [
      { value: "admin", label: "Admin" },
      { value: "superadmin", label: "Super Admin" },
    ]},
    { name: "active", label: "Active", type: "toggle" },
  ]} endpoint="/admin-users" backPath="/admin/admin-users" />;
}
