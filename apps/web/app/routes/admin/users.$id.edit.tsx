import { useState, useEffect } from "react";
import { useParams } from "react-router";
import { ResourceForm, type FieldDef } from "~/components/resource-form";
import { api } from "~/lib/api";

const fields: FieldDef[] = [
  // Profile
  { name: "name", label: "Name", required: true },
  { name: "email", label: "Email", type: "email", required: true },
  { name: "password", label: "Password (leave blank to keep)", type: "password" },
  { name: "email_verified_at", label: "Email Verified At", type: "datetime" },

  // Identifiers — useful for pulling random accounts into the backfill or
  // pointing webhooks at a specific test user.
  {
    name: "user_id",
    label: "User ID (UUID) — also Adapty's customer_user_id",
    placeholder: "e.g. 305df7f3-71e4-4177-831b-187db692611c",
  },
  {
    name: "adapty_id",
    label: "Adapty Profile ID",
    placeholder: "e.g. d8533a10-bcce-4e33-8c9d-88b05ac56559",
  },
  { name: "last_adapty_sync_at", label: "Last Adapty Sync At", type: "datetime" },

  // Subscription state
  {
    name: "subscription_product_id",
    label: "Subscription Plan",
    type: "select",
    options: [
      { value: "io.devlan.rfspager.monthly", label: "Monthly" },
      { value: "io.devlan.rfspager.yearly", label: "Yearly" },
      { value: "io.devlan.rfspager.gifted", label: "Gifted" },
    ],
  },
  {
    name: "subscription_platform",
    label: "Subscription Platform",
    type: "select",
    options: [
      { value: "apple", label: "Apple" },
      { value: "google", label: "Google" },
      { value: "stripe", label: "Stripe" },
    ],
  },
  { name: "subscription_expiry", label: "Subscription Expiry", type: "datetime" },
  { name: "subscription_valid", label: "Subscription Valid", type: "toggle" },
  { name: "will_renew", label: "Will Renew", type: "toggle" },
  { name: "cancellation_reason", label: "Cancellation Reason" },
  { name: "last_store_sync_at", label: "Last Store Sync At", type: "datetime" },

  // Store transaction identifiers — webhooks match users by these.
  {
    name: "apple_original_transaction_id",
    label: "Apple Original Transaction ID",
    placeholder: "15-19 digit integer from the JWS payload",
  },
  {
    name: "google_purchase_token",
    label: "Google Purchase Token",
    placeholder: "long alphanumeric token from Play",
  },

  { name: "superuser", label: "Superuser", type: "toggle" },
];

export default function EditUserPage() {
  const { id } = useParams();
  const [data, setData] = useState<Record<string, any> | null>(null);

  useEffect(() => { api.get(`/users/${id}`).then(setData); }, [id]);

  if (!data) return <p className="text-muted-foreground">Loading...</p>;

  return <ResourceForm title="Edit User" fields={fields} endpoint={`/users/${id}`} method="PUT" initialData={data} backPath="/admin/users" />;
}
