import { ListFilter } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router";

import { DayDivider, SkeletonRows } from "~/components/pager/message-row";
import { dayKey, dayLabel } from "~/components/pager/time";
import { CallRow } from "~/components/radio/call-row";
import { useClipPlayer } from "~/components/radio/clip-player";
import { useLiveAudio } from "~/components/radio/live-audio";
import { OnAir } from "~/components/radio/on-air";
import { useStreamToken } from "~/components/radio/stream-token";
import { TalkgroupList } from "~/components/radio/talkgroup-list";
import { useRadioFeed } from "~/components/radio/use-radio-feed";
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetTrigger } from "~/components/ui/sheet";
import { getCustomerToken } from "~/lib/customer-api";
import { useIsSubscriber } from "~/lib/subscriber";
import type { RadioCall } from "~/components/radio/types";

const heading = "font-[family-name:var(--font-heading)]";
const STALE_ON_AIR_MS = 2 * 60_000;

export function meta() {
  return [
    { title: "Radio — RFSPager" },
    {
      name: "description",
      content: "Listen to Rural Fire Service and Fire and Rescue NSW radio live and read every call as text, decoded by our own receivers.",
    },
  ];
}

export default function Radio() {
  const subscriber = useIsSubscriber();
  const [signedIn, setSignedIn] = useState<boolean | null>(null);
  useEffect(() => setSignedIn(Boolean(getCustomerToken())), [subscriber]);

  if (subscriber === null || signedIn === null) {
    return <div className="flex-1 bg-gray-50" />;
  }
  if (!subscriber) {
    return <Paywall signedIn={signedIn} />;
  }
  return <RadioFeed />;
}

function RadioFeed() {
  const [searchParams, setSearchParams] = useSearchParams();
  const tgParam = Number(searchParams.get("tg")) || null;
  const { fresh } = useStreamToken(true);
  const feed = useRadioFeed({ enabled: true, tgid: tgParam, freshToken: fresh });
  // The same keyup patched onto two talkgroups arrives as two calls: same
  // radio, same moment. Live listening plays it once.
  const callsRef = useRef(new Map<number, { src: number | null; at: number }>());
  callsRef.current = new Map(feed.calls.map((c) => [c.id, { src: c.source_rid, at: Date.parse(c.started_at) }]));
  const isDuplicate = useCallback((a: number, b: number) => {
    const x = callsRef.current.get(a);
    const y = callsRef.current.get(b);
    return Boolean(x && y && x.src && x.src === y.src && Math.abs(x.at - y.at) < 2_000);
  }, []);
  const live = useLiveAudio({ tgid: tgParam, freshToken: fresh, isDuplicate });
  const player = useClipPlayer(fresh);
  const [sheetOpen, setSheetOpen] = useState(false);

  const select = (tgid: number | null) => {
    setSheetOpen(false);
    setSearchParams(tgid ? { tg: String(tgid) } : {}, { replace: true });
  };
  const selectedTG = feed.talkgroups.find((t) => t.tgid === tgParam) ?? null;
  const filterLabel = selectedTG?.alpha_tag ?? (tgParam ? `talkgroup ${tgParam}` : "every talkgroup we can hear");

  // Talkgroups on air right now, one chip each. A call whose end we never
  // heard about shouldn't sit on the strip forever.
  const active = useMemo(() => {
    const cutoff = Date.now() - STALE_ON_AIR_MS;
    const seen = new Set<number | null>();
    return withoutPatchCopies(feed.calls).filter((c) => {
      if (c.ended_at || Date.parse(c.started_at) < cutoff || seen.has(c.tgid)) return false;
      seen.add(c.tgid);
      return true;
    });
  }, [feed.calls]);

  // By default the timeline shows what you can listen to: recorded calls,
  // and clear calls still on air (their recording lands when they end).
  const [showAll, setShowAll] = useState(false);
  const shown = useMemo(() => {
    const calls = withoutPatchCopies(feed.calls);
    return showAll ? calls : calls.filter((c) => c.audio_url || (!c.ended_at && !c.encrypted));
  }, [feed.calls, showAll]);
  const hiddenCount = feed.calls.length - shown.length;

  const rows = useMemo(() => {
    const out: React.ReactNode[] = [];
    let lastDay = "";
    for (const call of shown) {
      const key = dayKey(call.started_at);
      if (key !== lastDay) {
        out.push(<DayDivider key={`day-${key}`} label={dayLabel(key)} />);
        lastDay = key;
      }
      out.push(
        <CallRow
          key={call.id}
          call={call}
          arrivedLive={feed.arrivedLive(call.id)}
          active={player.current === call.id}
          playing={player.current === call.id && player.playing}
          progress={player.current === call.id ? player.progress : 0}
          failed={player.failed === call.id}
          onToggle={player.toggle}
        />,
      );
    }
    return out;
  }, [shown, feed.arrivedLive, player.current, player.playing, player.progress, player.failed, player.toggle]);

  // Infinite scroll.
  const sentinel = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = sentinel.current;
    if (!el) return;
    const obs = new IntersectionObserver((e) => e.some((x) => x.isIntersecting) && feed.loadMore(), {
      rootMargin: "0px 0px 900px 0px",
    });
    obs.observe(el);
    return () => obs.disconnect();
  }, [feed.loadMore]);

  return (
    <div className="flex-1 bg-gray-50">
      <div className="mx-auto max-w-[1400px] md:flex md:items-start">
        <aside className="sticky top-16 hidden h-[calc(100dvh-4rem)] w-72 shrink-0 flex-col border-r border-gray-200 bg-white md:flex lg:w-80">
          <div className="px-6 pb-5 pt-7">
            <h1 className={`${heading} text-2xl font-extrabold tracking-tight text-gray-900`}>Radio</h1>
            <p className="mt-2 text-sm leading-relaxed text-gray-500">
              Rural Fire Service and Fire and Rescue NSW radio, decoded by our receivers. Listen live, or read
              and replay every call.
            </p>
          </div>
          <TalkgroupList
            talkgroups={feed.talkgroups}
            activeId={tgParam}
            onSelect={select}
            className="min-h-0 flex-1 space-y-px overflow-y-auto px-3 pb-6 pt-1"
          />
        </aside>

        <section className="min-w-0 flex-1 bg-white md:max-w-4xl md:border-r md:border-gray-200">
          <div className="sticky top-16 z-20 flex h-14 items-center gap-2 border-b border-gray-200 bg-white/95 px-4 backdrop-blur md:hidden">
            <h1 className={`${heading} flex-1 text-lg font-extrabold tracking-tight text-gray-900`}>Radio</h1>
            <Sheet open={sheetOpen} onOpenChange={setSheetOpen}>
              <SheetTrigger className="inline-flex h-10 max-w-[12rem] shrink-0 items-center gap-1.5 rounded-lg border border-gray-200 bg-white px-3 text-sm font-semibold text-gray-700 outline-none hover:bg-gray-50 focus-visible:ring-2 focus-visible:ring-[#F7241D]/40">
                <ListFilter className="size-4 shrink-0" />
                <span className="truncate">{selectedTG?.alpha_tag ?? "Talkgroups"}</span>
              </SheetTrigger>
              <SheetContent side="left" className="w-80 p-0">
                <SheetHeader className="px-6 pt-6">
                  <SheetTitle>Talkgroups</SheetTitle>
                </SheetHeader>
                <TalkgroupList
                  talkgroups={feed.talkgroups}
                  activeId={tgParam}
                  onSelect={select}
                  className="space-y-px overflow-y-auto px-3 pb-6"
                />
              </SheetContent>
            </Sheet>
          </div>

          <div className="md:sticky md:top-16 md:z-20">
            <OnAir connection={feed.connection} active={active} live={live} talkgroups={feed.talkgroups} filterLabel={filterLabel} />
          </div>
          <div className="flex items-center gap-3 border-b border-gray-200 bg-gray-50 px-4 py-2 text-[13px] text-gray-500 sm:px-6">
            <p className="min-w-0 flex-1 truncate">
              {showAll
                ? "Showing every call, including encrypted ones and calls we couldn't record."
                : hiddenCount > 0
                  ? `Showing calls you can play. ${hiddenCount} encrypted or unrecorded hidden.`
                  : "Showing calls you can play."}
            </p>
            <button
              type="button"
              onClick={() => setShowAll(!showAll)}
              className="shrink-0 rounded font-semibold text-gray-700 underline-offset-2 outline-none hover:underline focus-visible:ring-2 focus-visible:ring-[#F7241D]/40"
            >
              {showAll ? "Only playable" : "Show all"}
            </button>
          </div>

          {feed.loading ? (
            <SkeletonRows />
          ) : shown.length === 0 ? (
            <div className="px-6 py-16 text-center">
              <p className={`${heading} text-lg font-bold text-gray-900`}>No calls yet</p>
              <p className="mt-2 text-sm text-gray-500">
                {tgParam
                  ? "Nothing has been heard on this talkgroup. Pick another, or wait for the next call."
                  : "Calls appear here the moment our receivers hear them."}
              </p>
            </div>
          ) : (
            <ul>{rows}</ul>
          )}
          <div ref={sentinel} />
          {feed.loadingMore && <p className="py-6 text-center text-sm text-gray-400">Loading earlier calls…</p>}
        </section>
      </div>
    </div>
  );
}

function Paywall({ signedIn }: { signedIn: boolean }) {
  return (
    <div className="flex flex-1 items-start justify-center bg-gray-50 px-4 py-20">
      <div className="max-w-md">
        <h1 className={`${heading} text-3xl font-extrabold tracking-tight text-gray-900`}>Hear the radio behind the pages</h1>
        <p className="mt-4 text-[15px] leading-relaxed text-gray-600">
          Radio lets you listen to Rural Fire Service and Fire and Rescue NSW traffic live and read every call as text, from the same
          receivers that decode the pager feed. It's part of an RFSPager subscription.
        </p>
        <div className="mt-8 flex flex-wrap gap-3">
          {signedIn ? (
            <Link
              to="/subscribe"
              className="rounded-lg bg-[#F7241D] px-5 py-2.5 text-sm font-semibold text-white outline-none hover:bg-[#d91f19] focus-visible:ring-2 focus-visible:ring-[#F7241D]/40"
            >
              Subscribe
            </Link>
          ) : (
            <>
              <Link
                to="/login?next=/radio"
                className="rounded-lg bg-[#F7241D] px-5 py-2.5 text-sm font-semibold text-white outline-none hover:bg-[#d91f19] focus-visible:ring-2 focus-visible:ring-[#F7241D]/40"
              >
                Sign in
              </Link>
              <Link
                to="/subscribe"
                className="rounded-lg border border-gray-200 bg-white px-5 py-2.5 text-sm font-semibold text-gray-700 outline-none hover:bg-gray-50 focus-visible:ring-2 focus-visible:ring-[#F7241D]/40"
              >
                See plans
              </Link>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

/**
 * The server stores a patched keyup once, but a copy can show up live
 * before the server knows it's one (its talker only arrives with the call's
 * end). Same radio within two seconds: keep the first.
 */
function withoutPatchCopies(calls: RadioCall[]): RadioCall[] {
  const firstBySource = new Map<number, number[]>(); // source → start times kept
  const byAge = [...calls].sort((a, b) => a.id - b.id);
  const drop = new Set<number>();
  for (const c of byAge) {
    if (!c.source_rid) continue;
    const at = Date.parse(c.started_at);
    const kept = firstBySource.get(c.source_rid) ?? [];
    if (kept.some((t) => Math.abs(t - at) < 2_000)) drop.add(c.id);
    else firstBySource.set(c.source_rid, [...kept, at]);
  }
  return drop.size ? calls.filter((c) => !drop.has(c.id)) : calls;
}
