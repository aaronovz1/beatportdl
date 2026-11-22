package main

import "golang.org/x/sys/unix"

const getInputTermios = unix.TIOCGETA
const setInputTermios = unix.TIOCSETA
