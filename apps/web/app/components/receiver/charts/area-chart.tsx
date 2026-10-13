import { useState, type PointerEvent } from "react";
import { cn } from "~/lib/utils";
import { SERIES_VARS_CLASS, nearestIndex, niceTicks, scaleLinear, seriesColor, useMeasure } from "./chart-utils";

export interface SpectrumMarker {
  freq_hz: number;
  label: string;
  enabled: boolean;
}

interface Props {
  /** Bin centre frequencies in Hz, ascending. */
  xs: number[];
  /** Bin power in dB, one per entry of `xs`. */
  ys: number[];
  /** Configured channel frequencies to mark on the x axis. */
  markers: SpectrumMarker[];
  height?: number;
}

const PAD = { top: 24, bottom: 34, left: 48, right: 16 };

/**
 * One-series area chart for a spectrum snapshot: frequency on x, power on
 * y, the series hue as a 10% wash under a 2px line, channel frequencies as
 * labelled vertical rules, and a crosshair tooltip that snaps to the nearest bin.
 */
export function SpectrumChart({ xs, ys, markers, height = 240 }: Props) {
  const [ref, width] = useMeasure<HTMLDivElement>();
  const [hover, setHover] = useState<number | null>(null);

  const n = Math.min(xs.length, ys.length);
  const plot = { x0: PAD.left, x1: Math.max(PAD.left + 20, width - PAD.right), y0: PAD.top, y1: height - PAD.bottom };
  const xDomain: [number, number] = n > 1 ? [xs[0], xs[n - 1]] : [0, 1];

  let lo = Infinity;
  let hi = -Infinity;
  for (let i = 0; i < n; i++) {
    const v = ys[i];
    if (!Number.isFinite(v)) continue;
    if (v < lo) lo = v;
    if (v > hi) hi = v;
  }
  if (!Number.isFinite(lo)) {
    lo = -100;
    hi = 0;
  }
  if (lo === hi) {
    lo -= 1;
    hi += 1;
  }
  const padY = (hi - lo) * 0.08;
  const yTicks = niceTicks(lo - padY, hi + padY, 5);
  const y0 = yTicks[0];
  const y1 = yTicks[yTicks.length - 1];

  const x = scaleLinear(xDomain, [plot.x0, plot.x1]);
  const y = scaleLinear([y0, y1], [plot.y1, plot.y0]);
  const xTicksMHz = niceTicks(xDomain[0] / 1e6, xDomain[1] / 1e6, 6).filter(
    (v) => v * 1e6 >= xDomain[0] && v * 1e6 <= xDomain[1],
  );

  let line = "";
  for (let i = 0; i < n; i++) {
    line += `${i === 0 ? "M" : "L"}${x(xs[i]).toFixed(1)} ${y(ys[i]).toFixed(1)}`;
  }
  const area = n > 1 ? `${line}L${x(xs[n - 1]).toFixed(1)} ${plot.y1}L${x(xs[0]).toFixed(1)} ${plot.y1}Z` : "";

  const hoverIdx = hover != null && hover >= 0 && hover < n ? hover : null;
  const hx = hoverIdx != null ? x(xs[hoverIdx]) : 0;
  const flip = hx > plot.x0 + (plot.x1 - plot.x0) * 0.6;

  const onMove = (e: PointerEvent<SVGSVGElement>) => {
    if (n < 2) return;
    const rect = e.currentTarget.getBoundingClientRect();
    const frac = (e.clientX - rect.left - plot.x0) / (plot.x1 - plot.x0);
    const f = xDomain[0] + Math.min(1, Math.max(0, frac)) * (xDomain[1] - xDomain[0]);
    setHover(nearestIndex(xs, f));
  };

  return (
    <div className={SERIES_VARS_CLASS}>
      <div ref={ref} className="relative w-full" style={{ height }}>
        {width > 0 && (
          <svg
            width={width}
            height={height}
            className="block select-none overflow-visible rounded outline-none"
            role="img"
            aria-label="Power spectrum by frequency"
            onPointerMove={onMove}
            onPointerLeave={() => setHover(null)}
          >
            {yTicks.map((v) => (
              <g key={v}>
                <line x1={plot.x0} x2={plot.x1} y1={y(v)} y2={y(v)} className="stroke-border" strokeWidth={1} />
                <text x={plot.x0 - 6} y={y(v)} dy="0.32em" textAnchor="end" className="fill-muted-foreground text-[10px] tabular-nums">
                  {v}
                </text>
              </g>
            ))}
            <line x1={plot.x0} x2={plot.x1} y1={plot.y1} y2={plot.y1} className="stroke-border" strokeWidth={1} />
            {xTicksMHz.map((v) => (
              <g key={v}>
                <line x1={x(v * 1e6)} x2={x(v * 1e6)} y1={plot.y1} y2={plot.y1 + 4} className="stroke-border" strokeWidth={1} />
                <text x={x(v * 1e6)} y={plot.y1 + 15} textAnchor="middle" className="fill-muted-foreground text-[10px] tabular-nums">
                  {v.toFixed(3)}
                </text>
              </g>
            ))}
            <text
              transform={`translate(11 ${(plot.y0 + plot.y1) / 2}) rotate(-90)`}
              textAnchor="middle"
              className="fill-muted-foreground text-[10px]"
            >
              Power (dB)
            </text>
            <text x={(plot.x0 + plot.x1) / 2} y={height - 3} textAnchor="middle" className="fill-muted-foreground text-[10px]">
              Frequency (MHz)
            </text>

            {area && <path d={area} fill={seriesColor(0)} fillOpacity={0.1} stroke="none" />}
            {line && (
              <path d={line} fill="none" stroke={seriesColor(0)} strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />
            )}

            {markers
              .filter((m) => m.freq_hz >= xDomain[0] && m.freq_hz <= xDomain[1])
              .map((m) => (
                <g key={`${m.label}-${m.freq_hz}`} className={cn(!m.enabled && "opacity-50")}>
                  <line x1={x(m.freq_hz)} x2={x(m.freq_hz)} y1={plot.y0} y2={plot.y1} className="stroke-foreground/50" strokeWidth={1} />
                  <text x={x(m.freq_hz)} y={plot.y0 - 8} textAnchor="middle" className="fill-foreground text-[10px]">
                    {m.label}
                    {!m.enabled && <tspan className="fill-muted-foreground"> (off)</tspan>}
                  </text>
                </g>
              ))}

            {hoverIdx != null && (
              <g>
                <line x1={hx} x2={hx} y1={plot.y0} y2={plot.y1} className="stroke-foreground/40" strokeWidth={1} />
                <circle cx={hx} cy={y(ys[hoverIdx])} r={4} fill={seriesColor(0)} className="stroke-card" strokeWidth={2} />
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
            <p className="font-semibold tabular-nums">{ys[hoverIdx].toFixed(1)} dB</p>
            <p className="text-muted-foreground tabular-nums">{(xs[hoverIdx] / 1e6).toFixed(4)} MHz</p>
          </div>
        )}
      </div>
    </div>
  );
}
