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
  type?: "text" | "email" | "password" | "number" | "textarea" | "toggle" | "color" | "datetime" | "select" | "hidden";
  required?: boolean;
  disabled?: boolean;
  options?: { value: string; label: string }[];
  placeholder?: string;
  // Optional escape hatch: render a fully custom input for this field. The
  // render function controls value display + dispatch; the form still wraps
  // it in the standard Label/spacing and includes the value on submit.
  render?: (props: { value: any; onChange: (v: any) => void; disabled?: boolean }) => React.ReactNode;
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
