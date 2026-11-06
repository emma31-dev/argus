// Command gen regenerates the SYNTHETIC fixture set that cmd/verify replays.
//
//	go run ./cmd/verify/gen [-out data/fixtures/synthetic]
//
// The data is invented; the scenario lives in internal/fixturegen.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/emma31-dev/argus/internal/fixturegen"
)

func main() {
	out := flag.String("out", "data/fixtures/synthetic", "output directory")
	flag.Parse()
	if err := fixturegen.Generate(*out); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Println("wrote SYNTHETIC fixtures to", *out)
}
