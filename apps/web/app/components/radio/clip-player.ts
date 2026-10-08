import { useCallback, useEffect, useRef, useState } from "react";

import type { RadioCall } from "./types";

/**
 * One <audio> element shared by every row: starting a clip stops the last.
 * Clips are fetched with a stream token in the URL, since <audio> can't send
 * an Authorization header.
 */
export function useClipPlayer(freshToken: () => Promise<string | null>) {
  const audio = useRef<HTMLAudioElement | null>(null);
  const [current, setCurrent] = useState<number | null>(null);
  const [playing, setPlaying] = useState(false);
  const [progress, setProgress] = useState(0);
  const [failed, setFailed] = useState<number | null>(null);

  useEffect(() => {
    const el = new Audio();
    el.preload = "auto";
    const onTime = () => setProgress(el.duration ? el.currentTime / el.duration : 0);
    const onPlay = () => setPlaying(true);
    const onPause = () => setPlaying(false);
    const onEnded = () => {
      setPlaying(false);
      setProgress(1);
    };
    el.addEventListener("timeupdate", onTime);
    el.addEventListener("play", onPlay);
    el.addEventListener("pause", onPause);
    el.addEventListener("ended", onEnded);
    audio.current = el;
    return () => {
      el.pause();
      el.removeAttribute("src");
    };
  }, []);

  const toggle = useCallback(
    async (call: RadioCall) => {
      const el = audio.current;
      if (!el || !call.audio_url) return;
      if (current === call.id) {
        if (el.paused) {
          if (el.ended) el.currentTime = 0;
          el.play().catch(() => setFailed(call.id));
        } else {
          el.pause();
        }
        return;
      }
      const token = await freshToken();
      if (!token) {
        setFailed(call.id);
        return;
      }
      setFailed(null);
      setCurrent(call.id);
      setProgress(0);
      el.src = `${call.audio_url}?token=${encodeURIComponent(token)}`;
      el.play().catch(() => setFailed(call.id));
    },
    [current, freshToken],
  );

  return { current, playing, progress, failed, toggle };
}
