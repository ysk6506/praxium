//go:build !unix

package main

import "errors"

// syscallMkfifo: this system has no named pipes of this kind; the test does not make one.
func syscallMkfifo(string) error { return errors.New("no named pipes here") }
