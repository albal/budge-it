import { fmtMoney } from "../api";

interface Props {
  label: string;
  value: number;
  signed?: boolean; // color + prefix by sign (for Net)
}

export default function StatTile({ label, value, signed }: Props) {
  const cls = signed ? (value >= 0 ? "good" : "bad") : "";
  const text = signed && value > 0 ? `+${fmtMoney(value)}` : fmtMoney(value);
  return (
    <div className="card stat-tile">
      <div className="label">{label}</div>
      <div className={`value ${cls}`}>{text}</div>
    </div>
  );
}
