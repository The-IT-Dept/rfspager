// Shapes returned by /api/v2/mobile/radio/*.

export interface RadioCall {
  id: number;
  sysid: number;
  rfss: number;
  site: number;
  site_id: number | null;
  site_name: string | null;
  tgid: number | null;
  tg_alpha_tag: string | null;
  tg_agency: string | null;
  tg_category: string | null;
  target_rid: number | null;
  target_alias: string | null;
  source_rid: number | null;
  source_alias: string | null;
  freq_hz: number | null;
  slot: number;
  encrypted: boolean;
  emergency: boolean;
  patch: boolean;
  started_at: string;
  ended_at: string | null;
  audio_url: string | null;
  audio_ms: number | null;
  transcript: string | null;
  transcribed_at: string | null;
  /** Why there's no transcript; "poor_audio" means too broken up to try. */
  transcript_error: string | null;
  /** Other talkgroups the same keyup went out on (a patch). */
  patched_with: string[] | null;
}

export interface CallPage {
  data: RadioCall[];
  has_more: boolean;
}

export interface Talkgroup {
  id: number;
  sysid: number;
  tgid: number;
  alpha_tag: string | null;
  description: string | null;
  agency: string | null;
  category: string | null;
  first_seen_at: string | null;
  last_seen_at: string | null;
  calls_clear: number;
  calls_encrypted: number;
}

/** One decoded voice chunk from /radio/live. */
export interface LiveChunk {
  call_id: string;
  call: number;
  tgid: number;
  seq: number;
  pcm: string; // base64, 8 kHz mono s16le
}
