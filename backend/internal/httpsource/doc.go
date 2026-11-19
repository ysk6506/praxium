// Package httpsource is the HTTP API adapter of the new ingestion flow (ADR-0092). It stands on the read port of
// core.ingest (PageReader, and the optional Observer and Previewer ports) for a source whose definition says
// adapter "http" (ADR-0090): it sends the requests the definition describes, signs them in with one of five kinds of
// authentication, follows one of six kinds of pagination, and gives the records of the responses as pages.
//
// Every connection is made by a client of adapter.egress, which the adapter is given as a small interface and makes
// anew for each read. The package imports neither net, net/http nor crypto/tls: it has no way to make a connection,
// a transport or a request of its own, and a structural test keeps it so.
//
// A secret is opened at the start of a read, for the host and port of the source, and its values are taken out in one
// file (auth.go) at the moment a request is made. No error, event, preview or page the package gives holds one:
// errors are ingest.SourceError values made of fixed words, the events are ingest.Event values that have no field to
// hold a value, and the preview goes through the masks of core.ingest. The package writes no log.
package httpsource
