package ingestcheck

import "embed"

// ExampleFiles are the definitions and the sample files that `praxium ingest init` writes for a person to try
// ingestion with (ADR-0041): the CSV and JSON Lines source of the acceptance tests with its samples, and the source
// of the Excel example. The values are made up. The workbook is not here: the command writes it with the writer of
// the xlsx unit, so no binary file is committed.
//
//go:embed testdata/source.json testdata/mapping.json testdata/sample samples-xlsx/source.json samples-xlsx/mapping.json
var ExampleFiles embed.FS
