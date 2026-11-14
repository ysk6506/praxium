import { type ReactNode, useId, useRef, useState } from "react";
import { cx } from "./cx";

export interface AppShellProps {
  readonly brand: ReactNode;
  readonly nav: ReactNode;
  readonly children: ReactNode;
  readonly footer?: ReactNode;
  /** A strip above everything, for a state the person must not miss (the demo mode, the local mode). */
  readonly banner?: ReactNode;
  /** Where the person is; when it changes, the folded menu closes (a link of the menu was followed). */
  readonly place?: string;
}

/**
 * The frame of every screen: navigation on the left, the page on the right. Under 48rem the navigation folds into a
 * menu the person opens, so the page starts at the top of the screen. The first thing a keyboard reaches is a link
 * that skips to the page.
 */
export function AppShell({ brand, nav, children, footer, banner, place }: AppShellProps) {
  // The menu is open for the place it was opened at: going somewhere else folds it.
  const [openedAt, setOpenedAt] = useState<string | undefined>(undefined);
  const here = place ?? "";
  const open = openedAt === here;
  const menuId = useId();
  const main = useRef<HTMLElement>(null);
  return (
    <>
      {/* A button, not a link: the address is the router's, and a link to a fragment would change it. */}
      <button
        type="button"
        onClick={() => main.current?.focus()}
        className="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-50 focus:cursor-pointer focus:rounded-md focus:border focus:border-line-strong focus:bg-surface focus:px-3 focus:py-1.5 focus:text-sm focus:font-medium focus:shadow-pop"
      >
        본문으로 건너뛰기
      </button>
      {banner ? (
        <output className="block border-b border-line bg-warn-soft px-4 py-1.5 text-center text-sm font-medium text-warn">
          {banner}
        </output>
      ) : null}
      <div className="flex min-h-screen flex-col md:flex-row">
        <aside className="flex shrink-0 flex-col gap-3 border-b border-line bg-surface p-3 md:sticky md:top-0 md:h-screen md:w-60 md:gap-4 md:overflow-y-auto md:border-r md:border-b-0">
          <div className="flex items-center justify-between gap-2">
            <div className="px-2 py-1 text-base font-bold tracking-tight text-fg">{brand}</div>
            <button
              type="button"
              aria-expanded={open}
              aria-controls={menuId}
              onClick={() => setOpenedAt(open ? undefined : here)}
              className="cursor-pointer rounded-md border border-line-strong px-3 py-1 text-sm font-medium text-fg hover:bg-sunken md:hidden"
            >
              {open ? "메뉴 닫기" : "메뉴"}
            </button>
          </div>
          <div
            id={menuId}
            className={cx("flex-1 flex-col gap-4 md:flex md:h-full", open ? "flex" : "hidden")}
          >
            <nav aria-label="주 메뉴" className="flex flex-col gap-4">
              {nav}
            </nav>
            {footer ? <div className="mt-auto px-2 text-xs text-subtle">{footer}</div> : null}
          </div>
        </aside>
        <main ref={main} tabIndex={-1} className="min-w-0 flex-1 px-4 py-5 outline-none md:px-8">
          <div className="mx-auto max-w-6xl">{children}</div>
        </main>
      </div>
    </>
  );
}

export function NavSection({
  title,
  children,
}: {
  readonly title: string;
  readonly children: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-0.5">
      <div className="px-2 pb-1 text-xs font-semibold text-subtle">{title}</div>
      {children}
    </div>
  );
}

export interface NavLinkProps {
  readonly href: string;
  readonly current?: boolean;
  readonly children: ReactNode;
}

export function NavLink({ href, current, children }: NavLinkProps) {
  return (
    <a
      href={href}
      aria-current={current ? "page" : undefined}
      className={cx(
        "flex items-center justify-between gap-2 rounded-md px-2 py-1.5 text-sm no-underline",
        current ? "bg-accent-soft font-medium text-accent" : "text-fg hover:bg-sunken",
      )}
    >
      {children}
    </a>
  );
}

export interface PageHeaderProps {
  readonly title: string;
  readonly subtitle?: ReactNode;
  readonly trail?: readonly { readonly label: string; readonly href?: string }[];
  readonly actions?: ReactNode;
}

export function PageHeader({ title, subtitle, trail, actions }: PageHeaderProps) {
  return (
    <header className="mb-5">
      {trail && trail.length > 0 ? (
        <nav
          aria-label="현재 위치"
          className="mb-1 flex flex-wrap items-center gap-1 text-xs text-muted"
        >
          {trail.map((step, index) => (
            <span key={`${step.label}-${step.href ?? ""}`} className="flex items-center gap-1">
              {index > 0 ? <span aria-hidden>/</span> : null}
              {step.href ? (
                <a href={step.href} className="text-muted hover:text-fg">
                  {step.label}
                </a>
              ) : (
                <span>{step.label}</span>
              )}
            </span>
          ))}
        </nav>
      ) : null}
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-xl font-semibold tracking-tight break-words">{title}</h1>
          {subtitle ? <div className="mt-0.5 text-sm text-muted">{subtitle}</div> : null}
        </div>
        {actions ? <div className="flex shrink-0 items-center gap-2">{actions}</div> : null}
      </div>
    </header>
  );
}

/** A group of facts as a definition list: the label on the left, the value on the right. */
export function KeyValues({
  items,
}: {
  readonly items: readonly (readonly [label: string, value: ReactNode])[];
}) {
  return (
    <dl className="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-1.5 text-sm">
      {items.map(([label, value]) => (
        <div key={label} className="contents">
          <dt className="text-muted">{label}</dt>
          <dd className="min-w-0 break-words">{value}</dd>
        </div>
      ))}
    </dl>
  );
}

/** An id, a path or a code: monospace, and it may break anywhere so it never pushes the page wide. */
export function Mono({
  children,
  className,
}: {
  readonly children: ReactNode;
  readonly className?: string;
}) {
  return <code className={cx("font-mono text-[0.92em] break-all", className)}>{children}</code>;
}

export function Section({
  title,
  children,
  aside,
  note,
}: {
  readonly title: string;
  readonly children: ReactNode;
  readonly aside?: ReactNode;
  /** A line under the title that says what the section shows. */
  readonly note?: ReactNode;
}) {
  return (
    <section className="mb-6">
      <div className="mb-2 flex items-center justify-between gap-2">
        <h2 className="text-base font-semibold">{title}</h2>
        {aside ? <div className="text-xs text-muted">{aside}</div> : null}
      </div>
      {note ? <p className="mb-2 text-sm text-muted">{note}</p> : null}
      {children}
    </section>
  );
}

/**
 * A value the product defines, shown by its display name; the code it was made from is in the tooltip. For the
 * values the screen translates (layer, freshness, state): the person reads Korean, whoever works with the API still
 * finds the code.
 */
export function Coded({ code, children }: { readonly code: string; readonly children: ReactNode }) {
  return (
    <abbr
      title={code}
      className="cursor-help underline decoration-line-strong decoration-dotted underline-offset-4"
    >
      {children}
    </abbr>
  );
}
