'use client';

import * as React from 'react';
import Link from 'next/link';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, FolderTree } from 'lucide-react';

import { SdkError, type ExpenseCategory } from '@fuelgrid/sdk';
import {
  Badge,
  type BadgeProps,
  Button,
  Card,
  CardContent,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  EmptyState,
  ErrorState,
  Input,
  Label,
  PageHeader,
  Skeleton,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@fuelgrid/ui';

import { PermissionGate } from '@/components/permission-gate';
import { usePermission } from '@/hooks/use-permissions';
import { api } from '@/lib/api';
import { formatMoney } from '@/lib/money';
import { toast } from '@/lib/toast';

function statusTone(status: string): BadgeProps['tone'] {
  return status === 'active' ? 'success' : 'neutral';
}

export default function ExpenseCategoriesPage() {
  const qc = useQueryClient();
  const [editing, setEditing] = React.useState<ExpenseCategory | null>(null);
  const [createOpen, setCreateOpen] = React.useState(false);

  const canManage = usePermission('expense.manage');

  const list = useQuery({
    queryKey: ['expense-categories'],
    queryFn: ({ signal }) => api.listExpenseCategories(signal),
  });

  function invalidate() {
    void qc.invalidateQueries({ queryKey: ['expense-categories'] });
  }

  const setStatus = useMutation({
    mutationFn: ({ id, status }: { id: string; status: 'active' | 'archived' }) =>
      api.setExpenseCategoryStatus(id, status),
    onSuccess: (_c, vars) => {
      invalidate();
      toast.success(vars.status === 'active' ? 'Category activated' : 'Category deactivated');
    },
    onError: (err) =>
      toast.error('Could not update category', err instanceof SdkError ? err.message : undefined),
  });

  const items = list.data?.items ?? [];

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Finance · Expenses"
        title="Expense categories"
        description="Manage expense categories, their accounting mapping, and approval thresholds."
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <Button asChild variant="ghost">
              <Link href="/expenses">
                <ArrowLeft className="size-4" />
                Expenses
              </Link>
            </Button>
            <PermissionGate permission="expense.manage">
              <Button type="button" size="sm" onClick={() => setCreateOpen(true)}>
                New category
              </Button>
            </PermissionGate>
          </div>
        }
      />

      {list.isPending ? (
        <div className="flex flex-col gap-2">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-14 rounded-lg" />
          ))}
        </div>
      ) : list.isError ? (
        (() => {
          const forbidden = list.error instanceof SdkError && list.error.status === 403;
          return (
            <ErrorState
              title={forbidden ? 'No access' : "Couldn't load categories"}
              description={
                forbidden
                  ? "You don't have permission to view expense categories (finance.read)."
                  : String((list.error as Error).message)
              }
              onRetry={forbidden ? undefined : () => list.refetch()}
            />
          );
        })()
      ) : items.length === 0 ? (
        <EmptyState
          title="No categories"
          description="Create a category to organise expenses by accounting account and approval threshold."
          icon={<FolderTree />}
        />
      ) : (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Account</TableHead>
                  <TableHead className="text-right">Approval threshold</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((c: ExpenseCategory) => {
                  const isActive = c.status === 'active';
                  const busy = setStatus.isPending && setStatus.variables?.id === c.id;
                  return (
                    <TableRow key={c.id}>
                      <TableCell className="font-medium">{c.name}</TableCell>
                      <TableCell className="text-muted-foreground">{c.account_key}</TableCell>
                      <TableCell className="text-right font-mono tabular-nums">
                        {c.approval_threshold != null && c.approval_threshold !== ''
                          ? formatMoney(c.approval_threshold)
                          : '—'}
                      </TableCell>
                      <TableCell>
                        <Badge tone={statusTone(c.status)}>{c.status}</Badge>
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex justify-end gap-2">
                          <PermissionGate permission="expense.manage">
                            <Button
                              type="button"
                              variant="secondary"
                              size="sm"
                              onClick={() => setEditing(c)}
                            >
                              Edit
                            </Button>
                          </PermissionGate>
                          <PermissionGate permission="expense.manage">
                            <Button
                              type="button"
                              variant="ghost"
                              size="sm"
                              disabled={busy}
                              onClick={() =>
                                setStatus.mutate({
                                  id: c.id,
                                  status: isActive ? 'archived' : 'active',
                                })
                              }
                            >
                              {isActive ? 'Deactivate' : 'Activate'}
                            </Button>
                          </PermissionGate>
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      {canManage === false ? (
        <p className="text-xs text-muted-foreground">
          You have read-only access to categories. Creating and editing requires the expense.manage
          permission.
        </p>
      ) : null}

      <CategoryDialog
        mode="create"
        open={createOpen}
        onOpenChange={setCreateOpen}
        canManage={canManage === true}
        onSaved={() => {
          setCreateOpen(false);
          invalidate();
        }}
      />
      <CategoryDialog
        mode="edit"
        category={editing ?? undefined}
        open={editing !== null}
        onOpenChange={(open) => {
          if (!open) setEditing(null);
        }}
        canManage={canManage === true}
        onSaved={() => {
          setEditing(null);
          invalidate();
        }}
      />
    </div>
  );
}

function CategoryDialog({
  mode,
  category,
  open,
  onOpenChange,
  canManage,
  onSaved,
}: {
  mode: 'create' | 'edit';
  category?: ExpenseCategory;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  canManage: boolean;
  onSaved: () => void;
}) {
  const [name, setName] = React.useState('');
  const [accountKey, setAccountKey] = React.useState('operating_expense');
  const [threshold, setThreshold] = React.useState('');

  // Seed the form when the dialog opens (edit) or resets (create).
  React.useEffect(() => {
    if (!open) return;
    setName(category?.name ?? '');
    setAccountKey(category?.account_key ?? 'operating_expense');
    setThreshold(category?.approval_threshold ?? '');
  }, [open, category]);

  const save = useMutation({
    mutationFn: () => {
      const req = {
        name: name.trim(),
        account_key: accountKey.trim() || undefined,
        approval_threshold: threshold.trim() || undefined,
      };
      return mode === 'create'
        ? api.createExpenseCategory(req)
        : api.updateExpenseCategory(category!.id, req);
    },
    onSuccess: () => {
      toast.success(mode === 'create' ? 'Category created' : 'Category updated');
      onSaved();
    },
    onError: (err) =>
      toast.error('Could not save category', err instanceof SdkError ? err.message : undefined),
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{mode === 'create' ? 'New category' : 'Edit category'}</DialogTitle>
          <DialogDescription>
            Set the category name, the accounting account it maps to, and an optional approval
            threshold.
          </DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="cat-name">Name</Label>
            <Input
              id="cat-name"
              required
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Utilities"
            />
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="cat-account">Account key</Label>
              <Input
                id="cat-account"
                value={accountKey}
                onChange={(e) => setAccountKey(e.target.value)}
                placeholder="operating_expense"
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="cat-threshold">Approval threshold</Label>
              <Input
                id="cat-threshold"
                inputMode="decimal"
                value={threshold}
                onChange={(e) => setThreshold(e.target.value)}
                placeholder="Optional"
              />
            </div>
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={!canManage || save.isPending || !name.trim()}>
              {save.isPending ? 'Saving…' : mode === 'create' ? 'Create category' : 'Save changes'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
