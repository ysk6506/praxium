// Package ingestcheck holds the acceptance checks of the ingestion slice (ADR-0033): files of different shapes are
// made from the generated world, the pipeline of core.ingest reads them through the file adapter into the reference
// store, and the KG is compared with the one the development loader makes from the same world. Units above the
// storage port may not import an adapter, so the checks that need the store and the loader live here. The package
// has no production code but the example files that `praxium ingest init` writes (examples.go, ADR-0041).
package ingestcheck
