import { useState, useEffect, useCallback } from "react";
import { useSearchParams, Link } from "react-router";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "~/components/ui/table";
import { Input } from "~/components/ui/input";
import { Button } from "~/components/ui/button";
import { api, buildQuery, type PaginatedResult, type ListParams } from "~/lib/api";
import { Plus, Pencil, Trash2, Search, ArrowUpDown } from "lucide-react";

export interface Column {
  key: string;
  label: string;
  sortable?: boolean;
  render?: (value: any, row: Record<string, any>) => React.ReactNode;
}

interface DataTableProps {
  endpoint: string;
  columns: Column[];
  title: string;
  createPath?: string;
  editPath?: (row: Record<string, any>) => string;
  onDelete?: (row: Record<string, any>) => Promise<void>;
  actions?: (row: Record<string, any>) => React.ReactNode;
  // Slot for filter controls in the page header (e.g. show-deleted toggle).
  headerActions?: React.ReactNode;
  idField?: string;
}

export function DataTable({ endpoint, columns, title, createPath, editPath, onDelete, actions, headerActions, idField = "id" }: DataTableProps) {
  const [searchParams, setSearchParams] = useSearchParams();
  const [result, setResult] = useState<PaginatedResult | null>(null);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState(searchParams.get("search") || "");

  const params: ListParams = {
    page: Number(searchParams.get("page")) || 1,
    per_page: Number(searchParams.get("per_page")) || 25,
    search: searchParams.get("search") || "",
    sort_by: searchParams.get("sort_by") || "",
    sort_dir: (searchParams.get("sort_dir") as "asc" | "desc") || "desc",
  };

  const fetchData = useCallback(async () => {
    setLoading(true);
    try {
      const data = await api.get<PaginatedResult>(`${endpoint}?${buildQuery(params)}`);
      setResult(data);
    } catch (e) {
      console.error(e);
    } finally {
      setLoading(false);
    }
  }, [endpoint, searchParams.toString()]);

  useEffect(() => { fetchData(); }, [fetchData]);

  const updateParams = (updates: Partial<ListParams>) => {
    const next = new URLSearchParams(searchParams);
    Object.entries(updates).forEach(([k, v]) => {
      if (v) next.set(k, String(v));
      else next.delete(k);
    });
    if (updates.search !== undefined) next.set("page", "1");
    setSearchParams(next);
  };

  const handleSort = (key: string) => {
    const dir = params.sort_by === key && params.sort_dir === "asc" ? "desc" : "asc";
    updateParams({ sort_by: key, sort_dir: dir });
  };

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault();
    updateParams({ search: search || undefined });
  };

  const handleDelete = async (row: Record<string, any>) => {
    if (!confirm("Are you sure?")) return;
    if (onDelete) {
      await onDelete(row);
      fetchData();
    }
  };

  return (
    <div className="space-y-4 min-w-0">
      <div className="flex items-center justify-between gap-4">
        <h1 className="text-2xl font-bold">{title}</h1>
        <div className="flex items-center gap-3">
          {headerActions}
          {createPath && (
            <Link to={createPath}><Button><Plus className="size-4" /> Create New</Button></Link>
          )}
        </div>
      </div>

      <form onSubmit={handleSearch} className="flex gap-2 max-w-sm">
        <Input placeholder="Search..." value={search} onChange={(e) => setSearch(e.target.value)} />
        <Button type="submit" variant="secondary"><Search className="size-4" /> Search</Button>
      </form>

      <div className="rounded-md border overflow-x-auto">
        <Table className="min-w-[800px]">
          <TableHeader>
            <TableRow>
              {columns.map((col) => (
                <TableHead
                  key={col.key}
                  className={col.sortable ? "cursor-pointer select-none hover:bg-muted/50" : ""}
                  onClick={col.sortable ? () => handleSort(col.key) : undefined}
                >
                  {col.label}
                  {params.sort_by === col.key && (params.sort_dir === "asc" ? " ↑" : " ↓")}
                </TableHead>
              ))}
              {(editPath || onDelete || actions) && <TableHead className="w-32">Actions</TableHead>}
            </TableRow>
          </TableHeader>
          <TableBody>
            {loading ? (
              <TableRow><TableCell colSpan={columns.length + 1} className="text-center py-8 text-muted-foreground">Loading...</TableCell></TableRow>
            ) : !result?.data.length ? (
              <TableRow><TableCell colSpan={columns.length + 1} className="text-center py-8 text-muted-foreground">No results found</TableCell></TableRow>
            ) : (
              result.data.map((row, i) => (
                <TableRow key={row[idField] ?? i}>
                  {columns.map((col) => (
                    <TableCell key={col.key}>
                      {col.render ? col.render(row[col.key], row) : formatValue(row[col.key])}
                    </TableCell>
                  ))}
                  {(editPath || onDelete || actions) && (
                    <TableCell>
                      <div className="flex gap-1">
                        {actions?.(row)}
                        {editPath && (
                          <Link to={editPath(row)}>
                            <Button variant="ghost" size="sm"><Pencil className="size-3.5" /></Button>
                          </Link>
                        )}
                        {onDelete && (
                          <Button variant="ghost" size="sm" className="text-destructive" onClick={() => handleDelete(row)}>
                            <Trash2 className="size-3.5" />
                          </Button>
                        )}
                      </div>
                    </TableCell>
                  )}
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>

      {result && result.total_pages > 1 && (
        <div className="flex items-center justify-between">
          <p className="text-sm text-muted-foreground">
            Showing {(result.page - 1) * result.per_page + 1}-{Math.min(result.page * result.per_page, result.total)} of {result.total}
          </p>
          <div className="flex gap-1">
            <Button variant="outline" size="sm" disabled={result.page <= 1}
              onClick={() => updateParams({ page: result.page - 1 })}>Previous</Button>
            <Button variant="outline" size="sm" disabled={result.page >= result.total_pages}
              onClick={() => updateParams({ page: result.page + 1 })}>Next</Button>
          </div>
        </div>
      )}
    </div>
  );
}

function formatValue(v: any): string {
  if (v === null || v === undefined) return "—";
  if (typeof v === "boolean") return v ? "Yes" : "No";
  if (typeof v === "object") return JSON.stringify(v);
  const s = String(v);
  // ISO date
  if (/^\d{4}-\d{2}-\d{2}T/.test(s)) {
    return new Date(s).toLocaleString();
  }
  if (s.length > 80) return s.slice(0, 80) + "…";
  return s;
}
