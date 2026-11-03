// Command gen regenerates the SYNTHETIC fixture set used by `make verify`.
//
//	go run ./cmd/verify/gen [-out evidence/fixtures/synthetic]
//
// The data is invented (see the generated README.md in the output directory).
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/emma31-dev/argus/internal/fixturegen"
)

func main() {
	out := flag.String("out", "evidence/fixtures/synthetic", "output directory")
	flag.Parse()
	if err := fixturegen.Generate(*out); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Println("wrote SYNTHETIC fixtures to", *out)
}
