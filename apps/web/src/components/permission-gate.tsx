'use client';

import * as React from 'react';

import { usePermission } from '@/hooks/use-permissions';

const DENY_MESSAGE = "You don't have permission";

interface PermissionGateProps {
  /** Backend permission code the wrapped control requires (e.g. "users.manage"). */
  permission: string;
  /** Supply when the permission is station-scoped so the check is meaningful. */
  stationId?: string | null;
  /**
   * How to treat a user who lacks the permission:
   *   - 'disable' (default): render the control, but disabled + a tooltip;
   *   - 'hide': render nothing.
   */
  mode?: 'disable' | 'hide';
  children: React.ReactElement;
}

/**
 * PermissionGate keeps high-privilege controls from being clickable by users
 * who can't use them — they'd just 403 on click (PAGE-013/SEC-5). It mirrors
 * the backend's policy via usePermission; the backend stays authoritative.
 *
 * While the permission set is still loading (`usePermission` returns null) the
 * control is disabled to avoid a click that races the answer, but never hidden
 * (no layout flicker).
 *
 * The child must accept `disabled` + `title` (a native button or our Button).
 */
export function PermissionGate({
  permission,
  stationId,
  mode = 'disable',
  children,
}: PermissionGateProps) {
  const allowed = usePermission(permission, { stationID: stationId });

  // Definitively denied.
  if (allowed === false) {
    if (mode === 'hide') return null;
    return disable(children);
  }

  // Still loading — disable to prevent a racing click, but keep it visible.
  if (allowed === null) {
    return disable(children, true);
  }

  return children;
}

function disable(child: React.ReactElement, loading = false): React.ReactElement {
  const childProps = child.props as {
    disabled?: boolean;
    title?: string;
    className?: string;
  };
  return React.cloneElement(child, {
    disabled: true,
    'aria-disabled': true,
    title: loading ? childProps.title : (childProps.title ?? DENY_MESSAGE),
  } as Partial<typeof childProps> & { 'aria-disabled': boolean });
}
