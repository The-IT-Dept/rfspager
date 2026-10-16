import { useState, type FormEvent, type ReactNode } from "react";
import { Check, ChevronRight, Lock, Pencil, Plus, Send, Trash2, TriangleAlert } from "lucide-react";
import { toast } from "../components/toast";
import {
  Button,
  CELL_LABEL,
  Dialog,
  EmptyRow,
  ErrorNote,
  Input,
  Panel,
  Pill,
  Switch,
  Table,
  type Tone,
} from "../components/ui";
import { ApiError, api, errorMessage } from "../lib/api";
import { cn, fmtAgo, fmtDuration, fmtInt, urlHost } from "../lib/format";
import { useInterval, useLoad, useNow } from "../lib/hooks";
import type { Endpoint, Webhook, WebhookInput, WebhookTestResult } from "../lib/types";

const MAX_WEBHOOKS = 20;

export function ConnectionsTab() {
  const conn = useLoad(() => api.connections(), []);
  useInterval(conn.reload, 5000);
  const now = useNow();

  const [editing, setEditing] = useState<Webhook | "new" | null>(null);
  const [deleting, setDeleting] = useState<Webhook | null>(null);

  const endpoints = conn.data?.endpoints ?? [];
  const webhooks = conn.data?.webhooks ?? [];

  const replaceHook = (w: Webhook) =>
    conn.setData((d) => {
      if (!d) return d;
      const list = d.webhooks ?? [];
      const has = list.some((x) => x.id === w.id);
      return { ...d, webhooks: has ? list.map((x) => (x.id === w.id ? w : x)) : [...list, w] };
    });

  return (
    <div className="space-y-3">
      {conn.error && !conn.data && <ErrorNote>Could not load connections: {conn.error}</ErrorNote>}

      <Panel title="Upstream" aside="Managed by RFSPager">
        <Table stack>
          <thead>
            <tr>
              <th>Endpoint</th>
              <th>Status</th>
              <th className="text-right">Ping</th>
              <th className="text-right">Reconnects</th>
              <th className="text-right">Queue</th>
              <th className="text-right">Pages sent</th>
              <th>Last page</th>
              <th>Last error</th>
            </tr>
          </thead>
          <tbody>
            {conn.data && endpoints.length === 0 && <EmptyRow colSpan={8}>No upstream endpoints configured.</EmptyRow>}
            {!conn.data && !conn.error && <EmptyRow colSpan={8}>Loading…</EmptyRow>}
            {endpoints.map((e) => (
              <EndpointRow key={e.name} e={e} now={now} />
            ))}
          </tbody>
        </Table>
        <p className="mt-2 text-[11px] text-muted-foreground">Counters start at zero each time the receiver restarts.</p>
      </Panel>

      <Panel
        title="Your webhooks"
        aside={
          <Button
            variant="primary"
            onClick={() => setEditing("new")}
            disabled={!conn.data || webhooks.length >= MAX_WEBHOOKS}
            title={webhooks.length >= MAX_WEBHOOKS ? `At most ${MAX_WEBHOOKS} webhooks` : undefined}
          >
            <Plus /> Add webhook
          </Button>
        }
      >
        <p className="mb-2 text-xs text-muted-foreground">
          Forward every page this receiver sends upstream to your own system: Node-RED, Home Assistant, a script on
          your network. Each webhook has its own queue, so a slow one never delays decoding.
        </p>
        <Table stack>
          <thead>
            <tr>
              <th>Webhook</th>
              <th>Status</th>
              <th>Signed</th>
              <th className="text-right">Sent</th>
              <th className="md:max-lg:hidden">Last delivery</th>
              <th>Last error</th>
              <th className="text-right">
                <span className="sr-only">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {conn.data && webhooks.length === 0 && (
              <EmptyRow colSpan={7}>No webhooks yet. Add one to get pages pushed to your own systems.</EmptyRow>
            )}
            {!conn.data && !conn.error && <EmptyRow colSpan={7}>Loading…</EmptyRow>}
            {webhooks.map((w) => (
              <WebhookRow
                key={w.id}
                w={w}
                now={now}
                onEdit={() => setEditing(w)}
                onDelete={() => setDeleting(w)}
                onChanged={replaceHook}
                reload={conn.reload}
              />
            ))}
          </tbody>
        </Table>
        <PayloadHelp />
      </Panel>

      <WebhookDialog
        key={editing === "new" ? "new" : editing?.id ?? "none"}
        hook={editing}
        onClose={() => setEditing(null)}
        onSaved={(w) => {
          replaceHook(w);
          setEditing(null);
          conn.reload();
        }}
      />

      <DeleteDialog
        hook={deleting}
        onClose={() => setDeleting(null)}
        onDeleted={(id) => {
          conn.setData((d) => (d ? { ...d, webhooks: (d.webhooks ?? []).filter((x) => x.id !== id) } : d));
          setDeleting(null);
          conn.reload();
        }}
      />
    </div>
  );
}

function EndpointRow({ e, now }: { e: Endpoint; now: number }) {
  const legacy = e.mode === "legacy";
  let tone: Tone;
  let label: string;
  if (legacy) {
    tone = e.connected ? "ok" : e.pages_sent + e.pages_failed > 0 ? "bad" : "muted";
    label = e.connected ? "Last post OK" : e.pages_sent + e.pages_failed > 0 ? "Last post failed" : "No posts yet";
  } else if (e.connected) {
    tone = "ok";
    label = e.connected_since ? `Connected ${fmtDuration((now - e.connected_since) / 1000)}` : "Connected";
  } else {
    tone = "bad";
    label = "Disconnected";
  }
  return (
    <tr>
      <td className="min-w-40 max-md:w-full">
        <div className="font-semibold">{e.name}</div>
        <div className="text-[11px] text-muted-foreground">
          {e.primary && "primary · "}
          {e.mode} · <span title={e.url}>{urlHost(e.url)}</span>
        </div>
      </td>
      <td>
        <Pill tone={tone}>{label}</Pill>
      </td>
      <td data-label="Ping" className={cn("text-right font-mono whitespace-nowrap tabular-nums", CELL_LABEL)}>
        {legacy || !e.rtt_ms ? "—" : `${e.rtt_ms} ms`}
      </td>
      <td data-label="Reconnects" className={cn("text-right font-mono tabular-nums", CELL_LABEL)}>
        {legacy ? "—" : fmtInt(e.reconnects_total)}
      </td>
      <td
        data-label="Queue"
        className={cn("text-right font-mono tabular-nums", CELL_LABEL)}
        title={e.dropped_frames_total ? `${e.dropped_frames_total} frames dropped` : undefined}
      >
        {legacy ? "—" : fmtInt(e.queue_depth)}
      </td>
      <td data-label="Sent" className={cn("text-right font-mono whitespace-nowrap tabular-nums", CELL_LABEL)}>
        {fmtInt(e.pages_sent)}
        {e.pages_failed > 0 && <span className="text-red-700"> / {fmtInt(e.pages_failed)} failed</span>}
      </td>
      <td data-label="Last page" className={cn("whitespace-nowrap", CELL_LABEL)}>
        {e.last_page_at ? fmtAgo(e.last_page_at, now) : <span className="text-muted-foreground">never</span>}
      </td>
      <td className={cn("min-w-40 text-[11.5px] max-md:w-full", !e.last_error && "max-md:hidden")}>
        {e.last_error ? (
          <span className="text-red-800">
            {e.last_error}
            {e.last_error_at && <span className="text-muted-foreground"> · {fmtAgo(e.last_error_at, now)}</span>}
          </span>
        ) : (
          <span className="text-muted-foreground">none</span>
        )}
      </td>
    </tr>
  );
}

const STATE_PILL: Record<Webhook["state"], { tone: Tone; label: string }> = {
  ok: { tone: "ok", label: "OK" },
  failing: { tone: "bad", label: "Failing" },
  idle: { tone: "muted", label: "Waiting for a page" },
  disabled: { tone: "muted", label: "Disabled" },
};

function WebhookRow({
  w,
  now,
  onEdit,
  onDelete,
  onChanged,
  reload,
}: {
  w: Webhook;
  now: number;
  onEdit: () => void;
  onDelete: () => void;
  onChanged: (w: Webhook) => void;
  reload: () => void;
}) {
  const [testing, setTesting] = useState(false);
  const [test, setTest] = useState<(WebhookTestResult & { at: number }) | { error: string; at: number } | null>(null);
  const [toggling, setToggling] = useState(false);
  const pill = STATE_PILL[w.state] ?? { tone: "muted" as Tone, label: w.state };

  const sendTest = async () => {
    setTesting(true);
    try {
      const r = await api.testWebhook(w.id);
      setTest({ ...r, at: Date.now() });
      if (r.ok) toast("ok", "Test delivered", `${w.name || urlHost(w.url)} answered ${r.status} in ${r.duration_ms} ms`);
      else toast("bad", "Test failed", r.error || (r.status ? `HTTP ${r.status}` : "No answer"));
    } catch (e) {
      setTest({ error: errorMessage(e), at: Date.now() });
      toast("bad", "Test failed", errorMessage(e));
    } finally {
      setTesting(false);
      reload();
    }
  };

  const toggle = async (enabled: boolean) => {
    setToggling(true);
    try {
      // Secret left out: keep the current one.
      const updated = await api.updateWebhook(w.id, { name: w.name, url: w.url, enabled });
      onChanged(updated);
    } catch (e) {
      toast("bad", enabled ? "Could not enable webhook" : "Could not disable webhook", errorMessage(e));
    } finally {
      setToggling(false);
    }
  };

  return (
    <tr className={cn(!w.enabled && "text-zinc-500")}>
      <td className="max-w-[22rem] min-w-44 max-md:w-full max-md:max-w-none">
        {w.name && <div className="font-semibold">{w.name}</div>}
        <div className={cn("font-mono text-[11.5px] break-all", w.name ? "text-muted-foreground" : "text-foreground")}>{w.url}</div>
        {test && <TestResult r={test} now={now} />}
      </td>
      <td>
        <Pill tone={pill.tone}>{pill.label}</Pill>
      </td>
      <td>
        {w.has_secret ? (
          <span className="inline-flex items-center gap-1" title="Signed with HMAC-SHA256 (X-RFSPager-Signature)">
            <Lock className="size-3 text-muted-foreground" /> HMAC
          </span>
        ) : (
          <span className="text-muted-foreground">unsigned</span>
        )}
      </td>
      <td data-label="Sent" className={cn("text-right font-mono whitespace-nowrap tabular-nums", CELL_LABEL)}>
        {fmtInt(w.sent)}
        {w.failed > 0 && <span className="text-red-700"> / {fmtInt(w.failed)} failed</span>}
      </td>
      <td data-label="Last OK" className={cn("whitespace-nowrap md:max-lg:hidden", CELL_LABEL)}>
        {w.last_ok_at ? (
          <>
            {fmtAgo(w.last_ok_at, now)}
            {w.last_status > 0 && <span className="text-muted-foreground"> · {w.last_status}</span>}
          </>
        ) : (
          <span className="text-muted-foreground">never</span>
        )}
      </td>
      <td className={cn("min-w-40 text-[11.5px] max-md:w-full", !w.last_error && "max-md:hidden")}>
        {w.last_error ? (
          <span className="text-red-800">
            {w.last_error}
            {w.last_error_at && <span className="text-muted-foreground"> · {fmtAgo(w.last_error_at, now)}</span>}
          </span>
        ) : (
          <span className="text-muted-foreground">none</span>
        )}
      </td>
      <td className="max-md:w-full">
        <div className="flex items-center gap-1.5 md:justify-end">
          <Switch checked={w.enabled} onChange={toggle} disabled={toggling} label={<span className="sr-only">Enabled</span>} />
          <Button onClick={sendTest} disabled={testing} title="Send one test event now">
            <Send /> {testing ? "Sending…" : "Send test"}
          </Button>
          <Button onClick={onEdit} title="Edit">
            <Pencil /> Edit
          </Button>
          <Button variant="ghost" onClick={onDelete} title="Delete" aria-label="Delete" className="hover:text-red-700">
            <Trash2 />
          </Button>
        </div>
      </td>
    </tr>
  );
}

function TestResult({
  r,
  now,
}: {
  r: (WebhookTestResult & { at: number }) | { error: string; at: number };
  now: number;
}) {
  const ok = "ok" in r && r.ok;
  return (
    <p className={cn("mt-1 inline-flex flex-wrap items-center gap-1 text-[11px]", ok ? "text-green-700" : "text-red-700")}>
      {ok ? <Check className="size-3" /> : <TriangleAlert className="size-3" />}
      {"ok" in r ? (
        ok ? (
          <>Test OK · {r.status} in {r.duration_ms} ms</>
        ) : (
          <>
            Test failed · {r.error || (r.status ? `HTTP ${r.status}` : "no answer")}
            {r.status > 0 && r.error && !r.error.includes(String(r.status)) ? ` (HTTP ${r.status})` : ""}
          </>
        )
      ) : (
        <>Test failed · {r.error}</>
      )}
      <span className="text-muted-foreground">· {fmtAgo(r.at, now)}</span>
    </p>
  );
}

function WebhookDialog({
  hook,
  onClose,
  onSaved,
}: {
  hook: Webhook | "new" | null;
  onClose: () => void;
  onSaved: (w: Webhook) => void;
}) {
  const existing = hook && hook !== "new" ? hook : null;
  const [name, setName] = useState(existing?.name ?? "");
  const [url, setUrl] = useState(existing?.url ?? "");
  const [secret, setSecret] = useState("");
  const [removeSecret, setRemoveSecret] = useState(false);
  const [enabled, setEnabled] = useState(existing?.enabled ?? true);
  const [saving, setSaving] = useState(false);
  const [fields, setFields] = useState<Record<string, string>>({});
  const [error, setError] = useState<string | null>(null);

  const submit = async (e?: FormEvent) => {
    e?.preventDefault();
    setSaving(true);
    setFields({});
    setError(null);
    const body: WebhookInput = { name: name.trim(), url: url.trim(), enabled };
    if (!existing) {
      body.secret = secret;
    } else if (removeSecret) {
      body.secret = "";
    } else if (secret) {
      body.secret = secret;
    }
    try {
      const w = existing ? await api.updateWebhook(existing.id, body) : await api.createWebhook(body);
      toast("ok", existing ? "Webhook saved" : "Webhook added", w.name || urlHost(w.url));
      onSaved(w);
    } catch (err) {
      if (err instanceof ApiError && err.status === 422 && Object.keys(err.fields).length) {
        setFields(err.fields);
        const known = ["name", "url", "secret", "enabled"];
        const other = Object.entries(err.fields).filter(([k]) => !known.includes(k));
        if (other.length) setError(other.map(([, v]) => v).join("; "));
      } else {
        setError(errorMessage(err));
      }
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog
      open={hook !== null}
      onClose={onClose}
      title={existing ? "Edit webhook" : "Add webhook"}
      footer={
        <>
          <Button size="md" onClick={onClose}>
            Cancel
          </Button>
          <Button size="md" variant="primary" onClick={() => submit()} disabled={saving}>
            {saving ? "Saving…" : existing ? "Save" : "Add webhook"}
          </Button>
        </>
      }
    >
      <form onSubmit={submit} className="space-y-4" noValidate>
        {error && <ErrorNote>{error}</ErrorNote>}
        <Field label="Name" hint="Optional, up to 60 characters" error={fields.name} htmlFor="wh-name">
          <Input
            id="wh-name"
            value={name}
            maxLength={60}
            onChange={(e) => setName(e.target.value)}
            placeholder="Node-RED"
            invalid={!!fields.name}
            autoFocus
          />
        </Field>
        <Field label="URL" hint="Where pages are POSTed as JSON. http:// or https://" error={fields.url} htmlFor="wh-url">
          <Input
            id="wh-url"
            type="url"
            inputMode="url"
            autoCapitalize="off"
            autoCorrect="off"
            spellCheck={false}
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            placeholder="http://192.168.1.20:1880/pages"
            invalid={!!fields.url}
            className="font-mono text-[12.5px]"
          />
        </Field>
        <Field
          label="Secret"
          hint={
            existing?.has_secret
              ? "Leave blank to keep the current secret."
              : "Optional. When set, each request carries an X-RFSPager-Signature HMAC so you can check it came from this receiver."
          }
          error={fields.secret}
          htmlFor="wh-secret"
        >
          <Input
            id="wh-secret"
            type="password"
            autoComplete="new-password"
            value={secret}
            disabled={removeSecret}
            onChange={(e) => setSecret(e.target.value)}
            placeholder={existing?.has_secret ? "•••••••• (unchanged)" : "No secret"}
            invalid={!!fields.secret}
          />
        </Field>
        {existing?.has_secret && (
          <div className="-mt-2">
            <label className="inline-flex items-center gap-2 text-xs">
              <input
                type="checkbox"
                checked={removeSecret}
                onChange={(e) => {
                  setRemoveSecret(e.target.checked);
                  if (e.target.checked) setSecret("");
                }}
                className="size-3.5 accent-zinc-900"
              />
              Remove the secret (stop signing requests)
            </label>
          </div>
        )}
        <div>
          <Switch checked={enabled} onChange={setEnabled} label={<span className="text-[13px]">Enabled</span>} />
          {fields.enabled && <p className="mt-1 text-xs text-red-700">{fields.enabled}</p>}
        </div>
        {/* Enter submits. */}
        <button type="submit" className="hidden" />
      </form>
    </Dialog>
  );
}

function Field({
  label,
  hint,
  error,
  htmlFor,
  children,
}: {
  label: string;
  hint?: ReactNode;
  error?: string;
  htmlFor: string;
  children: ReactNode;
}) {
  return (
    <div>
      <label htmlFor={htmlFor} className="mb-1 block text-xs font-semibold">
        {label}
      </label>
      {children}
      {error ? (
        <p className="mt-1 text-xs text-red-700" role="alert">
          {error}
        </p>
      ) : (
        hint && <p className="mt-1 text-[11px] text-muted-foreground">{hint}</p>
      )}
    </div>
  );
}

function DeleteDialog({
  hook,
  onClose,
  onDeleted,
}: {
  hook: Webhook | null;
  onClose: () => void;
  onDeleted: (id: string) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const del = async () => {
    if (!hook) return;
    setBusy(true);
    setError(null);
    try {
      await api.deleteWebhook(hook.id);
      toast("ok", "Webhook deleted", hook.name || urlHost(hook.url));
      onDeleted(hook.id);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      open={hook !== null}
      onClose={() => {
        setError(null);
        onClose();
      }}
      title="Delete webhook?"
      footer={
        <>
          <Button size="md" onClick={onClose}>
            Cancel
          </Button>
          <Button size="md" variant="danger" onClick={del} disabled={busy}>
            {busy ? "Deleting…" : "Delete"}
          </Button>
        </>
      }
    >
      {error && <ErrorNote className="mb-3">{error}</ErrorNote>}
      <p>
        Pages will stop being sent to{" "}
        <span className="font-mono text-[12px] break-all">{hook?.url}</span>
        {hook?.name ? ` (${hook.name})` : ""}. Pages already queued for it are discarded.
      </p>
    </Dialog>
  );
}

const SAMPLE_BODY = `{
  "event": "page",
  "receiver": {"hostname": "rfspager-bud-1", "system": "Budgewoi", "version": "5.9.0-abc1234"},
  "page": {
    "id": 18231,
    "address": "0521839",
    "message": "RFS: GRASS FIRE ...",
    "time": "2026-10-16T14:02:31+11:00",
    "timestamp": 1792123351,
    "channel": "RFS",
    "freq_hz": 148587500,
    "baud": 512,
    "function": 3,
    "quality": {"codewords": 12, "corrected": 1, "uncorrectable": 0}
  }
}`;

const SAMPLE_HEADERS = `POST <your URL>
Content-Type: application/json
User-Agent: rfspager-receiver/<version>
X-RFSPager-Event: page            ("test" from the Send test button)
X-RFSPager-Delivery: <uuid>       (same across retries of one delivery)
X-RFSPager-Signature: sha256=<hex HMAC-SHA256 of the raw body>   (only with a secret)`;

const SAMPLE_VERIFY = `import hmac, hashlib
expected = "sha256=" + hmac.new(secret.encode(), body, hashlib.sha256).hexdigest()
ok = hmac.compare_digest(expected, request.headers["X-RFSPager-Signature"])`;

function PayloadHelp() {
  return (
    <details className="group mt-3 rounded-md border border-border bg-zinc-50/60">
      <summary className="flex cursor-pointer list-none items-center gap-1.5 px-3 py-2 text-xs font-semibold select-none [&::-webkit-details-marker]:hidden">
        <ChevronRight className="size-3.5 transition-transform group-open:rotate-90" />
        What gets sent
      </summary>
      <div className="space-y-3 border-t border-border px-3 py-3 text-xs">
        <p className="text-muted-foreground">
          One JSON <code className="font-mono">POST</code> per page, after the same content filter as the upload to
          RFSPager. Any 2xx answer is success. Anything else, or no answer within 10 s, is retried after 2 s, 10 s and
          30 s, then counted as failed. In Node-RED use an <b>http in</b> node (method POST); in Home Assistant a{" "}
          <b>webhook</b> automation trigger, with the page under <code className="font-mono">trigger.json.page</code>.
        </p>
        <Code label="Headers">{SAMPLE_HEADERS}</Code>
        <Code label="Body">{SAMPLE_BODY}</Code>
        <Code label="Checking the signature (Python)">{SAMPLE_VERIFY}</Code>
        <p className="text-muted-foreground">
          <code className="font-mono">time</code> is ISO 8601 in the receiver's time zone,{" "}
          <code className="font-mono">timestamp</code> unix seconds. <code className="font-mono">quality</code> counts
          message codewords repaired (<code className="font-mono">corrected</code>) and beyond repair (
          <code className="font-mono">uncorrectable</code>); a non-zero uncorrectable means some characters may be wrong.
        </p>
      </div>
    </details>
  );
}

function Code({ label, children }: { label: string; children: string }) {
  return (
    <div>
      <p className="mb-1 text-[10.5px] font-semibold tracking-[0.04em] text-muted-foreground uppercase">{label}</p>
      <pre className="overflow-x-auto rounded-md border border-border bg-white p-2.5 font-mono text-[11.5px] leading-relaxed">
        {children}
      </pre>
    </div>
  );
}

