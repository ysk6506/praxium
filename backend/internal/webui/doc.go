// Package webui holds the built web UI that the API server serves next to the API (ADR-0035).
//
// The files come from apps/web by scripts/build-with-web.sh, which copies them into the dist folder. Only
// dist/placeholder.html is committed: a build without the web UI still compiles, passes its tests and answers with
// a page that says so. Nothing in the backend imports or reads apps/web.
package webui
