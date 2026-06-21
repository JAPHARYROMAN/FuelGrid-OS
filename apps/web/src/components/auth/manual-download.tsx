import { FileText } from 'lucide-react';

/**
 * "Download the Supervisor Manual (PDF)" affordance on the login screen.
 *
 * A small, secondary link sitting just beneath the "Pump attendant? Install the
 * app" QR affordance. It points at the branded, page-numbered PDF that ships as
 * a static asset in apps/web/public (served at the site root), so anyone on the
 * landing/login page can grab the operations manual without signing in.
 *
 * The `download` attribute asks the browser to save the file rather than render
 * it in a tab. It is a same-origin static asset, but we still set
 * rel="noopener" defensively to keep the link inert. The label is explicit
 * (purpose + file type) so screen readers announce exactly what will download.
 */
export function ManualDownload() {
  return (
    <div className="mt-2 flex flex-col items-center text-center">
      <a
        href="/supervisor-operations-manual.pdf"
        download
        rel="noopener"
        className="inline-flex min-h-11 items-center gap-1.5 rounded-md px-3 text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
      >
        <FileText className="size-3.5" aria-hidden />
        Download the Supervisor Manual (PDF)
      </a>
    </div>
  );
}
