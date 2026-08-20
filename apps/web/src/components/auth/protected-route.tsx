/**
 * Authentication is enforced by middleware using the real httpOnly session
 * cookie. This wrapper remains as the layout boundary but deliberately avoids
 * making a second decision from client-readable persisted state.
 */
export function ProtectedRoute({ children }: { children: React.ReactNode }) {
  return <>{children}</>;
}
