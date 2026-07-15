import { useRef, useState } from "react";
import { fmtMoney } from "../api";
import type { MonthlyFlow } from "../types";

const W = 560;
const H = 230;
const M = { top: 10, right: 8, bottom: 26, left: 52 };

/** Rect with only the top corners rounded, anchored to the baseline. */
function topRoundedRect(x: number, y: number, w: number, h: number): string {
  if (h <= 0) return "";
  const r = Math.min(4, h, w / 2);
  return [
    `M${x},${y + h}`,
    `L${x},${y + r}`,
    `Q${x},${y} ${x + r},${y}`,
    `L${x + w - r},${y}`,
    `Q${x + w},${y} ${x + w},${y + r}`,
    `L${x + w},${y + h}`,
    "Z",
  ].join(" ");
}

function niceMax(v: number): number {
  if (v <= 0) return 100;
  const pow = 10 ** Math.floor(Math.log10(v));
  for (const m of [1, 2, 2.5, 5, 10]) {
    if (m * pow >= v) return m * pow;
  }
  return 10 * pow;
}

function monthLabel(ym: string): string {
  const [y, m] = ym.split("-").map(Number);
  return new Date(y, m - 1, 1).toLocaleDateString(undefined, {
    month: "short",
    year: "2-digit",
  });
}

export default function MonthlyFlowChart({ months }: { months: MonthlyFlow[] }) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const [hover, setHover] = useState<{ i: number; x: number; y: number } | null>(null);

  if (months.length === 0) {
    return <div className="empty">Upload a statement to see monthly flows.</div>;
  }

  const innerW = W - M.left - M.right;
  const innerH = H - M.top - M.bottom;
  const yMax = niceMax(Math.max(...months.map((m) => Math.max(m.inflow, m.outflow))));
  const yScale = (v: number) => innerH - (v / yMax) * innerH;

  const slot = innerW / months.length;
  const barW = Math.min(26, Math.max(6, slot / 2 - 6));
  const ticks = [0.25, 0.5, 0.75, 1].map((t) => t * yMax);

  const onMove = (e: React.MouseEvent, i: number) => {
    const rect = wrapRef.current!.getBoundingClientRect();
    setHover({ i, x: e.clientX - rect.left, y: e.clientY - rect.top });
  };

  return (
    <div>
      <div className="chart-legend">
        <span className="legend-chip">
          <span className="legend-swatch" style={{ background: "var(--s2)" }} />
          Money in
        </span>
        <span className="legend-chip">
          <span className="legend-swatch" style={{ background: "var(--s1)" }} />
          Money out
        </span>
      </div>
      <div ref={wrapRef} style={{ position: "relative" }}>
        <svg viewBox={`0 0 ${W} ${H}`} style={{ width: "100%", display: "block" }} role="img"
          aria-label="Monthly money in versus money out">
          <g transform={`translate(${M.left},${M.top})`}>
            {ticks.map((t) => (
              <g key={t}>
                <line x1={0} x2={innerW} y1={yScale(t)} y2={yScale(t)}
                  stroke="var(--grid)" strokeWidth={1} />
                <text x={-8} y={yScale(t)} dy="0.34em" textAnchor="end"
                  fontSize={11} fill="var(--muted)" fontVariant="tabular-nums">
                  {t >= 1000 ? `${t / 1000}k` : t}
                </text>
              </g>
            ))}
            {months.map((m, i) => {
              const cx = i * slot + slot / 2;
              const inH = innerH - yScale(m.inflow);
              const outH = innerH - yScale(m.outflow);
              return (
                <g key={m.month} opacity={hover && hover.i !== i ? 0.55 : 1}>
                  <path d={topRoundedRect(cx - barW - 1, yScale(m.inflow), barW, inH)} fill="var(--s2)" />
                  <path d={topRoundedRect(cx + 1, yScale(m.outflow), barW, outH)} fill="var(--s1)" />
                  <text x={cx} y={innerH + 16} textAnchor="middle" fontSize={11} fill="var(--muted)">
                    {monthLabel(m.month)}
                  </text>
                  <rect x={i * slot} y={0} width={slot} height={innerH} fill="transparent"
                    onMouseMove={(e) => onMove(e, i)} onMouseLeave={() => setHover(null)} />
                </g>
              );
            })}
            <line x1={0} x2={innerW} y1={innerH} y2={innerH} stroke="var(--baseline)" strokeWidth={1} />
          </g>
        </svg>
        {hover && (
          <div className="viz-tooltip" style={{
            left: Math.min(hover.x + 12, wrapRef.current!.clientWidth - 170),
            top: hover.y + 12,
          }}>
            <div className="tt-title">{monthLabel(months[hover.i].month)}</div>
            <div className="tt-row">
              <span className="legend-chip">
                <span className="legend-swatch" style={{ background: "var(--s2)" }} />In
              </span>
              <span className="val">{fmtMoney(months[hover.i].inflow)}</span>
            </div>
            <div className="tt-row">
              <span className="legend-chip">
                <span className="legend-swatch" style={{ background: "var(--s1)" }} />Out
              </span>
              <span className="val">{fmtMoney(months[hover.i].outflow)}</span>
            </div>
            <div className="tt-row">
              <span>Net</span>
              <span className="val">{fmtMoney(months[hover.i].inflow - months[hover.i].outflow)}</span>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
