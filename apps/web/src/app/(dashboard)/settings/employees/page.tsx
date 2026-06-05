'use client';

import { useEffect, useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus } from 'lucide-react';

import { SdkError, type Employee, type EmployeeRole, type EmployeeRoleOption } from '@fuelgrid/sdk';
import {
  Badge,
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
import { api } from '@/lib/api';

type RoleChoice = Pick<EmployeeRoleOption, 'code' | 'name'>;

const DEFAULT_ROLES: RoleChoice[] = [
  { code: 'pump_attendant', name: 'Pump attendant' },
  { code: 'cashier', name: 'Cashier' },
  { code: 'supervisor', name: 'Supervisor' },
  { code: 'manager', name: 'Manager' },
  { code: 'security', name: 'Security' },
  { code: 'other', name: 'Other' },
];

function humanizeRole(role: string): string {
  return role
    .split('_')
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
}

interface FormState {
  full_name: string;
  role: EmployeeRole;
  employee_code: string;
  phone: string;
  email: string;
  status: 'active' | 'inactive';
}

const blankForm: FormState = {
  full_name: '',
  role: 'pump_attendant',
  employee_code: '',
  phone: '',
  email: '',
  status: 'active',
};

export default function EmployeesPage() {
  const qc = useQueryClient();
  const [stationID, setStationID] = useState('');
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<Employee | null>(null);
  const [form, setForm] = useState<FormState>(blankForm);
  const [submitError, setSubmitError] = useState<string | null>(null);
  const [roleOpen, setRoleOpen] = useState(false);
  const [roleName, setRoleName] = useState('');
  const [roleError, setRoleError] = useState<string | null>(null);

  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });

  useEffect(() => {
    const first = stations.data?.items?.[0];
    if (!stationID && first) setStationID(first.id);
  }, [stationID, stations.data]);

  const employeesKey = ['employees', stationID];
  const rolesKey = ['employee-roles'];
  const roleOptions = useQuery({
    queryKey: rolesKey,
    queryFn: ({ signal }) => api.listEmployeeRoles(signal),
  });
  const list = useQuery({
    queryKey: employeesKey,
    // TODO(pagination): the API now returns a paged envelope. We request the
    // max page size and render a single page; add a Load more / prev-next
    // control here if a station's headcount grows past one page.
    queryFn: ({ signal }) => api.listEmployees(stationID, { limit: 200 }, signal),
    enabled: !!stationID,
  });

  const roleChoices = useMemo<RoleChoice[]>(() => {
    const items = roleOptions.data?.items ?? [];
    return items.length > 0 ? items : DEFAULT_ROLES;
  }, [roleOptions.data]);

  const roleChoicesForForm = useMemo<RoleChoice[]>(() => {
    if (!form.role || roleChoices.some((role) => role.code === form.role)) return roleChoices;
    return [...roleChoices, { code: form.role, name: humanizeRole(form.role) }];
  }, [form.role, roleChoices]);

  const roleLabels = useMemo(
    () => new Map(roleChoices.map((role) => [role.code, role.name])),
    [roleChoices],
  );

  function roleLabel(role: string): string {
    return roleLabels.get(role) ?? humanizeRole(role);
  }

  const create = useMutation({
    mutationFn: (input: FormState) =>
      api.createEmployee(stationID, {
        full_name: input.full_name,
        role: input.role,
        employee_code: input.employee_code || undefined,
        phone: input.phone || undefined,
        email: input.email || undefined,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: employeesKey });
      setOpen(false);
      setForm(blankForm);
    },
    onError: (err) => setSubmitError(err instanceof SdkError ? err.message : 'Could not save'),
  });

  const createRole = useMutation({
    mutationFn: () => api.createEmployeeRole({ name: roleName.trim() }),
    onSuccess: (role) => {
      qc.invalidateQueries({ queryKey: rolesKey });
      setForm((prev) => ({ ...prev, role: role.code }));
      setRoleOpen(false);
      setRoleName('');
      setRoleError(null);
    },
    onError: (err) =>
      setRoleError(err instanceof SdkError ? err.message : 'Could not create employee role'),
  });

  const update = useMutation({
    mutationFn: ({ id, input }: { id: string; input: FormState }) =>
      api.updateEmployee(id, {
        full_name: input.full_name,
        role: input.role,
        status: input.status,
        employee_code: input.employee_code || undefined,
        phone: input.phone || undefined,
        email: input.email || undefined,
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: employeesKey });
      setOpen(false);
      setEditing(null);
    },
    onError: (err) => setSubmitError(err instanceof SdkError ? err.message : 'Could not save'),
  });

  function openCreate() {
    setEditing(null);
    setForm(blankForm);
    setSubmitError(null);
    setOpen(true);
  }

  function openEdit(e: Employee) {
    setEditing(e);
    setForm({
      full_name: e.full_name,
      role: e.role,
      employee_code: e.employee_code ?? '',
      phone: e.phone ?? '',
      email: e.email ?? '',
      status: e.status,
    });
    setSubmitError(null);
    setOpen(true);
  }

  function submit() {
    if (!form.full_name.trim()) {
      setSubmitError('Full name is required');
      return;
    }
    if (editing) update.mutate({ id: editing.id, input: form });
    else create.mutate(form);
  }

  function openCreateRole() {
    setRoleName('');
    setRoleError(null);
    setRoleOpen(true);
  }

  function submitRole() {
    if (!roleName.trim()) {
      setRoleError('Role name is required');
      return;
    }
    createRole.mutate();
  }

  const stationSelect =
    (stations.data?.items?.length ?? 0) > 0 ? (
      <label className="flex items-center gap-2 text-sm">
        <span className="text-muted-foreground">Station</span>
        <select
          className="h-9 rounded-md border border-border bg-background px-2 text-sm"
          value={stationID}
          onChange={(e) => setStationID(e.target.value)}
        >
          {stations.data!.items.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name} ({s.code})
            </option>
          ))}
        </select>
      </label>
    ) : null;

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Settings"
        title="Employees"
        description={`${list.data?.count ?? 0} employees at this station — the workforce that shift teams draw from.`}
        actions={
          <div className="flex items-center gap-3">
            {stationSelect}
            <PermissionGate permission="station.manage">
              <div className="flex items-center gap-2">
                <Button variant="secondary" onClick={openCreateRole}>
                  <Plus className="size-4" />
                  New role
                </Button>
                <Button onClick={openCreate} disabled={!stationID}>
                  <Plus className="size-4" />
                  New employee
                </Button>
              </div>
            </PermissionGate>
          </div>
        }
      />

      {!stationID ? (
        <EmptyState
          title="No station selected"
          description="Pick a station to manage its workforce."
        />
      ) : list.isPending ? (
        <Card>
          <CardContent className="flex flex-col gap-2 p-4">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-14 rounded-lg" />
            ))}
          </CardContent>
        </Card>
      ) : list.isError ? (
        <ErrorState
          title="Couldn't load employees"
          description={String((list.error as Error).message)}
          onRetry={() => list.refetch()}
        />
      ) : (list.data?.items?.length ?? 0) === 0 ? (
        <EmptyState
          title="No employees yet"
          description="Add the people who staff this station before assigning them to shift teams."
          action={
            <PermissionGate permission="station.manage">
              <Button onClick={openCreate}>Add one</Button>
            </PermissionGate>
          }
        />
      ) : (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Code</TableHead>
                  <TableHead>Role</TableHead>
                  <TableHead>Contact</TableHead>
                  <TableHead>Login</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {list.data!.items.map((e) => (
                  <TableRow key={e.id}>
                    <TableCell className="font-medium">{e.full_name}</TableCell>
                    <TableCell className="font-mono text-xs tabular-nums">
                      {e.employee_code ?? '—'}
                    </TableCell>
                    <TableCell className="text-muted-foreground">{roleLabel(e.role)}</TableCell>
                    <TableCell className="text-muted-foreground">
                      {[e.phone, e.email].filter(Boolean).join(' · ') || '—'}
                    </TableCell>
                    <TableCell>
                      <Badge tone={e.user_id ? 'info' : 'neutral'}>
                        {e.user_id ? 'linked' : 'none'}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <Badge tone={e.status === 'active' ? 'success' : 'warning'}>{e.status}</Badge>
                    </TableCell>
                    <TableCell className="text-right">
                      <PermissionGate permission="station.manage">
                        <Button variant="ghost" size="sm" onClick={() => openEdit(e)}>
                          Edit
                        </Button>
                      </PermissionGate>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editing ? 'Edit employee' : 'New employee'}</DialogTitle>
            <DialogDescription>
              {editing
                ? 'Update this employee. Linking a login account happens via Users.'
                : 'Add a member of this station’s workforce.'}
            </DialogDescription>
          </DialogHeader>

          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              submit();
            }}
          >
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="full_name">Full name</Label>
              <Input
                id="full_name"
                value={form.full_name}
                onChange={(e) => setForm({ ...form, full_name: e.target.value })}
                required
              />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="role">Role</Label>
                <select
                  id="role"
                  className="h-10 rounded-md border border-border bg-background px-3 text-sm"
                  value={form.role}
                  onChange={(e) => setForm({ ...form, role: e.target.value as EmployeeRole })}
                >
                  {roleChoicesForForm.map((r) => (
                    <option key={r.code} value={r.code}>
                      {r.name}
                    </option>
                  ))}
                </select>
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="employee_code">Employee code</Label>
                <Input
                  id="employee_code"
                  value={form.employee_code}
                  onChange={(e) => setForm({ ...form, employee_code: e.target.value })}
                  placeholder="EMP-010"
                />
              </div>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="phone">Phone</Label>
                <Input
                  id="phone"
                  value={form.phone}
                  onChange={(e) => setForm({ ...form, phone: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="email">Email</Label>
                <Input
                  id="email"
                  type="email"
                  value={form.email}
                  onChange={(e) => setForm({ ...form, email: e.target.value })}
                />
              </div>
            </div>
            {editing ? (
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="status">Status</Label>
                <select
                  id="status"
                  className="h-10 rounded-md border border-border bg-background px-3 text-sm"
                  value={form.status}
                  onChange={(e) =>
                    setForm({ ...form, status: e.target.value as 'active' | 'inactive' })
                  }
                >
                  <option value="active">active</option>
                  <option value="inactive">inactive</option>
                </select>
              </div>
            ) : null}

            {submitError ? (
              <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
                {submitError}
              </p>
            ) : null}

            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => setOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={create.isPending || update.isPending}>
                {create.isPending || update.isPending ? 'Saving…' : 'Save'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={roleOpen} onOpenChange={setRoleOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>New employee role</DialogTitle>
            <DialogDescription>
              Add a workforce role that can be assigned to employees.
            </DialogDescription>
          </DialogHeader>

          <form
            className="flex flex-col gap-3"
            onSubmit={(e) => {
              e.preventDefault();
              submitRole();
            }}
          >
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="employee_role_name">Role name</Label>
              <Input
                id="employee_role_name"
                value={roleName}
                onChange={(e) => setRoleName(e.target.value)}
                placeholder="Security"
                required
              />
            </div>

            {roleError ? (
              <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
                {roleError}
              </p>
            ) : null}

            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => setRoleOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={createRole.isPending}>
                {createRole.isPending ? 'Saving…' : 'Save role'}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
