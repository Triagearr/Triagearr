//go:build !unix

package runlock

import "os"

// Non-unix platforms get no cross-process enforcement. Triagearr ships
// linux-only (see .goreleaser.yaml), so this path exists purely to keep
// cross-compilation and `go vet` green; the in-process channel still guards
// goroutines within one process.
func tryFlock(*os.File) error { return nil }

func unflock(*os.File) error { return nil }
