import { fmtMoney, recategorize } from "../api";
import type { Transaction } from "../types";

const ADD_SENTINEL = "__add__";

interface Props {
  txns: Transaction[];
  categories: string[];
  onChanged: () => void;
  onAddCategory: (name: string) => Promise<void>;
}

export default function TransactionTable({ txns, categories, onChanged, onAddCategory }: Props) {
  if (txns.length === 0) {
    return <div className="empty">No transactions match the current filters.</div>;
  }

  const change = async (id: string, category: string) => {
    try {
      await recategorize(id, category);
      onChanged();
    } catch (err) {
      alert(`Could not update category: ${(err as Error).message}`);
    }
  };

  const addAndApply = async (id: string) => {
    const name = window.prompt("New category name")?.trim();
    if (!name) return;
    if (categories.some((c) => c.toLowerCase() === name.toLowerCase())) {
      alert(`"${name}" already exists — pick it from the list instead.`);
      return;
    }
    try {
      await onAddCategory(name);
      await change(id, name);
    } catch (err) {
      alert(`Could not add category: ${(err as Error).message}`);
    }
  };

  return (
    <div className="table-scroll">
      <table className="txns">
        <thead>
          <tr>
            <th>Date</th>
            <th>Description</th>
            <th>Category</th>
            <th style={{ textAlign: "right" }}>Amount</th>
          </tr>
        </thead>
        <tbody>
          {txns.map((t) => (
            <tr key={t.id}>
              <td style={{ whiteSpace: "nowrap" }}>
                {new Date(t.date).toLocaleDateString()}
              </td>
              <td>{t.description}</td>
              <td>
                {/* Changing this persists a rule for the merchant */}
                <select
                  value={t.category}
                  onChange={(e) =>
                    e.target.value === ADD_SENTINEL
                      ? void addAndApply(t.id)
                      : void change(t.id, e.target.value)
                  }
                  title="Re-categorize (saves as a rule for this merchant)"
                >
                  <option value={ADD_SENTINEL}>+ Add category…</option>
                  {categories.map((c) => (
                    <option key={c} value={c}>{c}</option>
                  ))}
                </select>
              </td>
              <td className={`amt ${t.direction}`}>
                {t.direction === "credit" ? "+" : "−"}
                {fmtMoney(t.amount)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
