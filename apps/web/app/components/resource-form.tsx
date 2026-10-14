import { useState } from "react";
import { useNavigate } from "react-router";
import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { Textarea } from "~/components/ui/textarea";
import { Switch } from "~/components/ui/switch";
import { Card, CardContent, CardHeader, CardTitle } from "~/components/ui/card";
import { api } from "~/lib/api";

export interface FieldDef {
  name: string;
  label: string;
  // "datetime" is a plain text input holding the raw value (legacy). Use
  // "datetime-local" for a native picker: the value is shown in local time and
  // submitted as an ISO-8601 UTC string (unchanged values are sent back as-is).
  type?: "text" | "email" | "password" | "number" | "textarea" | "toggle" | "color" | "datetime" | "datetime-local" | "select" | "hidden";
  required?: boolean;
  disabled?: boolean;
  options?: { value: string; label: string }[];
  placeholder?: string;
  /** HTML `step` for number inputs, e.g. "any" for decimals. */
  step?: string;
  // Optional escape hatch: render a fully custom input for this field. The
  // render function controls value display + dispatch; the form still wraps
  // it in the standard Label/spacing and includes the value on submit.
  render?: (props: { value: any; onChange: (v: any) => void; disabled?: boolean }) => React.ReactNode;
}

function pad2(n: number): string {
  return String(n).padStart(2, "0");
}

/** ISO/RFC 3339 timestamp -> "YYYY-MM-DDTHH:mm" in local time for <input type="datetime-local">. */
function toLocalInput(v: unknown): string {
  if (v == null || v === "") return "";
  const d = new Date(String(v));
  if (Number.isNaN(d.getTime())) return "";
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}T${pad2(d.getHours())}:${pad2(d.getMinutes())}`;
}

interface ResourceFormProps {
  title: string;
  fields: FieldDef[];
  endpoint: string;
  method?: "POST" | "PUT";
  initialData?: Record<string, any>;
  backPath: string;
}

export function ResourceForm({ title, fields, endpoint, method = "POST", initialData, backPath }: ResourceFormProps) {
  const navigate = useNavigate();
  const [data, setData] = useState<Record<string, any>>(() => {
    const d: Record<string, any> = {};
    fields.forEach((f) => {
      if (f.type === "toggle") d[f.name] = initialData?.[f.name] ?? false;
      else if (f.type === "datetime-local") d[f.name] = toLocalInput(initialData?.[f.name]);
      else d[f.name] = initialData?.[f.name] ?? "";
    });
    return d;
  });
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    setError("");
    try {
      const payload: Record<string, any> = {};
      fields.forEach((f) => {
        if (f.disabled) return;
        const v = data[f.name];
        const initial = initialData?.[f.name];
        const isBlank = v === "" || v === undefined || v === null;
        if (f.type === "number") {
          if (!isBlank) payload[f.name] = Number(v);
          else if (initial != null) payload[f.name] = null; // explicitly cleared
        } else if (f.type === "toggle") {
          payload[f.name] = !!v;
        } else if (f.type === "datetime-local") {
          if (isBlank) {
            if (initial != null && initial !== "") payload[f.name] = null; // explicitly cleared
          } else if (initial != null && v === toLocalInput(initial)) {
            payload[f.name] = initial; // untouched: keep full precision
          } else {
            const d = new Date(String(v)); // no offset in the string = local time
            if (Number.isNaN(d.getTime())) throw new Error(`${f.label}: invalid date`);
            payload[f.name] = d.toISOString();
          }
        } else if (!isBlank) {
          payload[f.name] = v;
        } else if (initial != null && initial !== "") {
          // Field used to have a value and the user blanked it — explicitly
          // null on the server side rather than silently keeping the old value.
          payload[f.name] = null;
        }
      });

      if (method === "PUT") await api.put(endpoint, payload);
      else await api.post(endpoint, payload);
      navigate(backPath);
    } catch (err: any) {
      setError(err.message || "Failed to save");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="max-w-2xl">
      <Card>
        <CardHeader>
          <CardTitle>{title}</CardTitle>
        </CardHeader>
        <CardContent>
          {error && <p className="text-destructive text-sm mb-4">{error}</p>}
          <form onSubmit={handleSubmit} className="space-y-4">
            {fields.filter(f => f.type !== "hidden").map((field) => (
              <div key={field.name} className="space-y-2">
                <Label htmlFor={field.name}>{field.label}</Label>
                {field.render ? (
                  field.render({
                    value: data[field.name],
                    onChange: (v) => setData({ ...data, [field.name]: v }),
                    disabled: field.disabled,
                  })
                ) : field.type === "textarea" ? (
                  <Textarea id={field.name} value={data[field.name] || ""} disabled={field.disabled}
                    onChange={(e) => setData({ ...data, [field.name]: e.target.value })} />
                ) : field.type === "toggle" ? (
                  <div><Switch id={field.name} checked={!!data[field.name]} disabled={field.disabled}
                    onCheckedChange={(v) => setData({ ...data, [field.name]: v })} /></div>
                ) : field.type === "select" ? (
                  <select id={field.name} value={data[field.name] || ""} disabled={field.disabled}
                    className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-sm"
                    onChange={(e) => setData({ ...data, [field.name]: e.target.value })}>
                    <option value="">Select...</option>
                    {field.options?.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
                  </select>
                ) : (
                  <Input id={field.name} type={field.type || "text"} value={data[field.name] ?? ""}
                    disabled={field.disabled} required={field.required} placeholder={field.placeholder}
                    step={field.step}
                    onChange={(e) => setData({ ...data, [field.name]: e.target.value })} />
                )}
              </div>
            ))}
            <div className="flex gap-2 pt-4">
              <Button type="submit" disabled={saving}>{saving ? "Saving..." : "Save"}</Button>
              <Button type="button" variant="outline" onClick={() => navigate(backPath)}>Cancel</Button>
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
