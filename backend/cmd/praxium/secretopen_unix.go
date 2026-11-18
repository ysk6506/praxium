//go:build unix

package main

import "syscall"

// openReadFlags make the open of a file that is read refuse a link at the last part of the name and not wait for a
// pipe or a device that has nothing to give (the file was looked at before, and may have been swapped since).
const openReadFlags = syscall.O_NOFOLLOW | syscall.O_NONBLOCK
