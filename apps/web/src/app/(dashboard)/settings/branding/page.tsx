'use client';

import { useEffect, useRef, useState } from 'react';
import { useForm } from 'react-hook-form';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ImageOff, Upload } from 'lucide-react';

import { SdkError, type TenantBranding, type TenantBrandingUpdate } from '@fuelgrid/sdk';
import {
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  ErrorState,
  Input,
  Label,
  PageHeader,
  Skeleton,
  Tooltip,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { toast } from '@/lib/toast';
import { usePermission } from '@/hooks/use-permissions';

const MAX_LOGO_BYTES = 1 << 20; // 1 MiB — mirrors the server cap.
const ACCEPTED_LOGO = ['image/png', 'image/jpeg'];

interface BrandingFormValues {
  display_name: string;
  legal_name: string;
  tax_id: string;
  registration_no: string;
  address_line1: string;
  address_line2: string;
  city: string;
  country: string;
  phone: string;
  email: string;
  website: string;
  footer_note: string;
}

function brandingToForm(b: TenantBranding | undefined): BrandingFormValues {
  return {
    display_name: b?.display_name ?? '',
    legal_name: b?.legal_name ?? '',
    tax_id: b?.tax_id ?? '',
    registration_no: b?.registration_no ?? '',
    address_line1: b?.address_line1 ?? '',
    address_line2: b?.address_line2 ?? '',
    city: b?.city ?? '',
    country: b?.country ?? '',
    phone: b?.phone ?? '',
    email: b?.email ?? '',
    website: b?.website ?? '',
    footer_note: b?.footer_note ?? '',
  };
}

export default function BrandingPage() {
  const qc = useQueryClient();
  const canManage = usePermission('companies.manage');
  const fileRef = useRef<HTMLInputElement>(null);
  // Cache-buster so the <img> refetches after an upload/delete.
  const [logoVersion, setLogoVersion] = useState(0);

  const query = useQuery({
    queryKey: ['branding'],
    queryFn: ({ signal }) => api.getBranding(signal),
  });

  const { register, handleSubmit, reset, watch } = useForm<BrandingFormValues>({
    values: brandingToForm(query.data),
  });

  useEffect(() => {
    if (query.data) reset(brandingToForm(query.data));
  }, [query.data, reset]);

  const save = useMutation({
    mutationFn: (input: TenantBrandingUpdate) => api.updateBranding(input),
    onSuccess: (data) => {
      qc.setQueryData(['branding'], data);
      toast.success('Branding saved');
    },
    onError: (err) => {
      toast.error(err instanceof SdkError ? err.message : 'Could not save branding');
    },
  });

  const uploadLogo = useMutation({
    mutationFn: (file: File) => api.uploadBrandingLogo(file),
    onSuccess: (data) => {
      qc.setQueryData(['branding'], data);
      setLogoVersion((v) => v + 1);
      toast.success('Logo updated');
    },
    onError: (err) => {
      toast.error(err instanceof SdkError ? err.message : 'Could not upload logo');
    },
  });

  const removeLogo = useMutation({
    mutationFn: () => api.deleteBrandingLogo(),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['branding'] });
      setLogoVersion((v) => v + 1);
      toast.success('Logo removed');
    },
    onError: (err) => {
      toast.error(err instanceof SdkError ? err.message : 'Could not remove logo');
    },
  });

  function onPickLogo(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    e.target.value = ''; // allow re-selecting the same file
    if (!file) return;
    if (!ACCEPTED_LOGO.includes(file.type)) {
      toast.error('Logo must be a PNG or JPEG image');
      return;
    }
    if (file.size > MAX_LOGO_BYTES) {
      toast.error('Logo must be 1 MiB or smaller');
      return;
    }
    uploadLogo.mutate(file);
  }

  function submit(values: BrandingFormValues) {
    save.mutate(values);
  }

  const disabled = canManage !== true;
  const live = watch();
  const hasLogo = query.data?.has_logo ?? false;
  const logoSrc = hasLogo ? `${api.brandingLogoUrl()}?v=${logoVersion}` : null;

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Settings"
        title="Branding"
        description="The company letterhead printed on every downloadable document — invoices, statements, and reports."
      />

      {query.isPending ? (
        <Card>
          <CardContent className="flex flex-col gap-3 p-6">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-10 rounded-lg" />
            ))}
          </CardContent>
        </Card>
      ) : query.isError ? (
        <ErrorState
          title="Couldn't load branding"
          description={String((query.error as Error).message)}
          onRetry={() => query.refetch()}
        />
      ) : (
        <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_360px]">
          {/* Left: the editable form. */}
          <form className="flex flex-col gap-6" onSubmit={handleSubmit(submit)}>
            <Card>
              <CardHeader>
                <CardTitle>Company identity</CardTitle>
                <CardDescription>
                  Shown at the top of every document. The brand name appears large; the rest forms
                  the address block.
                </CardDescription>
              </CardHeader>
              <CardContent className="grid gap-4 sm:grid-cols-2">
                <Field label="Brand / trading name" htmlFor="display_name">
                  <Input id="display_name" disabled={disabled} {...register('display_name')} />
                </Field>
                <Field label="Legal name" htmlFor="legal_name">
                  <Input id="legal_name" disabled={disabled} {...register('legal_name')} />
                </Field>
                <Field label="Tax PIN" htmlFor="tax_id">
                  <Input id="tax_id" disabled={disabled} {...register('tax_id')} />
                </Field>
                <Field label="Registration no." htmlFor="registration_no">
                  <Input
                    id="registration_no"
                    disabled={disabled}
                    {...register('registration_no')}
                  />
                </Field>
                <Field label="Address line 1" htmlFor="address_line1">
                  <Input id="address_line1" disabled={disabled} {...register('address_line1')} />
                </Field>
                <Field label="Address line 2" htmlFor="address_line2">
                  <Input id="address_line2" disabled={disabled} {...register('address_line2')} />
                </Field>
                <Field label="City" htmlFor="city">
                  <Input id="city" disabled={disabled} {...register('city')} />
                </Field>
                <Field label="Country" htmlFor="country">
                  <Input id="country" disabled={disabled} {...register('country')} />
                </Field>
                <Field label="Phone" htmlFor="phone">
                  <Input id="phone" disabled={disabled} {...register('phone')} />
                </Field>
                <Field label="Email" htmlFor="email">
                  <Input id="email" type="email" disabled={disabled} {...register('email')} />
                </Field>
                <Field label="Website" htmlFor="website">
                  <Input id="website" disabled={disabled} {...register('website')} />
                </Field>
                <Field label="Footer note" htmlFor="footer_note">
                  <Input
                    id="footer_note"
                    placeholder="e.g. Confidential"
                    disabled={disabled}
                    {...register('footer_note')}
                  />
                </Field>
              </CardContent>
            </Card>

            <div className="flex items-center justify-end gap-3">
              {disabled ? (
                <Tooltip label="You need the companies.manage permission to edit branding.">
                  <span>
                    <Button type="submit" disabled>
                      Save changes
                    </Button>
                  </span>
                </Tooltip>
              ) : (
                <Button type="submit" disabled={save.isPending}>
                  {save.isPending ? 'Saving…' : 'Save changes'}
                </Button>
              )}
            </div>
          </form>

          {/* Right: logo control + live letterhead preview. */}
          <div className="flex flex-col gap-6">
            <Card>
              <CardHeader>
                <CardTitle>Logo</CardTitle>
                <CardDescription>
                  PNG or JPEG, up to 1 MiB. Shown top-left of the letterhead.
                </CardDescription>
              </CardHeader>
              <CardContent className="flex flex-col gap-4">
                <div className="flex h-28 items-center justify-center rounded-lg border border-dashed border-border bg-muted/30">
                  {logoSrc ? (
                    <img
                      src={logoSrc}
                      alt="Company logo"
                      className="max-h-24 max-w-full object-contain"
                    />
                  ) : (
                    <span className="flex items-center gap-2 text-sm text-muted-foreground">
                      <ImageOff className="size-4" />
                      No logo set
                    </span>
                  )}
                </div>
                <input
                  ref={fileRef}
                  type="file"
                  accept="image/png,image/jpeg"
                  className="hidden"
                  onChange={onPickLogo}
                />
                <div className="flex gap-2">
                  <Button
                    type="button"
                    variant="secondary"
                    disabled={disabled || uploadLogo.isPending}
                    onClick={() => fileRef.current?.click()}
                  >
                    <Upload className="size-4" />
                    {uploadLogo.isPending ? 'Uploading…' : hasLogo ? 'Replace' : 'Upload'}
                  </Button>
                  {hasLogo ? (
                    <Button
                      type="button"
                      variant="ghost"
                      disabled={disabled || removeLogo.isPending}
                      onClick={() => removeLogo.mutate()}
                    >
                      Remove
                    </Button>
                  ) : null}
                </div>
              </CardContent>
            </Card>

            <LetterheadPreview values={live} logoSrc={logoSrc} />
          </div>
        </div>
      )}
    </div>
  );
}

function Field({
  label,
  htmlFor,
  children,
}: {
  label: string;
  htmlFor: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={htmlFor}>{label}</Label>
      {children}
    </div>
  );
}

/**
 * LetterheadPreview renders how the document header will look — logo + company
 * block + divider — so the user sees the letterhead before downloading anything.
 * Built only from @fuelgrid/ui primitives + Tailwind, so it tracks the theme
 * (light/dark/navy) automatically.
 */
function LetterheadPreview({
  values,
  logoSrc,
}: {
  values: BrandingFormValues;
  logoSrc: string | null;
}) {
  const brand = values.display_name || values.legal_name || 'Your company';
  const cityCountry = [values.city, values.country].filter(Boolean).join(', ');
  const contact = [values.phone, values.email, values.website].filter(Boolean).join('  •  ');
  const taxLine = [
    values.tax_id ? `Tax PIN: ${values.tax_id}` : '',
    values.registration_no ? `Reg No: ${values.registration_no}` : '',
  ]
    .filter(Boolean)
    .join('   ');

  return (
    <Card>
      <CardHeader>
        <CardTitle>Letterhead preview</CardTitle>
        <CardDescription>How the header will appear on a generated PDF.</CardDescription>
      </CardHeader>
      <CardContent>
        <div className="rounded-lg border border-border bg-card p-4">
          <div className="flex items-start gap-3">
            {logoSrc ? (
              <img src={logoSrc} alt="" className="h-12 w-12 shrink-0 object-contain" />
            ) : null}
            <div className="min-w-0 flex-1">
              <p className="truncate text-base font-semibold text-foreground">{brand}</p>
              {values.legal_name && values.legal_name !== brand ? (
                <p className="truncate text-xs text-muted-foreground">{values.legal_name}</p>
              ) : null}
              {values.address_line1 ? (
                <p className="truncate text-xs text-muted-foreground">{values.address_line1}</p>
              ) : null}
              {values.address_line2 ? (
                <p className="truncate text-xs text-muted-foreground">{values.address_line2}</p>
              ) : null}
              {cityCountry ? (
                <p className="truncate text-xs text-muted-foreground">{cityCountry}</p>
              ) : null}
              {contact ? <p className="truncate text-xs text-muted-foreground">{contact}</p> : null}
              {taxLine ? <p className="truncate text-xs text-muted-foreground">{taxLine}</p> : null}
            </div>
          </div>
          <div className="mt-3 border-t border-border" />
          <p className="mt-3 text-[11px] italic text-muted-foreground">
            Generated {new Date().toISOString().slice(0, 10)}
            {values.footer_note ? `  •  ${values.footer_note}` : ''}
          </p>
        </div>
      </CardContent>
    </Card>
  );
}
