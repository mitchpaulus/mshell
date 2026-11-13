// Package lib holds the standard library, std.msh, which is built into the
// msh binary.
package lib

import _ "embed"

// Std is the text of std.msh.
//
//go:embed std.msh
var Std string
