'use client';

import * as React from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { FileText } from 'lucide-react';

import { SdkError, type Customer, type CustomerInvoice, type CustomerPayment } from '@fuelgrid/sdk';
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

import { DocumentActions } from '@/components/document-actions';
import { PermissionGate } from '@/components/permission-gate';
import { usePermission } from '@/hooks/use-permissions';
import { api } from '@/lib/api';
import { formatMoney } from '@/lib/money';
import { toast } from '@/lib/toast';

/** Today as a YYYY-MM-DD string for the payment-date default. */
function todayISO(): string {
  return new Date().toISOString().slice(0, 10);
}

function statusTone(status: string): BadgeProps['tone'] {
  switch (status) {
    case 'paid':
      return 'success';
    case 'issued':
      return 'info';
    case 'partially_paid':
      return 'warning';
    case 'void':
    case 'cancelled':
      return 'danger';
    default:
      return 'neutral';
  }
}

export default function CreditInvoicesPage() {
  const qc = useQueryClient();
  const [customerID, setCustomerID] = React.useState<string>('');
  const [detailID, setDetailID] = React.useState<string | null>(null);
  const [payOpen, setPayOpen] = React.useState(false);
  const [createOpen, setCreateOpen] = React.useState(false);
  const [reverseTarget, setReverseTarget] = React.useState<CustomerPayment | null>(null);

  const customers = useQuery({
    queryKey: ['customers'],
    queryFn: ({ signal }) => api.listCustomers(signal),
  });

  const list = useQuery({
    queryKey: ['customer-invoices', customerID],
    queryFn: ({ signal }) =>
      api.listCustomerInvoices({ customerID: customerID || undefined }, signal),
  });

  const canManage = usePermission('customer_invoice.manage');
  const canIssue = usePermission('customer_invoice.issue');
  const canAllocate = usePermission('customer_payment.manage');

  const payments = useQuery({
    queryKey: ['customer-payments'],
    queryFn: ({ signal }) => api.listCustomerPayments(signal),
  });

  function invalidate() {
    void qc.invalidateQueries({ queryKey: ['customer-invoices'] });
    void qc.invalidateQueries({ queryKey: ['customer-payments'] });
  }

  const issue = useMutation({
    mutationFn: (id: string) => api.issueCustomerInvoice(id),
    onSuccess: () => {
      invalidate();
      toast.success('Invoice issued', 'The invoice is now posted to the ledger.');
    },
    onError: (err) =>
      toast.error('Could not issue invoice', err instanceof SdkError ? err.message : undefined),
  });

  const customerName = React.useCallback(
    (id: string) => customers.data?.items.find((c) => c.id === id)?.name ?? id,
    [customers.data],
  );

  const items = list.data?.items ?? [];
  const customerList = customers.data?.items ?? [];

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Finance · Credit"
        title="Customer invoices"
        description="Receivables invoices with their outstanding balance. Issue drafts to the ledger, download a formal PDF, and allocate a customer payment across invoices."
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <PermissionGate permission="customer_payment.manage">
              <Button type="button" size="sm" variant="secondary" onClick={() => setPayOpen(true)}>
                Allocate payment
              </Button>
            </PermissionGate>
            <PermissionGate permission="customer_invoice.manage">
              <Button type="button" size="sm" onClick={() => setCreateOpen(true)}>
                New invoice
              </Button>
            </PermissionGate>
          </div>
        }
      />

      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          size="sm"
          variant={customerID === '' ? 'primary' : 'secondary'}
          onClick={() => setCustomerID('')}
        >
          All customers
        </Button>
        {customerList.map((c: Customer) => (
          <Button
            key={c.id}
            type="button"
            size="sm"
            variant={customerID === c.id ? 'primary' : 'secondary'}
            onClick={() => setCustomerID(c.id)}
          >
            {c.name}
          </Button>
        ))}
      </div>

      {list.isPending ? (
        <div className="flex flex-col gap-2">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-14 rounded-lg" />
          ))}
        </div>
      ) : list.isError ? (
        (() => {
          const forbidden = list.error instanceof SdkError && list.error.status === 403;
          return (
            <ErrorState
              title={forbidden ? 'No access' : "Couldn't load invoices"}
              description={
                forbidden
                  ? "You don't have permission to view customer invoices (finance.read)."
                  : String((list.error as Error).message)
              }
              onRetry={forbidden ? undefined : () => list.refetch()}
            />
          );
        })()
      ) : items.length === 0 ? (
        <EmptyState
          title="No invoices"
          description={
            customerID
              ? 'This customer has no invoices.'
              : 'Customer invoices will appear here once raised.'
          }
          icon={<FileText />}
        />
      ) : (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Number</TableHead>
                  <TableHead>Customer</TableHead>
                  <TableHead>Date</TableHead>
                  <TableHead>Due</TableHead>
                  <TableHead className="text-right">Amount</TableHead>
                  <TableHead className="text-right">Outstanding</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((inv: CustomerInvoice) => (
                  <TableRow key={inv.id}>
                    <TableCell className="font-mono text-xs">{inv.invoice_number ?? '—'}</TableCell>
                    <TableCell>{customerName(inv.customer_id)}</TableCell>
                    <TableCell className="whitespace-nowrap font-mono text-xs">
                      {inv.invoice_date}
                    </TableCell>
                    <TableCell className="whitespace-nowrap font-mono text-xs">
                      {inv.due_date ?? '—'}
                    </TableCell>
                    <TableCell className="text-right font-mono font-medium tabular-nums">
                      {formatMoney(inv.amount)}
                    </TableCell>
                    <TableCell className="text-right font-mono font-medium tabular-nums">
                      {formatMoney(inv.outstanding_amount)}
                    </TableCell>
                    <TableCell>
                      <Badge tone={statusTone(inv.status)}>{inv.status}</Badge>
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex flex-wrap items-center justify-end gap-2">
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          onClick={() => setDetailID(inv.id)}
                        >
                          Details
                        </Button>
                        <DocumentActions
                          onFetch={() => api.customerInvoicePdf(inv.id)}
                          filename={`invoice-${inv.invoice_number ?? inv.id}.pdf`}
                          permission="finance.read"
                        />
                        {inv.status === 'draft' ? (
                          <PermissionGate permission="customer_invoice.issue">
                            <Button
                              type="button"
                              size="sm"
                              disabled={issue.isPending}
                              onClick={() => issue.mutate(inv.id)}
                            >
                              Issue
                            </Button>
                          </PermissionGate>
                        ) : null}
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      <PaymentsSection
        query={payments}
        customerName={customerName}
        canReverse={canAllocate === true}
        onReverse={(p) => setReverseTarget(p)}
      />

      {/* Read-only hint: explain why action controls are inert. */}
      {canManage === false && canIssue === false && canAllocate === false ? (
        <p className="text-xs text-muted-foreground">
          You have read-only access to receivables. Issuing invoices and allocating payments
          requires the relevant finance permissions.
        </p>
      ) : null}

      <InvoiceDetailDialog
        invoiceID={detailID}
        onClose={() => setDetailID(null)}
        customerName={customerName}
      />

      <AllocatePaymentDialog
        open={payOpen}
        onOpenChange={setPayOpen}
        canAllocate={canAllocate === true}
        customers={customerList}
        onPosted={() => {
          setPayOpen(false);
          invalidate();
        }}
      />

      <CreateInvoiceDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        canManage={canManage === true}
        customers={customerList}
        onCreated={() => {
          setCreateOpen(false);
          invalidate();
        }}
      />

      <ReversePaymentDialog
        payment={reverseTarget}
        canReverse={canAllocate === true}
        customerName={customerName}
        onClose={() => setReverseTarget(null)}
        onReversed={() => {
          setReverseTarget(null);
          invalidate();
        }}
      />
    </div>
  );
}

/** Map a payment status to a badge tone (posted = info, voided = danger). */
function paymentTone(status: string): BadgeProps['tone'] {
  switch (status) {
    case 'posted':
      return 'info';
    case 'voided':
      return 'danger';
    default:
      return 'neutral';
  }
}

function PaymentsSection({
  query,
  customerName,
  canReverse,
  onReverse,
}: {
  query: ReturnType<typeof useQuery<{ items: CustomerPayment[] }>>;
  customerName: (id: string) => string;
  canReverse: boolean;
  onReverse: (payment: CustomerPayment) => void;
}) {
  const items = query.data?.items ?? [];
  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-col gap-0.5">
        <h2 className="text-sm font-semibold">Customer payments</h2>
        <p className="text-xs text-muted-foreground">
          Posted payments and their allocations. Reversing a payment restores the affected
          invoices&apos; balances and posts a balanced ledger reversal — the original payment is
          kept (voided), never deleted.
        </p>
      </div>
      {query.isPending ? (
        <Skeleton className="h-24 rounded-lg" />
      ) : query.isError ? (
        (() => {
          const forbidden = query.error instanceof SdkError && query.error.status === 403;
          return (
            <ErrorState
              title={forbidden ? 'No access' : "Couldn't load payments"}
              description={
                forbidden
                  ? "You don't have permission to view customer payments (finance.read)."
                  : String((query.error as Error).message)
              }
              onRetry={forbidden ? undefined : () => query.refetch()}
            />
          );
        })()
      ) : items.length === 0 ? (
        <EmptyState
          title="No payments"
          description="Customer payments will appear here once allocated."
          icon={<FileText />}
        />
      ) : (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Date</TableHead>
                  <TableHead>Customer</TableHead>
                  <TableHead>Method</TableHead>
                  <TableHead>Reference</TableHead>
                  <TableHead className="text-right">Amount</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((pmt) => (
                  <TableRow key={pmt.id}>
                    <TableCell className="whitespace-nowrap font-mono text-xs">
                      {pmt.payment_date}
                    </TableCell>
                    <TableCell>{customerName(pmt.customer_id)}</TableCell>
                    <TableCell>{pmt.method}</TableCell>
                    <TableCell className="font-mono text-xs">{pmt.reference ?? '—'}</TableCell>
                    <TableCell className="text-right font-mono font-medium tabular-nums">
                      {formatMoney(pmt.amount)}
                    </TableCell>
                    <TableCell>
                      <Badge tone={paymentTone(pmt.status)}>{pmt.status}</Badge>
                    </TableCell>
                    <TableCell className="text-right">
                      {pmt.status === 'posted' ? (
                        <PermissionGate permission="customer_payment.manage">
                          <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            disabled={!canReverse}
                            onClick={() => onReverse(pmt)}
                          >
                            Reverse
                          </Button>
                        </PermissionGate>
                      ) : null}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
    </section>
  );
}

function ReversePaymentDialog({
  payment,
  canReverse,
  customerName,
  onClose,
  onReversed,
}: {
  payment: CustomerPayment | null;
  canReverse: boolean;
  customerName: (id: string) => string;
  onClose: () => void;
  onReversed: () => void;
}) {
  const [reason, setReason] = React.useState('');

  React.useEffect(() => {
    if (!payment) setReason('');
  }, [payment]);

  const reverse = useMutation({
    mutationFn: () =>
      api.reverseCustomerPayment(payment!.id, { reason: reason.trim() || undefined }),
    onSuccess: () => {
      toast.success('Payment reversed', "The invoices' balances were restored.");
      onReversed();
    },
    onError: (err) =>
      toast.error('Could not reverse payment', err instanceof SdkError ? err.message : undefined),
  });

  return (
    <Dialog open={payment !== null} onOpenChange={(o) => (o ? undefined : onClose())}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Reverse customer payment</DialogTitle>
          <DialogDescription>
            This voids the payment and restores the outstanding balance of every invoice it was
            applied to, posting a balanced reversal to the ledger. The original payment record is
            preserved. This cannot be undone.
          </DialogDescription>
        </DialogHeader>
        {payment ? (
          <dl className="grid grid-cols-2 gap-x-4 gap-y-3 text-sm">
            <Field label="Customer" value={customerName(payment.customer_id)} />
            <Field label="Date" value={payment.payment_date} mono />
            <Field label="Method" value={payment.method} />
            <Field label="Amount" value={formatMoney(payment.amount)} mono />
          </dl>
        ) : null}
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="reverse-reason">Reason (optional)</Label>
          <Input
            id="reverse-reason"
            placeholder="e.g. duplicate entry, wrong customer"
            value={reason}
            onChange={(e) => setReason(e.target.value)}
          />
        </div>
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button
            type="button"
            variant="danger"
            disabled={!canReverse || reverse.isPending}
            onClick={() => reverse.mutate()}
          >
            {reverse.isPending ? 'Reversing…' : 'Reverse payment'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function InvoiceDetailDialog({
  invoiceID,
  onClose,
  customerName,
}: {
  invoiceID: string | null;
  onClose: () => void;
  customerName: (id: string) => string;
}) {
  const detail = useQuery({
    queryKey: ['customer-invoice', invoiceID],
    queryFn: ({ signal }) => api.getCustomerInvoice(invoiceID as string, signal),
    enabled: invoiceID !== null,
  });

  const inv = detail.data;

  return (
    <Dialog open={invoiceID !== null} onOpenChange={(o) => (o ? undefined : onClose())}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Invoice details</DialogTitle>
          <DialogDescription>
            {inv?.invoice_number ? `Invoice ${inv.invoice_number}` : 'Customer invoice'}
          </DialogDescription>
        </DialogHeader>
        {detail.isPending && invoiceID ? (
          <div className="flex flex-col gap-2">
            <Skeleton className="h-6 rounded" />
            <Skeleton className="h-6 rounded" />
            <Skeleton className="h-6 rounded" />
          </div>
        ) : detail.isError ? (
          <ErrorState
            title="Couldn't load invoice"
            description={
              detail.error instanceof SdkError ? detail.error.message : 'Please try again.'
            }
            onRetry={() => detail.refetch()}
          />
        ) : inv ? (
          <dl className="grid grid-cols-2 gap-x-4 gap-y-3 text-sm">
            <Field label="Customer" value={customerName(inv.customer_id)} />
            <Field label="Status" value={inv.status} />
            <Field label="Invoice date" value={inv.invoice_date} mono />
            <Field label="Due date" value={inv.due_date ?? '—'} mono />
            <Field label="Source" value={inv.source_type} />
            <Field label="Number" value={inv.invoice_number ?? '—'} mono />
            <Field label="Amount" value={formatMoney(inv.amount)} mono />
            <Field label="Outstanding" value={formatMoney(inv.outstanding_amount)} mono />
          </dl>
        ) : null}
        <DialogFooter>
          {inv ? (
            <DocumentActions
              onFetch={() => api.customerInvoicePdf(inv.id)}
              filename={`invoice-${inv.invoice_number ?? inv.id}.pdf`}
              permission="finance.read"
            />
          ) : null}
          <Button type="button" variant="ghost" onClick={onClose}>
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function Field({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex flex-col gap-0.5">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className={mono ? 'font-mono tabular-nums' : undefined}>{value}</dd>
    </div>
  );
}

interface AllocationRow {
  customer_invoice_id: string;
  amount: string;
}

function AllocatePaymentDialog({
  open,
  onOpenChange,
  canAllocate,
  customers,
  onPosted,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  canAllocate: boolean;
  customers: Customer[];
  onPosted: () => void;
}) {
  const [customerID, setCustomerID] = React.useState('');
  const [paymentDate, setPaymentDate] = React.useState(todayISO);
  const [method, setMethod] = React.useState('bank');
  const [reference, setReference] = React.useState('');
  const [allocations, setAllocations] = React.useState<Record<string, string>>({});

  // Outstanding invoices for the chosen customer drive the allocation rows.
  const invoices = useQuery({
    queryKey: ['customer-invoices', customerID, 'allocate'],
    queryFn: ({ signal }) => api.listCustomerInvoices({ customerID }, signal),
    enabled: open && customerID !== '',
  });

  // Reset the form whenever the dialog is closed so a reopen starts fresh.
  React.useEffect(() => {
    if (!open) {
      setCustomerID('');
      setPaymentDate(todayISO());
      setMethod('bank');
      setReference('');
      setAllocations({});
    }
  }, [open]);

  const outstanding = (invoices.data?.items ?? []).filter(
    (i) => Number(i.outstanding_amount) > 0 && i.status !== 'draft',
  );

  const post = useMutation({
    mutationFn: () => {
      const rows: AllocationRow[] = Object.entries(allocations)
        .filter(([, amt]) => amt.trim() !== '' && Number(amt) > 0)
        .map(([id, amount]) => ({ customer_invoice_id: id, amount: amount.trim() }));
      return api.postCustomerPayment({
        customer_id: customerID,
        payment_date: paymentDate,
        method,
        reference: reference || undefined,
        allocations: rows,
      });
    },
    onSuccess: () => {
      toast.success('Payment allocated', 'The payment was posted and applied to the invoices.');
      onPosted();
    },
    onError: (err) =>
      toast.error('Could not allocate payment', err instanceof SdkError ? err.message : undefined),
  });

  const rows: AllocationRow[] = Object.entries(allocations)
    .filter(([, amt]) => amt.trim() !== '' && Number(amt) > 0)
    .map(([id, amount]) => ({ customer_invoice_id: id, amount }));

  const canSubmit =
    canAllocate && customerID !== '' && method !== '' && paymentDate !== '' && rows.length > 0;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Allocate a customer payment</DialogTitle>
          <DialogDescription>
            Record a payment and apply it across the customer&apos;s outstanding invoices. Each
            allocation must not exceed an invoice&apos;s outstanding balance.
          </DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            post.mutate();
          }}
        >
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="alloc-customer">Customer</Label>
            <select
              id="alloc-customer"
              className="h-9 rounded-md border border-border bg-background px-3 text-sm"
              value={customerID}
              onChange={(e) => {
                setCustomerID(e.target.value);
                setAllocations({});
              }}
            >
              <option value="">Select a customer…</option>
              {customers.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </select>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="alloc-date">Payment date</Label>
              <Input
                id="alloc-date"
                type="date"
                required
                value={paymentDate}
                onChange={(e) => setPaymentDate(e.target.value)}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="alloc-method">Method</Label>
              <Input
                id="alloc-method"
                placeholder="bank, cash, mobile_money…"
                required
                value={method}
                onChange={(e) => setMethod(e.target.value)}
              />
            </div>
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="alloc-ref">Reference</Label>
            <Input
              id="alloc-ref"
              placeholder="Optional payment reference"
              value={reference}
              onChange={(e) => setReference(e.target.value)}
            />
          </div>

          <div className="flex flex-col gap-2">
            <span className="text-sm font-medium">Outstanding invoices</span>
            {customerID === '' ? (
              <p className="text-xs text-muted-foreground">Select a customer to list invoices.</p>
            ) : invoices.isPending ? (
              <Skeleton className="h-16 rounded" />
            ) : outstanding.length === 0 ? (
              <p className="text-xs text-muted-foreground">
                This customer has no outstanding issued invoices to allocate against.
              </p>
            ) : (
              <div className="flex flex-col gap-2">
                {outstanding.map((inv) => (
                  <div
                    key={inv.id}
                    className="flex items-center justify-between gap-3 rounded-md border border-border p-2"
                  >
                    <div className="flex flex-col">
                      <span className="text-sm">{inv.invoice_number ?? inv.id.slice(0, 8)}</span>
                      <span className="text-xs text-muted-foreground">
                        Outstanding {formatMoney(inv.outstanding_amount)}
                      </span>
                    </div>
                    <Input
                      aria-label={`Allocate to invoice ${inv.invoice_number ?? inv.id}`}
                      inputMode="decimal"
                      placeholder="0.00"
                      className="w-28"
                      value={allocations[inv.id] ?? ''}
                      onChange={(e) =>
                        setAllocations((prev) => ({ ...prev, [inv.id]: e.target.value }))
                      }
                    />
                  </div>
                ))}
              </div>
            )}
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={!canSubmit || post.isPending}>
              {post.isPending ? 'Allocating…' : 'Allocate payment'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

interface DraftLine {
  description: string;
  amount: string;
}

function CreateInvoiceDialog({
  open,
  onOpenChange,
  canManage,
  customers,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  canManage: boolean;
  customers: Customer[];
  onCreated: () => void;
}) {
  const [customerID, setCustomerID] = React.useState('');
  const [invoiceNumber, setInvoiceNumber] = React.useState('');
  const [invoiceDate, setInvoiceDate] = React.useState(todayISO);
  const [dueDate, setDueDate] = React.useState('');
  const [lines, setLines] = React.useState<DraftLine[]>([{ description: '', amount: '' }]);

  // Reset the form whenever the dialog closes so a reopen starts fresh.
  React.useEffect(() => {
    if (!open) {
      setCustomerID('');
      setInvoiceNumber('');
      setInvoiceDate(todayISO());
      setDueDate('');
      setLines([{ description: '', amount: '' }]);
    }
  }, [open]);

  function updateLine(index: number, patch: Partial<DraftLine>) {
    setLines((prev) => prev.map((ln, i) => (i === index ? { ...ln, ...patch } : ln)));
  }
  function addLine() {
    setLines((prev) => [...prev, { description: '', amount: '' }]);
  }
  function removeLine(index: number) {
    setLines((prev) => (prev.length === 1 ? prev : prev.filter((_, i) => i !== index)));
  }

  const validLines = lines.filter((ln) => ln.amount.trim() !== '' && Number(ln.amount) > 0);
  const total = validLines.reduce((sum, ln) => sum + Number(ln.amount), 0);

  const create = useMutation({
    mutationFn: () =>
      api.createCustomerInvoice({
        customer_id: customerID,
        invoice_number: invoiceNumber.trim() || undefined,
        invoice_date: invoiceDate || undefined,
        due_date: dueDate || undefined,
        lines: validLines.map((ln) => ({
          description: ln.description.trim() || undefined,
          amount: ln.amount.trim(),
        })),
      }),
    onSuccess: () => {
      toast.success(
        'Invoice created',
        'The draft invoice was created. Issue it to post to the ledger.',
      );
      onCreated();
    },
    onError: (err) =>
      toast.error('Could not create invoice', err instanceof SdkError ? err.message : undefined),
  });

  const canSubmit = canManage && customerID !== '' && validLines.length > 0;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New customer invoice</DialogTitle>
          <DialogDescription>
            Create a draft invoice with one or more billed lines. The invoice is created as a draft;
            issue it afterwards to post it to the ledger.
          </DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate();
          }}
        >
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="create-customer">Customer</Label>
            <select
              id="create-customer"
              className="h-9 rounded-md border border-border bg-background px-3 text-sm"
              value={customerID}
              onChange={(e) => setCustomerID(e.target.value)}
            >
              <option value="">Select a customer…</option>
              {customers.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </select>
          </div>

          <div className="grid grid-cols-3 gap-3">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="create-number">Number</Label>
              <Input
                id="create-number"
                placeholder="Optional"
                value={invoiceNumber}
                onChange={(e) => setInvoiceNumber(e.target.value)}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="create-date">Invoice date</Label>
              <Input
                id="create-date"
                type="date"
                value={invoiceDate}
                onChange={(e) => setInvoiceDate(e.target.value)}
              />
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="create-due">Due date</Label>
              <Input
                id="create-due"
                type="date"
                value={dueDate}
                onChange={(e) => setDueDate(e.target.value)}
              />
            </div>
          </div>

          <div className="flex flex-col gap-2">
            <div className="flex items-center justify-between">
              <span className="text-sm font-medium">Lines</span>
              <Button type="button" variant="secondary" size="sm" onClick={addLine}>
                Add line
              </Button>
            </div>
            {lines.map((ln, i) => (
              <div key={i} className="flex items-end gap-2">
                <div className="flex flex-1 flex-col gap-1.5">
                  <Label htmlFor={`line-desc-${i}`} className="sr-only">
                    Line {i + 1} description
                  </Label>
                  <Input
                    id={`line-desc-${i}`}
                    placeholder="Description (optional)"
                    value={ln.description}
                    onChange={(e) => updateLine(i, { description: e.target.value })}
                  />
                </div>
                <div className="flex w-32 flex-col gap-1.5">
                  <Label htmlFor={`line-amt-${i}`} className="sr-only">
                    Line {i + 1} amount
                  </Label>
                  <Input
                    id={`line-amt-${i}`}
                    aria-label={`Line ${i + 1} amount`}
                    inputMode="decimal"
                    placeholder="0.00"
                    value={ln.amount}
                    onChange={(e) => updateLine(i, { amount: e.target.value })}
                  />
                </div>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  disabled={lines.length === 1}
                  onClick={() => removeLine(i)}
                  aria-label={`Remove line ${i + 1}`}
                >
                  Remove
                </Button>
              </div>
            ))}
            <div className="flex justify-end text-sm">
              <span className="text-muted-foreground">Total&nbsp;</span>
              <span className="font-mono font-medium tabular-nums">
                {formatMoney(total.toFixed(2))}
              </span>
            </div>
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={!canSubmit || create.isPending}>
              {create.isPending ? 'Creating…' : 'Create invoice'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
