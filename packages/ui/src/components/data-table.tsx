'use client';

import * as React from 'react';
import { ArrowDown, ArrowUp, ChevronsUpDown } from 'lucide-react';

import { cn } from '../lib/cn';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from './table';

export type SortDirection = 'asc' | 'desc';

export interface DataTableColumn<T> {
  /** Stable key — also the React key for the column. */
  id: string;
  /** Header label. */
  header: React.ReactNode;
  /** Cell renderer for a row. */
  cell: (row: T) => React.ReactNode;
  /**
   * When provided, the column is sortable and this returns the value to sort
   * by. Strings sort case-insensitively; numbers numerically.
   */
  sortValue?: (row: T) => string | number | null | undefined;
  /** Right-align numeric columns (header + cells). */
  align?: 'left' | 'right';
  /** Extra className applied to each cell. */
  className?: string;
  /** Extra className applied to the header cell. */
  headerClassName?: string;
}

export interface DataTableProps<T> {
  columns: DataTableColumn<T>[];
  rows: T[];
  /** Stable row key. */
  rowKey: (row: T) => string;
  /** Initial sort. */
  defaultSort?: { columnId: string; direction: SortDirection };
  /** Optional row click handler (renders rows as interactive). */
  onRowClick?: (row: T) => void;
  className?: string;
  /** When true (default), the header sticks to the top of a scroll container. */
  stickyHeader?: boolean;
  /** Rendered in the tbody when `rows` is empty. */
  emptyContent?: React.ReactNode;
}

function compare(a: string | number | null | undefined, b: string | number | null | undefined) {
  // Nullish always sorts last regardless of direction caller, then re-flipped.
  if (a == null && b == null) return 0;
  if (a == null) return 1;
  if (b == null) return -1;
  if (typeof a === 'number' && typeof b === 'number') return a - b;
  return String(a).localeCompare(String(b), undefined, { numeric: true, sensitivity: 'base' });
}

/**
 * DataTable — a sortable, sticky-header table over the @fuelgrid/ui Table
 * primitives. Sorting is client-side and uncontrolled: click a sortable header
 * to cycle asc -> desc. Keeps the Refined Console look (muted header, hover
 * rows, mono/tabular numbers via the caller's cell renderer).
 */
export function DataTable<T>({
  columns,
  rows,
  rowKey,
  defaultSort,
  onRowClick,
  className,
  stickyHeader = true,
  emptyContent,
}: DataTableProps<T>) {
  const [sort, setSort] = React.useState<{ columnId: string; direction: SortDirection } | null>(
    defaultSort ?? null,
  );

  const sortedRows = React.useMemo(() => {
    if (!sort) return rows;
    const col = columns.find((c) => c.id === sort.columnId);
    if (!col?.sortValue) return rows;
    const getValue = col.sortValue;
    const dir = sort.direction === 'asc' ? 1 : -1;
    return [...rows].sort((a, b) => compare(getValue(a), getValue(b)) * dir);
  }, [rows, sort, columns]);

  function toggleSort(columnId: string) {
    setSort((prev) => {
      if (prev?.columnId !== columnId) return { columnId, direction: 'asc' };
      return { columnId, direction: prev.direction === 'asc' ? 'desc' : 'asc' };
    });
  }

  return (
    <Table className={className}>
      <TableHeader className={cn(stickyHeader && 'sticky top-0 z-10 bg-card/95 backdrop-blur-sm')}>
        <TableRow>
          {columns.map((col) => {
            const sortable = Boolean(col.sortValue);
            const active = sort?.columnId === col.id;
            return (
              <TableHead
                key={col.id}
                className={cn(col.align === 'right' && 'text-right', col.headerClassName)}
                aria-sort={
                  active ? (sort?.direction === 'asc' ? 'ascending' : 'descending') : undefined
                }
              >
                {sortable ? (
                  <button
                    type="button"
                    onClick={() => toggleSort(col.id)}
                    className={cn(
                      'inline-flex items-center gap-1 rounded text-xs font-medium transition-colors hover:text-foreground',
                      col.align === 'right' && 'flex-row-reverse',
                      active ? 'text-foreground' : 'text-muted-foreground',
                    )}
                  >
                    {col.header}
                    {active ? (
                      sort?.direction === 'asc' ? (
                        <ArrowUp className="size-3.5" />
                      ) : (
                        <ArrowDown className="size-3.5" />
                      )
                    ) : (
                      <ChevronsUpDown className="size-3.5 opacity-50" />
                    )}
                  </button>
                ) : (
                  col.header
                )}
              </TableHead>
            );
          })}
        </TableRow>
      </TableHeader>
      <TableBody>
        {sortedRows.length === 0 ? (
          <TableRow className="hover:bg-transparent">
            <TableCell colSpan={columns.length} className="py-10 text-center">
              {emptyContent ?? <span className="text-sm text-muted-foreground">No rows.</span>}
            </TableCell>
          </TableRow>
        ) : (
          sortedRows.map((row) => (
            <TableRow
              key={rowKey(row)}
              onClick={onRowClick ? () => onRowClick(row) : undefined}
              className={cn(onRowClick && 'cursor-pointer')}
            >
              {columns.map((col) => (
                <TableCell
                  key={col.id}
                  className={cn(col.align === 'right' && 'text-right', col.className)}
                >
                  {col.cell(row)}
                </TableCell>
              ))}
            </TableRow>
          ))
        )}
      </TableBody>
    </Table>
  );
}
