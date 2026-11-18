//go:build unix

package main

import "syscall"

// syscallMkfifo makes a named pipe, for the test of a value file that is not a plain file.
func syscallMkfifo(path string) error { return syscall.Mkfifo(path, 0o600) }
