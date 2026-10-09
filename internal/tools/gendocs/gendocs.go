// Package gendocs writes the command-line reference from the real command tree, so the
// published reference cannot drift from the program. It is not part of the shipped binary.
package gendocs

import (
	"io"

	"github.com/amitpatole/cgroupguard/internal/cli"
	"github.com/spf13/cobra/doc"
)

// Generate writes one Markdown file per command into dir.
func Generate(dir string) error {
	code := 0
	root := cli.NewRoot(io.Discard, io.Discard, &code)
	root.DisableAutoGenTag = true // no timestamps: output is reproducible
	return doc.GenMarkdownTree(root, dir)
}
