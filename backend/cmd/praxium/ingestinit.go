package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/ysk6506/praxium/backend/internal/ingestcheck"
	"github.com/ysk6506/praxium/backend/internal/kernel"
	"github.com/ysk6506/praxium/backend/internal/xlsx"
)

const ingestInitUsage = "usage: praxium ingest init [-out <folder>]"

const ingestInitBody = `Writes the examples of ingestion into a folder, so you can try the whole path with no files of your own: a source
definition and a mapping definition for CSV and JSON Lines files with the sample files, and a source definition and
a mapping definition for an Excel workbook with a sample workbook (made up, with a title above the header, a merged
cell and a total row, as real sheets have). The files are made up; open them to see what a definition looks like, and
copy and change them for your own files. It never overwrites: if one of the files is there, it stops.

Then it prints the commands to try next: ingest inspect, ingest run -data-dir and local.
`

type initOptions struct{ out string }

func newInitFlags(o *initOptions) *flag.FlagSet {
	fs := flag.NewFlagSet("ingest init", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.out, "out", "praxium-example", "the folder to write the examples to (made if it is not there)")
	return fs
}

// initHelpEntries are the help entries of this file's commands.
func initHelpEntries(out map[string]helpEntry) {
	out["ingest init"] = helpEntry{usage: ingestInitUsage, body: ingestInitBody, flags: func() *flag.FlagSet { return newInitFlags(&initOptions{}) }}
}

// exampleFile is one file of the examples: where it goes, relative to the folder, and its content.
type exampleFile struct {
	name string
	data []byte
}

// csvExampleFiles are the files of the CSV and JSON Lines example, and the datasets they are the files of.
var csvExampleInputs = []struct{ dataset, file string }{
	{"ds_racks", "racks.csv"}, {"ds_servers", "servers.csv"}, {"ds_hosts", "hosts.jsonl"}, {"ds_vms", "vms.jsonl"}, {"ds_vm_power", "vm-power.csv"},
}

const workbookName = "datacenter.xlsx"

// exampleFiles reads the embedded definitions and samples and makes the workbook.
func exampleFiles() ([]exampleFile, error) {
	var files []exampleFile
	add := func(name, from string) error {
		data, err := ingestcheck.ExampleFiles.ReadFile(from)
		if err != nil {
			return err
		}
		files = append(files, exampleFile{name, data})
		return nil
	}
	steps := [][2]string{
		{"csv/source.json", "testdata/source.json"}, {"csv/mapping.json", "testdata/mapping.json"},
		{"excel/source.json", "samples-xlsx/source.json"}, {"excel/mapping.json", "samples-xlsx/mapping.json"},
	}
	for _, in := range csvExampleInputs {
		steps = append(steps, [2]string{"csv/" + in.file, "testdata/sample/" + in.file})
	}
	for _, s := range steps {
		if err := add(s[0], s[1]); err != nil {
			return nil, err
		}
	}
	book, err := exampleWorkbook()
	if err != nil {
		return nil, err
	}
	files = append(files, exampleFile{"excel/" + workbookName, book})
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	return files, nil
}

// exampleWorkbook writes the sample workbook with the writer of the xlsx unit: two sheets, the second with a title
// above the header, a merged cell and a total row. The names and numbers are made up.
func exampleWorkbook() ([]byte, error) {
	b := &xlsx.Book{SharedStrings: true}
	racks := b.Sheet("랙")
	racks.Row(1, xlsx.Str("자산번호"), xlsx.Str("이름"), xlsx.Str("높이(U)"))
	racks.Row(2, xlsx.Str("XR-3001"), xlsx.Str("rack-x1"), xlsx.Num("42"))
	racks.Row(3, xlsx.Str("XR-3002"), xlsx.Str("rack-x2"), xlsx.Num("45"))
	servers := b.Sheet("서버 목록")
	servers.Row(1, xlsx.Str("서버 목록 (예시)"))
	servers.Row(2, xlsx.Str("합성한 값입니다. 머리글은 3행입니다."))
	servers.Row(3, xlsx.Str("자산번호"), xlsx.Str("시리얼"), xlsx.Str("제조사"), xlsx.Str("랙"), xlsx.Str("전원"))
	servers.Row(4, xlsx.Str("XS-4001"), xlsx.Str("SN-X100"), xlsx.Str("Zorvane"), xlsx.Str("XR-3001"), xlsx.Str("on"))
	servers.Row(5, xlsx.Str("XS-4002"), xlsx.Str("SN-X200"), xlsx.Str("Zorvane"), xlsx.C{}, xlsx.Str("off"))
	servers.Row(6, xlsx.Str("XS-4003"), xlsx.Str("SN-X300"), xlsx.Str("Kelmar"), xlsx.Str("XR-3002"), xlsx.Str("on"))
	servers.Row(7, xlsx.Str("합계"))
	servers.Merges = []string{"D4:D5"}
	return b.Bytes()
}

// runInit is `praxium ingest init`.
func runInit(args []string, stdout, stderr io.Writer) int {
	var o initOptions
	fs := newInitFlags(&o)
	if err := fs.Parse(args); err != nil {
		return usageFail(stderr, flagProblem(err), "ingest init")
	}
	if fs.NArg() > 0 {
		return usageFail(stderr, kernel.Errorf(kernel.CodeInvalidValue, "unexpected argument %q", fs.Arg(0)), "ingest init")
	}
	files, err := exampleFiles()
	if err == nil {
		err = writeExamples(o.out, files)
	}
	if err != nil {
		fmt.Fprintf(stderr, "praxium: %s\n", errText(err))
		return exitFailure
	}
	printInitNext(stdout, o.out, files)
	return exitOK
}

// writeExamples writes the files under the folder and never over a file that is there: all of them or none.
func writeExamples(dir string, files []exampleFile) error {
	for _, f := range files {
		target := filepath.Join(dir, filepath.FromSlash(f.name))
		if _, err := os.Stat(target); err == nil {
			return kernel.Errorf(kernel.CodeConflict, "%s is already there; praxium ingest init does not overwrite, use an empty folder with -out", target)
		}
	}
	for _, f := range files {
		target := filepath.Join(dir, filepath.FromSlash(f.name))
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return kernel.Wrap(kernel.CodeInvalidValue, err, "cannot create the folder for "+target)
		}
		if err := os.WriteFile(target, f.data, 0o640); err != nil {
			return kernel.Wrap(kernel.CodeInvalidValue, err, "cannot write "+target)
		}
	}
	return nil
}

// printInitNext lists what was written and the commands to try next, with the paths this run wrote.
func printInitNext(out io.Writer, dir string, files []exampleFile) {
	fmt.Fprintf(out, "Wrote %d example files to %s:\n", len(files), dir)
	for _, f := range files {
		fmt.Fprintf(out, "  %s\n", filepath.Join(dir, filepath.FromSlash(f.name)))
	}
	csvDir, xlDir := path.Join(dir, "csv"), path.Join(dir, "excel")
	fmt.Fprintf(out, "\nTry it, one step at a time (the data folder \"data\" is made by the second step):\n\n")
	fmt.Fprintf(out, "1. Look at a file before you write a mapping for it:\n   praxium ingest inspect -input %s\n\n", path.Join(xlDir, workbookName))
	fmt.Fprintf(out, "2. Read the Excel workbook and the CSV and JSON Lines files into a KG kept in the folder \"data\":\n")
	fmt.Fprintf(out, "   praxium ingest run -data-dir data -source %s -mapping %s \\\n     -input ds_racks=%s -input ds_servers=%s\n",
		path.Join(xlDir, "source.json"), path.Join(xlDir, "mapping.json"), path.Join(xlDir, workbookName), path.Join(xlDir, workbookName))
	fmt.Fprintf(out, "   praxium ingest run -data-dir data -source %s -mapping %s \\\n", path.Join(csvDir, "source.json"), path.Join(csvDir, "mapping.json"))
	for i, in := range csvExampleInputs {
		end := " \\\n"
		if i == len(csvExampleInputs)-1 {
			end = "\n"
		}
		fmt.Fprintf(out, "     -input %s=%s%s", in.dataset, path.Join(csvDir, in.file), end)
	}
	fmt.Fprintf(out, "\n3. Look at the result in the web UI (open the address it prints):\n   praxium local -data-dir data\n")
}
