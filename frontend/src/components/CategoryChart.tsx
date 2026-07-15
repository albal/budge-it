import { useState } from "react";
import { fmtMoney } from "../api";
import type { CategoryTotal } from "../types";

// Fixed categorical slot order (validated palette); "Other" gets neutral gray
// so a generated 9th hue never appears.
const SLOTS = [
  "var(--s1)", "var(--s2)", "var(--s3)", "var(--s4)",
  "var(--s5)", "var(--s6)", "var(--s7)", "var(--s8)",
];
const OTHER_COLOR = "var(--muted)";
const MAX_SEGMENTS = 7;

export default function CategoryChart({ data }: { data: CategoryTotal[] }) {
  const [hover, setHover] = useState<string | null>(null);

  const spending = data.filter((d) => d.total > 0);
  if (spending.length === 0) {
    return <div className="empty">No spending yet for this period.</div>;
  }

  const head = spending.slice(0, MAX_SEGMENTS);
  const tail = spending.slice(MAX_SEGMENTS);
  const rows = [...head];
  if (tail.length > 0) {
    rows.push({
      category: "Other",
      total: tail.reduce((s, d) => s + d.total, 0),
      count: tail.reduce((s, d) => s + d.count, 0),
    });
  }
  const grand = rows.reduce((s, d) => s + d.total, 0);
  const colorOf = (i: number, name: string) =>
    name === "Other" ? OTHER_COLOR : SLOTS[i % SLOTS.length];

  return (
    <div>
      {/* 100% stacked bar; the 2px flex gap is the spacer between fills */}
      <div style={{ display: "flex", gap: 2, height: 28 }}
        role="img" aria-label="Share of spending by category">
        {rows.map((r, i) => (
          <div key={r.category}
            title={`${r.category}: ${fmtMoney(r.total)}`}
            onMouseEnter={() => setHover(r.category)}
            onMouseLeave={() => setHover(null)}
            style={{
              width: `${(r.total / grand) * 100}%`,
              minWidth: 3,
              background: colorOf(i, r.category),
              borderRadius:
                i === 0 ? "4px 0 0 4px" : i === rows.length - 1 ? "0 4px 4px 0" : 0,
              opacity: hover && hover !== r.category ? 0.35 : 1,
              transition: "opacity 0.12s",
            }}
          />
        ))}
      </div>
      {/* Legend doubles as the visible-value relief for low-contrast hues */}
      <div className="cat-list">
        {rows.map((r, i) => (
          <div key={r.category}
            className={`cat-row${hover && hover !== r.category ? " dim" : ""}`}
            onMouseEnter={() => setHover(r.category)}
            onMouseLeave={() => setHover(null)}>
            <span className="legend-swatch" style={{ background: colorOf(i, r.category) }} />
            <span className="cat-name">{r.category}</span>
            <span className="cat-amt">{fmtMoney(r.total)}</span>
            <span className="cat-pct">{((r.total / grand) * 100).toFixed(1)}%</span>
          </div>
        ))}
      </div>
    </div>
  );
}
