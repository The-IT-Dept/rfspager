import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router";
import {
  customerApi,
  clearCustomerToken,
  productLabel,
  type CustomerUser,
  CustomerApiError,
} from "~/lib/customer-api";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "~/components/ui/card";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { Button } from "~/components/ui/button";
import { Badge } from "~/components/ui/badge";
import { Separator } from "~/components/ui/separator";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "~/components/ui/dialog";

const heading = "font-[family-name:var(--font-heading)]";

export function meta() {
  return [{ title: "My Account — RFSPager" }];
}

export default function AccountRoute() {
  const navigate = useNavigate();
  const [user, setUser] = useState<CustomerUser | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    customerApi
      .me()
      .then((u) => setUser(u))
      .catch((err: unknown) => {
        if (err instanceof CustomerApiError && err.status === 401) {
          navigate("/login?next=/account");
        }
      })
      .finally(() => setLoading(false));
  }, [navigate]);

  if (loading) {
    return (
      <div className="max-w-3xl mx-auto px-6 py-12">
        <div className="h-8 w-48 bg-gray-100 rounded animate-pulse mb-8" />
        <div className="h-64 bg-gray-100 rounded-2xl animate-pulse" />
      </div>
    );
  }

  if (!user) return null;

  return (
    <div className="max-w-3xl mx-auto px-6 py-12 space-y-8">
      <header>
        <h1 className={`${heading} text-3xl font-extrabold text-gray-900 mb-2`}>My Account</h1>
        <p className="text-sm text-gray-500">
          Manage your profile, password and subscription.
        </p>
      </header>

      <ProfileSection user={user} onUpdated={setUser} />
      <EmailSection user={user} onUpdated={setUser} />
      <PasswordSection />
      <SubscriptionSection user={user} />
      <DangerSection onDeleted={() => navigate("/")} />
    </div>
  );
}

// ----------------------------------------------------------------------------
// Profile (name)
// ----------------------------------------------------------------------------

function ProfileSection({
  user,
  onUpdated,
}: {
  user: CustomerUser;
  onUpdated: (u: CustomerUser) => void;
}) {
  const [name, setName] = useState(user.name ?? "");
  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState<{ kind: "ok" | "error"; text: string } | null>(null);

  const dirty = name.trim() !== (user.name ?? "") && name.trim().length > 0;

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    setStatus(null);
    try {
      const updated = await customerApi.updateProfile({ name: name.trim() });
      onUpdated(updated);
      setStatus({ kind: "ok", text: "Saved." });
    } catch (err: unknown) {
      setStatus({ kind: "error", text: err instanceof Error ? err.message : "Failed to save" });
    } finally {
      setSaving(false);
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>Profile</CardTitle>
        <CardDescription>This is the name others see when you respond to messages.</CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={save} className="space-y-4">
          <div>
            <Label htmlFor="profile-name">Name</Label>
            <Input
              id="profile-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              autoComplete="name"
              className="mt-1.5"
            />
          </div>
          <div className="flex items-center gap-3">
            <Button type="submit" disabled={!dirty || saving}>
              {saving ? "Saving…" : "Save"}
            </Button>
            {status && (
              <span className={status.kind === "ok" ? "text-sm text-green-600" : "text-sm text-red-600"}>
                {status.text}
              </span>
            )}
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

// ----------------------------------------------------------------------------
// Email (requires current password)
// ----------------------------------------------------------------------------

function EmailSection({
  user,
  onUpdated,
}: {
  user: CustomerUser;
  onUpdated: (u: CustomerUser) => void;
}) {
  const [email, setEmail] = useState(user.email);
  const [password, setPassword] = useState("");
  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState<{ kind: "ok" | "error"; text: string } | null>(null);

  const dirty =
    email.trim().toLowerCase() !== user.email.toLowerCase() &&
    /.+@.+\..+/.test(email.trim()) &&
    password.length > 0;

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    setStatus(null);
    try {
      const updated = await customerApi.updateProfile({
        email: email.trim(),
        current_password: password,
      });
      onUpdated(updated);
      setPassword("");
      setStatus({ kind: "ok", text: "Email updated." });
    } catch (err: unknown) {
      setStatus({ kind: "error", text: err instanceof Error ? err.message : "Failed to update" });
    } finally {
      setSaving(false);
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>Email</CardTitle>
        <CardDescription>
          This is the address you use to sign in. We require your current password to change it.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={save} className="space-y-4">
          <div>
            <Label htmlFor="account-email">Email</Label>
            <Input
              id="account-email"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              autoComplete="email"
              className="mt-1.5"
            />
          </div>
          <div>
            <Label htmlFor="email-password">Current password</Label>
            <Input
              id="email-password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="current-password"
              className="mt-1.5"
            />
          </div>
          <div className="flex items-center gap-3">
            <Button type="submit" disabled={!dirty || saving}>
              {saving ? "Saving…" : "Change email"}
            </Button>
            {status && (
              <span className={status.kind === "ok" ? "text-sm text-green-600" : "text-sm text-red-600"}>
                {status.text}
              </span>
            )}
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

// ----------------------------------------------------------------------------
// Password
// ----------------------------------------------------------------------------

function PasswordSection() {
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [confirm, setConfirm] = useState("");
  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState<{ kind: "ok" | "error"; text: string } | null>(null);

  const canSave = current.length > 0 && next.length >= 8 && next === confirm;

  const save = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!canSave) return;
    setSaving(true);
    setStatus(null);
    try {
      await customerApi.updatePassword(current, next, confirm);
      setCurrent("");
      setNext("");
      setConfirm("");
      setStatus({ kind: "ok", text: "Password updated." });
    } catch (err: unknown) {
      setStatus({ kind: "error", text: err instanceof Error ? err.message : "Failed to update" });
    } finally {
      setSaving(false);
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>Password</CardTitle>
        <CardDescription>Minimum 8 characters.</CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={save} className="space-y-4">
          <div>
            <Label htmlFor="pw-current">Current password</Label>
            <Input
              id="pw-current"
              type="password"
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
              autoComplete="current-password"
              className="mt-1.5"
            />
          </div>
          <div className="grid sm:grid-cols-2 gap-4">
            <div>
              <Label htmlFor="pw-new">New password</Label>
              <Input
                id="pw-new"
                type="password"
                value={next}
                onChange={(e) => setNext(e.target.value)}
                autoComplete="new-password"
                className="mt-1.5"
              />
            </div>
            <div>
              <Label htmlFor="pw-confirm">Confirm new password</Label>
              <Input
                id="pw-confirm"
                type="password"
                value={confirm}
                onChange={(e) => setConfirm(e.target.value)}
                autoComplete="new-password"
                className="mt-1.5"
              />
            </div>
          </div>
          {confirm.length > 0 && next !== confirm && (
            <p className="text-sm text-red-600">Passwords don't match.</p>
          )}
          <div className="flex items-center gap-3">
            <Button type="submit" disabled={!canSave || saving}>
              {saving ? "Saving…" : "Change password"}
            </Button>
            {status && (
              <span className={status.kind === "ok" ? "text-sm text-green-600" : "text-sm text-red-600"}>
                {status.text}
              </span>
            )}
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

// ----------------------------------------------------------------------------
// Subscription (read-only)
// ----------------------------------------------------------------------------

function SubscriptionSection({ user }: { user: CustomerUser }) {
  const expiry = user.subscription_expiry ? new Date(user.subscription_expiry) : null;
  const expiryLabel = expiry
    ? expiry.toLocaleDateString(undefined, { year: "numeric", month: "long", day: "numeric" })
    : null;

  const platform = user.subscription_platform;
  const platformLabel =
    platform === "apple" ? "Apple App Store" :
    platform === "google" ? "Google Play" :
    platform === "stripe" ? "Stripe (web)" :
    platform || "—";

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between space-y-0">
        <div>
          <CardTitle>Subscription</CardTitle>
          <CardDescription>
            Manage your subscription in the store where you purchased it.
          </CardDescription>
        </div>
        {user.subscription_valid ? (
          <Badge className="bg-[#F7241D]">Pro</Badge>
        ) : (
          <Badge variant="secondary">Free</Badge>
        )}
      </CardHeader>
      <CardContent>
        <dl className="divide-y divide-gray-100 text-sm">
          <Row label="Plan" value={productLabel(user.subscription_product_id)} />
          <Row label="Status" value={user.subscription_valid ? "Active" : "Inactive"} />
          {user.subscription_valid && (
            <>
              <Row label={user.will_renew ? "Renews on" : "Expires on"} value={expiryLabel || "—"} />
              <Row label="Auto-renews" value={user.will_renew ? "Yes" : "No"} />
              <Row label="Managed by" value={platformLabel} />
            </>
          )}
        </dl>
      </CardContent>
    </Card>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between py-2.5">
      <dt className="text-gray-500">{label}</dt>
      <dd className="font-medium text-gray-900">{value}</dd>
    </div>
  );
}

// ----------------------------------------------------------------------------
// Danger zone — delete account
// ----------------------------------------------------------------------------

function DangerSection({ onDeleted }: { onDeleted: () => void }) {
  const [open, setOpen] = useState(false);
  const [password, setPassword] = useState("");
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState("");

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!password) return;
    setDeleting(true);
    setError("");
    try {
      await customerApi.deleteAccount(password);
      clearCustomerToken();
      onDeleted();
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : "Failed to delete account");
    } finally {
      setDeleting(false);
    }
  };

  return (
    <Card className="border-red-200">
      <CardHeader>
        <CardTitle className="text-red-600">Delete account</CardTitle>
        <CardDescription>
          Permanently deletes your profile, capcode subscriptions, alert zones and push
          registrations. This can't be undone.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Button variant="destructive" onClick={() => setOpen(true)}>
          Delete my account
        </Button>
      </CardContent>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete account?</DialogTitle>
            <DialogDescription>
              This permanently removes your account. An active App Store, Play Store or Stripe
              subscription will continue to bill until you cancel it where you purchased it.
            </DialogDescription>
          </DialogHeader>

          <form onSubmit={submit} className="space-y-4">
            <Separator />
            <div>
              <Label htmlFor="delete-pw">Confirm with your password</Label>
              <Input
                id="delete-pw"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="current-password"
                className="mt-1.5"
                autoFocus
              />
            </div>
            {error && <p className="text-sm text-red-600">{error}</p>}
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => setOpen(false)} disabled={deleting}>
                Cancel
              </Button>
              <Button type="submit" variant="destructive" disabled={!password || deleting}>
                {deleting ? "Deleting…" : "Delete account"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <CardContent className="border-t pt-4 mt-2">
        <p className="text-xs text-gray-400">
          Need help instead? <Link to="mailto:hello@rfspager.app" className="text-[#F7241D] hover:underline">Contact support</Link>.
        </p>
      </CardContent>
    </Card>
  );
}
