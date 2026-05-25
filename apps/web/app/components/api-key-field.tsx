import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";

// Pagermon-style key: 24 chars, uppercase alphanumeric (0-9 + A-Z).
// Generated client-side from crypto.getRandomValues — these keys are
// per-system ingress tokens, public-ish (clients on user-controlled boxes
// hold them), so there's no secrecy concern with browser-side generation.
const KEY_LEN = 24;
const ALPHABET = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ";

function generateKey(): string {
  const bytes = new Uint8Array(KEY_LEN);
  crypto.getRandomValues(bytes);
  let out = "";
  for (const b of bytes) out += ALPHABET[b % ALPHABET.length];
  return out;
}

interface Props {
  value: string;
  onChange: (v: string) => void;
  disabled?: boolean;
}

export function ApiKeyField({ value, onChange, disabled }: Props) {
  return (
    <div className="space-y-2">
      <div className="flex gap-2">
        <Input
          type="text"
          value={value ?? ""}
          disabled={disabled}
          placeholder="empty = ingress disabled"
          className="font-mono"
          onChange={(e) => onChange(e.target.value)}
        />
        <Button
          type="button"
          variant="outline"
          disabled={disabled}
          onClick={() => onChange(generateKey())}
        >
          Generate
        </Button>
        <Button
          type="button"
          variant="outline"
          disabled={disabled || !value}
          onClick={() => navigator.clipboard.writeText(value ?? "")}
        >
          Copy
        </Button>
        <Button
          type="button"
          variant="ghost"
          disabled={disabled || !value}
          onClick={() => onChange("")}
        >
          Clear
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        Pagermon clients POST to <code>/api/messages</code> with this in the <code>apikey</code> header.
        Blank to disable ingress for this system.
      </p>
    </div>
  );
}
