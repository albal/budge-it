export interface User {
  id: string;
  email: string;
  /** Claimed by the first account to log in (or granted via ADMIN_EMAILS).
   *  Only gates the UI link — the backend re-checks authorization on every
   *  /admin request. */
  isAdmin?: boolean;
}

/** A user account plus the volume of data that deleting it would remove. */
export interface AdminUser {
  id: string;
  email: string;
  isAdmin: boolean;
  createdAt: string;
  uploadCount: number;
  txnCount: number;
  ruleCount: number;
  categoryCount: number;
}

export interface Upload {
  id: string;
  filename: string;
  contentType: string;
  sizeBytes: number;
  status: "pending" | "processing" | "done" | "error";
  error?: string;
  txnCount: number;
  createdAt: string;
  processedAt?: string;
}

export interface Transaction {
  id: string;
  date: string;
  description: string;
  merchant: string;
  amount: number;
  direction: "debit" | "credit";
  category: string;
}

export interface MonthlyFlow {
  month: string; // YYYY-MM
  inflow: number;
  outflow: number;
}

export interface Summary {
  totalInflow: number;
  totalOutflow: number;
  net: number;
  months: MonthlyFlow[];
}

export interface CategoryTotal {
  category: string;
  total: number;
  count: number;
}
