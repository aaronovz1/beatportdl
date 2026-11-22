package main

import "golang.org/x/sys/unix"

const getInputTermios = unix.TCGETS
const setInputTermios = unix.TCSETS
