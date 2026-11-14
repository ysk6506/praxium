// Package filesource is the file adapter of the ingestion pipeline (ADR-0033): it reads the bytes of a file into a
// capture. It speaks one protocol, a file that was given, and knows neither the model nor the mapping.
//
// It has no method that writes to the source (ingestion.md 3.2), and it never opens a file itself: the
// composition root hands it an opener, since the operating system is the root's alone. A read that is cut short or
// is larger than the limit is a capture that says it is not complete, and the pipeline uses nothing of it.
package filesource
