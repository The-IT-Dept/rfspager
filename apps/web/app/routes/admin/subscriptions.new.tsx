import { ResourceForm } from "~/components/resource-form";

export default function NewSubscriptionPage() {
  return <ResourceForm title="Create Subscription" fields={[
    { name: "user_id", label: "User ID", type: "number", required: true },
    { name: "capcode_id", label: "Capcode ID", type: "number", required: true },
  ]} endpoint="/subscriptions" backPath="/admin/subscriptions" />;
}
