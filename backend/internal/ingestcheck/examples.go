package ingestcheck

import "embed"

// ExampleFiles are the definitions and the sample files that `praxium ingest init` writes for a person to try
// ingestion with (ADR-0041): a CSV and JSON Lines source with its samples (a copy of what the acceptance tests use, with
// the identifiers a person looks things up by), and the source of the Excel example. The values are made up. The workbook is not here: the command writes it with the writer of
// the xlsx unit, so no binary file is committed. guide/ingest-definitions.md is a copy of docs/guides/ingest-definitions.md
// (ADR-0046): the release archive has no docs folder, so the command writes the format guide next to the examples; a test
// keeps the copy equal to the original.
//
//go:embed samples-csv/source.json samples-csv/mapping.json samples-csv/sample samples-xlsx/source.json samples-xlsx/mapping.json guide/ingest-definitions.md
var ExampleFiles embed.FS
