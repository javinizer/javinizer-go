// /manual is a client-only continuation of /browse (D6): the pendingScrape
// store is a module singleton populated client-side by /browse's "Manual Scrape"
// action, so the page must not SSR (a server render always sees a null store
// and would flash/redirect). The null-store redirect is handled in +page.svelte
// onMount (dodges the server-load footgun of an always-redirect +page.server.ts).
export const ssr = false;
