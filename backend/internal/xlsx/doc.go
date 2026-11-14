// Package xlsx reads the workbook files of Excel (.xlsx and .xlsm) into the cells the ingestion pipeline works with
// (ADR-0040). A workbook is a ZIP archive of XML parts, so the reader is the standard library's archive/zip and
// encoding/xml and nothing else.
//
// It reads bytes and follows nothing: an external link or a data connection of the workbook is never opened, and a
// macro of a macro-enabled file is neither read nor run. A formula gives the value the file stored for it. The file
// formats it cannot read (the binary .xls, a workbook with a password, a damaged archive) are an error that says so,
// and a file over a safety limit is an error that names the limit.
//
// The package also holds a small writer of workbooks (Book), which tests use to make files of every shape the
// readers of the real tools make; the product never calls it.
package xlsx
