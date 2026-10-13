package main

import "golang.org/x/sys/unix"

// fread is the BSD name for "the input queue". tcflush(fd, TCIFLUSH) is
// this ioctl on macOS.
const fread = 0x1

func flushInput(fd int) error {
	return unix.IoctlSetPointerInt(fd, unix.TIOCFLUSH, fread)
}
