export interface User {
  id: string;
  email: string;
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
