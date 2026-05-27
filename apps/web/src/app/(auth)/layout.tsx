export default function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="grid min-h-screen place-items-center bg-background p-6">
      <main className="w-full max-w-md">{children}</main>
    </div>
  );
}
