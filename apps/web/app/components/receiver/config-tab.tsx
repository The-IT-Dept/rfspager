import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { RefreshCw, Save } from "lucide-react";
import { Button } from "~/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "~/components/ui/card";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import {
  ConfigValidationError,
  errorMessage,
  getSystemConfig,
  putSystemConfig,
  validateRuntimeConfig,
  type ConfigResponse,
  type DongleInfo,
  type FieldErrors,
  type RuntimeConfig,
} from "~/lib/receiver-api";
import { fromDraft, sameConfig, toDraft, type ConfigDraft } from "./config-draft";
import { ConfigDefaults } from "./config-defaults";
import { ConfigForm } from "./config-form";
import { ConfigRevisions } from "./config-revisions";
import { fmtDate } from "./format";
import { ToneBadge, configStatusTone } from "./status-badge";

const FIELD_KEY_RE =
  /^(tuner_gain_tenths_db|ppm|sample_rate|dongle_serial|dongle_index|post_all|log_level|stats_interval_s|channels(\[\d+\]\.(label|freq_hz|enabled))?)$/;

/** How long to watch for the receiver's applied/rejected result after a save. */
const RESULT_WATCH_MS = 45_000;
const RESULT_POLL_MS = 3_000;

interface Props {
  systemId: string;
  dongles: DongleInfo[];
  onSaved?: (config: RuntimeConfig) => void;
}

export function ConfigTab({ systemId, dongles, onSaved }: Props) {
  const [data, setData] = useState<ConfigResponse | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [draft, setDraft] = useState<ConfigDraft | null>(null);
  const [loadedFrom, setLoadedFrom] = useState<string | null>(null);
  const [note, setNote] = useState("");
  const [errors, setErrors] = useState<FieldErrors>({});
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [savedVersion, setSavedVersion] = useState<number | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const watchUntilRef = useRef(0);

  const load = useCallback(
    async (resetDraft: boolean) => {
      try {
        const resp = await getSystemConfig(systemId);
        setData(resp);
        setLoadError(null);
        if (resetDraft) {
          setDraft(toDraft(resp.current?.config ?? resp.defaults));
          setLoadedFrom(null);
        }
      } catch (e: unknown) {
        setLoadError(errorMessage(e));
      }
    },
    [systemId],
  );

  useEffect(() => {
    void load(true);
  }, [load]);

  // After a save, re-read the status until the receiver reports on that revision or the watch expires.
  const appliedVersion = data?.applied?.version ?? null;
  useEffect(() => {
    if (savedVersion == null) return;
    if (appliedVersion != null && appliedVersion >= savedVersion) return;
    if (Date.now() >= watchUntilRef.current) return;
    const t = setTimeout(() => void load(false), RESULT_POLL_MS);
    return () => clearTimeout(t);
  }, [savedVersion, appliedVersion, data, load]);

  const dirty = useMemo(() => {
    if (!draft || !data) return false;
    const base = data.current?.config ?? data.defaults;
    return !sameConfig(fromDraft(draft).config, base);
  }, [draft, data]);

  const otherErrors = Object.entries(errors).filter(([k]) => !FIELD_KEY_RE.test(k));

  const save = async () => {
    if (!draft) return;
    const { config, errors: parseErrors } = fromDraft(draft);
    const clientErrors: FieldErrors = { ...validateRuntimeConfig(config), ...parseErrors };
    setErrors(clientErrors);
    setSaveError(null);
    setSavedVersion(null);
    if (Object.keys(clientErrors).length > 0) {
      setSaveError("Fix the highlighted fields first.");
      return;
    }
    setSaving(true);
    try {
      const resp = await putSystemConfig(systemId, { config, note: note.trim() || undefined });
      const saved = resp.current?.config ?? config;
      setData(resp);
      setErrors({});
      setNote("");
      setDraft(toDraft(saved));
      setLoadedFrom(null);
      setSavedVersion(resp.current?.version ?? null);
      watchUntilRef.current = Date.now() + RESULT_WATCH_MS;
      onSaved?.(saved);
    } catch (e: unknown) {
      if (e instanceof ConfigValidationError) {
        setErrors(e.errors);
        setSaveError(Object.keys(e.errors).length > 0 ? "The server rejected some fields." : e.message);
      } else {
        setSaveError(errorMessage(e));
      }
    } finally {
      setSaving(false);
    }
  };

  const refresh = async () => {
    setRefreshing(true);
    await load(false);
    setRefreshing(false);
  };

  if (loadError && !data) {
    return (
      <Card>
        <CardContent className="space-y-3">
          <p className="text-sm text-destructive">Could not load the configuration: {loadError}</p>
          <Button type="button" variant="outline" size="sm" onClick={() => void load(true)}>
            Retry
          </Button>
        </CardContent>
      </Card>
    );
  }
  if (!data || !draft) return <p className="text-sm text-muted-foreground">Loading configuration…</p>;

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle className="flex flex-wrap items-center justify-between gap-3">
            <span>Configuration</span>
            <Button type="button" variant="ghost" size="sm" onClick={() => void refresh()} disabled={refreshing}>
              <RefreshCw className={refreshing ? "animate-spin" : undefined} /> Refresh
            </Button>
          </CardTitle>
          <CardDescription>
            <AppliedStatus data={data} />
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-5">
          {loadError && <p className="text-sm text-destructive">Refresh failed: {loadError}</p>}
          {loadedFrom && (
            <p className="text-xs text-muted-foreground">Form loaded from {loadedFrom}. Save to make it the current revision.</p>
          )}

          <ConfigForm
            draft={draft}
            errors={errors}
            disabled={saving}
            dongles={dongles}
            onChange={(next) => {
              setDraft(next);
              setSavedVersion(null);
            }}
          />

          {otherErrors.length > 0 && (
            <ul className="list-disc pl-5 text-sm text-destructive">
              {otherErrors.map(([k, v]) => (
                <li key={k}>
                  <span className="font-mono">{k}</span>: {v}
                </li>
              ))}
            </ul>
          )}

          <div className="grid gap-3 sm:grid-cols-[1fr_auto] sm:items-end">
            <div className="space-y-1.5">
              <Label htmlFor="cfg-note">Note</Label>
              <Input
                id="cfg-note"
                value={note}
                onChange={(e) => setNote(e.target.value)}
                placeholder="Why this change (optional)"
                maxLength={200}
                disabled={saving}
              />
            </div>
            <Button type="button" onClick={() => void save()} disabled={saving}>
              <Save /> {saving ? "Saving…" : "Save revision"}
            </Button>
          </div>

          {saveError && <p className="text-sm text-destructive">{saveError}</p>}
          {savedVersion != null && !saveError && (
            <p className="text-sm text-emerald-700 dark:text-emerald-300">
              Saved revision {savedVersion}.{" "}
              {appliedVersion != null && appliedVersion >= savedVersion
                ? `The receiver reported ${data.applied?.status || "a result"}.`
                : "Waiting for the receiver to apply it…"}
            </p>
          )}
          {dirty ? (
            <p className="text-xs text-amber-700 dark:text-amber-300">Unsaved changes.</p>
          ) : (
            savedVersion == null && !saveError && <p className="text-xs text-muted-foreground">No unsaved changes.</p>
          )}
        </CardContent>
      </Card>

      <ConfigRevisions
        revisions={data.revisions}
        currentVersion={data.current?.version ?? null}
        applied={data.applied}
        disabled={saving}
        onLoad={(rev) => {
          setDraft(toDraft(rev.config));
          setErrors({});
          setSaveError(null);
          setSavedVersion(null);
          setLoadedFrom(`revision ${rev.version}`);
        }}
      />

      <ConfigDefaults
        defaults={data.defaults}
        current={data.current?.config ?? null}
        disabled={saving}
        onLoad={() => {
          setDraft(toDraft(data.defaults));
          setErrors({});
          setSaveError(null);
          setSavedVersion(null);
          setLoadedFrom("the defaults");
        }}
      />
    </div>
  );
}

function AppliedStatus({ data }: { data: ConfigResponse }) {
  const { current, applied, revisions } = data;
  if (!current) {
    return (
      <>
        No revision saved yet. The receiver runs its built-in defaults (or its env bootstrap) until one is saved; a protocol-2 receiver
        seeds revision 1 from its running config when it connects.
      </>
    );
  }
  const savedAt = fmtDate(current.created_at);
  if (!applied) {
    return <>Revision {current.version} saved {savedAt}. The receiver has not reported a result yet.</>;
  }
  const appliedRev = revisions.find((r) => r.version === applied.version);
  const badge = <ToneBadge tone={configStatusTone(applied.status)}>{applied.status || "reported"}</ToneBadge>;
  if (applied.version === current.version) {
    return (
      <span className="inline-flex flex-wrap items-center gap-2">
        Revision {applied.version}: {badge} <span>(saved {savedAt})</span>
      </span>
    );
  }
  return (
    <span className="inline-flex flex-wrap items-center gap-2">
      Revision {current.version} saved {savedAt}; the receiver still reports revision {applied.version} {badge}
      {appliedRev && <span>(saved {fmtDate(appliedRev.created_at)})</span>}. The newest revision is delivered on its next connection.
    </span>
  );
}
