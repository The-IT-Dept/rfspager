import { useId, useMemo, useState, type KeyboardEvent, type PointerEvent } from "react";
import { cn } from "~/lib/utils";
import {
  SERIES_VARS_CLASS,
  fmtTimeFull,
  fmtTimeTick,
  nearestIndex,
  niceTicks,
  scaleLinear,
  seriesColor,
  timeTicks,
  useMeasure,
} from "./chart-utils";

export interface LineSeries {
  key: string;
  label: string;
  /** Fixed categorical slot (0..3). Follows the channel, never its position in the list. */
  slot: number;
  /** One value per entry of `times`; null is a gap. */
  values: (number | null)[];
}

export interface Threshold {
  value: number;
  label: string;
}

interface Props {
  /** Sample timestamps (ms), ascending, shared by every series. */
  times: number[];
  series: LineSeries[];
  yLabel: string;
  yFormat?: (v: number) => string;
  yDomain?: [number, number];
  height?: number;
  thresholds?: Threshold[];
  /** Hold the previous render at reduced opacity while new data loads. */
  dimmed?: boolean;
  emptyText?: string;
}

const PAD = { top: 14, bottom: 34, left: 48, right: 72 };
const END_LABEL_GAP = 12;

const defaultFormat = (v: number) => String(Math.round(v * 10) / 10);

/**
 * Multi-series time line chart: 2px lines with gaps for missing samples,
 * hairline grid, crosshair tooltip listing every series, keyboard stepping,
 * a legend for two or more series, selective end labels, and a table twin.
 */
export function LineChart({
  times,
  series,
  yLabel,
  yFormat = defaultFormat,
  yDomain,
  height = 220,
  thresholds = [],
  dimmed,
  emptyText = "No samples in this range.",
}: Props) {
  const [ref, width] = useMeasure<HTMLDivElement>();
  const [hover, setHover] = useState<number | null>(null);
  const clipId = useId();

  const n = times.length;
  const hasData = n > 0 && series.some((s) => s.values.some((v) => v != null && Number.isFinite(v)));

  const { y0, y1, yTicks } = useMemo(() => computeYDomain(series, thresholds, yDomain), [series, thresholds, yDomain]);

  const plot = {
    x0: PAD.left,
    x1: Math.max(PAD.left + 20, width - PAD.right),
    y0: PAD.top,
    y1: height - PAD.bottom,
  };
  const t0 = n ? times[0] : 0;
  const t1 = n ? (times[n - 1] > t0 ? times[n - 1] : t0 + 60_000) : 1;
  const x = scaleLinear([t0, t1], [plot.x0, plot.x1]);
  const y = scaleLinear([y0, y1], [plot.y1, plot.y0]);
  const xTicks = n ? timeTicks(t0, t1) : [];

  const paths = series.map((s) => buildPath(s.values, times, x, y));

  // Last real point of each series carries a marker and, when they do not
  // collide, a direct label. Colliding labels fall back to the legend.
  const ends = series.map((s) => {
    let i = s.values.length - 1;
    while (i >= 0 && !isNum(s.values[i])) i--;
    return i >= 0 ? { i, v: s.values[i] as number } : null;
  });
  const endYs = ends.flatMap((e) => (e ? [y(e.v)] : [])).sort((a, b) => a - b);
  const collide = endYs.some((v, i) => i > 0 && v - endYs[i - 1] < END_LABEL_GAP);
  const showEndLabels = !collide && series.length <= 4;

  const hoverIdx = hover != null && hover >= 0 && hover < n ? hover : null;
  const hx = hoverIdx != null ? x(times[hoverIdx]) : 0;
  const flip = hx > plot.x0 + (plot.x1 - plot.x0) * 0.6;

  const onMove = (e: PointerEvent<SVGSVGElement>) => {
    if (!n) return;
    const rect = e.currentTarget.getBoundingClientRect();
    const frac = (e.clientX - rect.left - plot.x0) / (plot.x1 - plot.x0);
    const t = t0 + Math.min(1, Math.max(0, frac)) * (t1 - t0);
    setHover(nearestIndex(times, t));
  };

  const onKey = (e: KeyboardEvent<SVGSVGElement>) => {
    if (!n) return;
    switch (e.key) {
      case "ArrowLeft":
        setHover((h) => Math.max(0, (h ?? n) - 1));
        break;
      case "ArrowRight":
        setHover((h) => Math.min(n - 1, (h ?? -1) + 1));
        break;
      case "Home":
        setHover(0);
        break;
      case "End":
        setHover(n - 1);
        break;
      case "Escape":
        setHover(null);
        break;
      default:
        return;
    }
    e.preventDefault();
  };

  return (
    <div className={cn(SERIES_VARS_CLASS, "transition-opacity", dimmed && "opacity-60")}>
      <div ref={ref} className="relative w-full" style={{ height }}>
        {width > 0 && (
          <svg
            width={width}
            height={height}
            className="block select-none overflow-visible rounded outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
            role="img"
            aria-label={`${yLabel} over time`}
            tabIndex={0}
            onPointerMove={onMove}
            onPointerLeave={() => setHover(null)}
            onKeyDown={onKey}
          >
            <defs>
              <clipPath id={clipId}>
                <rect x={plot.x0} y={plot.y0 - 4} width={Math.max(0, plot.x1 - plot.x0)} height={plot.y1 - plot.y0 + 8} />
              </clipPath>
            </defs>

            {yTicks.map((v) => (
              <g key={v}>
                <line x1={plot.x0} x2={plot.x1} y1={y(v)} y2={y(v)} className="stroke-border" strokeWidth={1} />
                <text x={plot.x0 - 6} y={y(v)} dy="0.32em" textAnchor="end" className="fill-muted-foreground text-[10px] tabular-nums">
                  {yFormat(v)}
                </text>
              </g>
            ))}
            <line x1={plot.x0} x2={plot.x1} y1={plot.y1} y2={plot.y1} className="stroke-border" strokeWidth={1} />

            {xTicks.map((t) => (
              <g key={t}>
                <line x1={x(t)} x2={x(t)} y1={plot.y1} y2={plot.y1 + 4} className="stroke-border" strokeWidth={1} />
                <text x={x(t)} y={plot.y1 + 15} textAnchor="middle" className="fill-muted-foreground text-[10px] tabular-nums">
                  {fmtTimeTick(t)}
                </text>
              </g>
            ))}

            <text
              transform={`translate(11 ${(plot.y0 + plot.y1) / 2}) rotate(-90)`}
              textAnchor="middle"
              className="fill-muted-foreground text-[10px]"
            >
              {yLabel}
            </text>
            <text x={(plot.x0 + plot.x1) / 2} y={height - 3} textAnchor="middle" className="fill-muted-foreground text-[10px]">
              Time
            </text>

            {thresholds.map((th) => (
              <g key={th.label}>
                <line
                  x1={plot.x0}
                  x2={plot.x1}
                  y1={y(th.value)}
                  y2={y(th.value)}
                  className="stroke-amber-500/70"
                  strokeWidth={1}
                  strokeDasharray="4 3"
                />
                <text x={plot.x1 - 2} y={y(th.value) - 3} textAnchor="end" className="fill-muted-foreground text-[10px]">
                  {th.label}
                </text>
              </g>
            ))}

            {!hasData && (
              <text x={(plot.x0 + plot.x1) / 2} y={(plot.y0 + plot.y1) / 2} textAnchor="middle" className="fill-muted-foreground text-xs">
                {emptyText}
              </text>
            )}

            <g clipPath={`url(#${clipId})`}>
              {series.map((s, si) => (
                <g key={s.key}>
                  <path
                    d={paths[si].d}
                    fill="none"
                    stroke={seriesColor(s.slot)}
                    strokeWidth={2}
                    strokeLinejoin="round"
                    strokeLinecap="round"
                  />
                  {paths[si].dots.map(([cx, cy], k) => (
                    <circle key={k} cx={cx} cy={cy} r={2} fill={seriesColor(s.slot)} />
                  ))}
                </g>
              ))}
            </g>

            {ends.map((e, si) =>
              e ? (
                <g key={series[si].key}>
                  <circle
                    cx={x(times[e.i])}
                    cy={y(e.v)}
                    r={4}
                    fill={seriesColor(series[si].slot)}
                    className="stroke-card"
                    strokeWidth={2}
                  />
                  {showEndLabels && (
                    <text x={x(times[e.i]) + 8} y={y(e.v)} dy="0.32em" className="fill-foreground text-[10px] tabular-nums">
                      {yFormat(e.v)}
                      <tspan className="fill-muted-foreground"> {series[si].label}</tspan>
                    </text>
                  )}
                </g>
              ) : null,
            )}

            {hoverIdx != null && (
              <g>
                <line x1={hx} x2={hx} y1={plot.y0} y2={plot.y1} className="stroke-foreground/40" strokeWidth={1} />
                {series.map((s) => {
                  const v = s.values[hoverIdx];
                  if (!isNum(v)) return null;
                  return (
                    <circle key={s.key} cx={hx} cy={y(v)} r={4} fill={seriesColor(s.slot)} className="stroke-card" strokeWidth={2} />
                  );
                })}
              </g>
            )}
          </svg>
        )}

        {hoverIdx != null && (
          <div
            className={cn(
              "pointer-events-none absolute top-2 z-10 rounded-md bg-popover px-2.5 py-1.5 text-xs text-popover-foreground shadow-md ring-1 ring-foreground/10",
              flip && "-translate-x-full",
            )}
            style={{ left: flip ? hx - 10 : hx + 10 }}
          >
            <p className="mb-1 font-medium">{fmtTimeFull(times[hoverIdx])}</p>
            {series.map((s) => {
              const v = s.values[hoverIdx];
              return (
                <p key={s.key} className="flex items-center gap-2 tabular-nums">
                  <span className="inline-block h-0.5 w-3 rounded" style={{ background: seriesColor(s.slot) }} aria-hidden />
                  <span className="font-semibold">{isNum(v) ? yFormat(v) : "—"}</span>
                  <span className="text-muted-foreground">{s.label}</span>
                </p>
              );
            })}
          </div>
        )}
      </div>

      {series.length >= 2 && (
        <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
          {series.map((s) => (
            <span key={s.key} className="inline-flex items-center gap-1.5">
              <span className="inline-block h-0.5 w-4 rounded" style={{ background: seriesColor(s.slot) }} aria-hidden />
              {s.label}
            </span>
          ))}
        </div>
      )}

      {hasData && (
        <details className="mt-2 text-xs">
          <summary className="cursor-pointer select-none text-muted-foreground hover:text-foreground">Table view</summary>
          <div className="mt-1 max-h-64 overflow-auto rounded-md border">
            <table className="w-full text-left tabular-nums">
              <thead className="sticky top-0 bg-muted">
                <tr>
                  <th className="px-2 py-1 font-medium">Time</th>
                  {series.map((s) => (
                    <th key={s.key} className="px-2 py-1 font-medium">
                      {s.label}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {times.map((t, i) => (
                  <tr key={i} className="border-t">
                    <td className="whitespace-nowrap px-2 py-0.5">{fmtTimeFull(t)}</td>
                    {series.map((s) => {
                      const v = s.values[i];
                      return (
                        <td key={s.key} className="px-2 py-0.5">
                          {isNum(v) ? yFormat(v) : "—"}
                        </td>
                      );
                    })}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </details>
      )}
    </div>
  );
}

function isNum(v: number | null | undefined): v is number {
  return v != null && Number.isFinite(v);
}

function computeYDomain(series: LineSeries[], thresholds: Threshold[], fixed?: [number, number]) {
  if (fixed) {
    const ticks = niceTicks(fixed[0], fixed[1], 5).filter((t) => t >= fixed[0] && t <= fixed[1]);
    return { y0: fixed[0], y1: fixed[1], yTicks: ticks };
  }
  let lo = Infinity;
  let hi = -Infinity;
  for (const s of series) {
    for (const v of s.values) {
      if (!isNum(v)) continue;
      if (v < lo) lo = v;
      if (v > hi) hi = v;
    }
  }
  for (const th of thresholds) {
    if (th.value < lo) lo = th.value;
    if (th.value > hi) hi = th.value;
  }
  if (!Number.isFinite(lo)) {
    lo = 0;
    hi = 1;
  }
  if (lo === hi) {
    lo -= 1;
    hi += 1;
  }
  const pad = (hi - lo) * 0.08;
  const ticks = niceTicks(lo - pad, hi + pad, 5);
  return { y0: ticks[0], y1: ticks[ticks.length - 1], yTicks: ticks };
}

function buildPath(
  values: (number | null)[],
  times: number[],
  x: (v: number) => number,
  y: (v: number) => number,
): { d: string; dots: [number, number][] } {
  let d = "";
  let run = 0;
  const dots: [number, number][] = [];
  const ok = (i: number) => i >= 0 && i < values.length && isNum(values[i]);
  for (let i = 0; i < values.length && i < times.length; i++) {
    if (!ok(i)) {
      run = 0;
      continue;
    }
    const px = x(times[i]);
    const py = y(values[i] as number);
    d += `${run === 0 ? "M" : "L"}${px.toFixed(1)} ${py.toFixed(1)}`;
    run++;
    if (!ok(i - 1) && !ok(i + 1)) dots.push([px, py]);
  }
  return { d, dots };
}
