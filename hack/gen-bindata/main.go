// +build ignore

/*
Copyright 2021 k0s authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"strings"
	"sync"
	"text/template"
)

var Usage = func() {
	fmt.Fprintf(os.Stderr, "Usage: %s [options] <directories>\n", os.Args[0])
	flag.PrintDefaults()
}

type fileInfo struct {
	Name         string
	Path         string
	TempFile     string
	Offset, Size int64
}

func compressFiles(prefix string) []fileInfo {
	var tmpFiles []fileInfo

	// compress the files
	var wg sync.WaitGroup
	for _, dir := range flag.Args() {
		files, err := os.ReadDir(dir)
		if err != nil {
			log.Fatal(err)
		}
		for _, f := range files {
			tmpf, err := os.CreateTemp("", f.Name())
			if err != nil {
				log.Fatal(err)
			}

			filePath := path.Join(dir, f.Name())
			name := strings.TrimPrefix(filePath, prefix) + ".gz"
			tmpFiles = append(tmpFiles, fileInfo{
				Name:     name,
				Path:     filePath,
				TempFile: tmpf.Name(),
			})

			gz, err := gzip.NewWriterLevel(tmpf, gzip.BestCompression)
			if err != nil {
				log.Fatal(err)
			}

			inf, err := os.Open(filePath)
			if err != nil {
				log.Fatal(err)
			}

			wg.Add(1)
			go func(wg *sync.WaitGroup) {
				size, err := io.Copy(gz, inf)
				if err != nil {
					log.Fatal(err)
				}

				fi, err := tmpf.Stat()
				if err != nil {
					log.Fatal(err)
				}

				inf.Close()
				gz.Close()
				fmt.Fprintf(os.Stderr, "%s: %d/%d MiB\n", name, fi.Size()/(1024*1024), size/(1024*1024))
				wg.Done()
			}(&wg)
		}
	}
	wg.Wait()
	return tmpFiles
}

func main() {
	var prefix, pkg, outfile, gofile string

	var bindata []fileInfo

	flag.StringVar(&prefix, "prefix", "", "Optional path prefix to strip off asset names.")
	flag.StringVar(&pkg, "pkg", "main", "Package name to use in the generated code.")
	flag.StringVar(&outfile, "o", "./bindata", "Optional name of the output file to be generated.")
	flag.StringVar(&gofile, "gofile", "./bindata.go", "Optional name of the go file to be generated.")
	flag.Parse()

	if flag.NArg() == 0 {
		Usage()
		os.Exit(1)
	}

	tmpFiles := compressFiles(prefix)

	outf, err := os.Create(outfile)
	if err != nil {
		log.Fatal(err)
	}
	defer outf.Close()

	var offset int64 = 0

	fmt.Fprintf(os.Stderr, "Writing %s...\n", outfile)
	for _, t := range tmpFiles {
		inf, err := os.Open(t.TempFile)
		if err != nil {
			log.Fatal(err)
		}

		size, err := io.Copy(outf, inf)
		inf.Close()
		if err != nil {
			log.Fatal(err)
		}
		os.Remove(t.TempFile)

		t.Offset = offset
		t.Size = size
		bindata = append(bindata, t)

		offset += size
	}

	f, err := os.Create(gofile)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	packageTemplate.Execute(f, struct {
		OutFile     string
		Pkg         string
		BinData     []fileInfo
		BinDataSize int64
	}{
		OutFile:     outfile,
		Pkg:         pkg,
		BinData:     bindata,
		BinDataSize: offset,
	})

}

var packageTemplate = template.Must(template.New("").Parse(`// Code generated by go generate; DO NOT EDIT.

// datafile: {{ .OutFile }}

package {{ .Pkg }}

var (
	BinData = map[string]struct{ offset, size int64 }{
	{{ range .BinData }}
		"{{ .Name }}": { {{ .Offset }}, {{ .Size }}}, {{ end }}
	}

	BinDataSize int64 = {{ .BinDataSize }}
)

`))
