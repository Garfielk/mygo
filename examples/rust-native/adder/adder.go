// Package adder binds a Rust crate, built by the CLI from ./rust as
// mygo-plugin.json says, with package ffi.
package adder

import (
	_ "embed"

	"github.com/egoist/mygo/ffi"
)

//go:embed mygo-plugin.json
var manifest []byte

var lib = ffi.New("adder", manifest)

// fns are the crate's functions, bound by their names in snake case.
var fns struct {
	Add         func(a, b int32) int32
	Fibonacci   func(n uint32) uint64
	CountVowels func(s string) uint32
	Version     func() string `ffi:"adder_version"`
}

// Load loads the library and binds its functions; the others need it.
func Load() error { return lib.Bind(&fns) }

// Add returns a + b, wrapping around.
func Add(a, b int32) int32 { return fns.Add(a, b) }

// Fibonacci returns the nth Fibonacci number.
func Fibonacci(n uint32) uint64 { return fns.Fibonacci(n) }

// CountVowels returns how many ASCII vowels s has.
func CountVowels(s string) uint32 { return fns.CountVowels(s) }

// Version returns the crate's name and version.
func Version() string { return fns.Version() }
