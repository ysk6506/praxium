//go:build !unix

package main

// openReadFlags is nothing on a system without the flags of unix: the file is looked at before and after the open.
const openReadFlags = 0
